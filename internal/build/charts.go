package build

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/helmwave/nelmwave/internal/chart"
	"github.com/helmwave/nelmwave/internal/config"
	"github.com/helmwave/nelmwave/internal/plan"
	"github.com/helmwave/nelmwave/internal/repo"
)

// Charts puts every chart the plan names into outDir/charts/ and records it in
// the release's ChartFile. Once that field is set, up/down/diff load the chart
// from the build directory and reach for nothing else — which is the point:
// build where the charts are, apply where they are not.
//
// Remote charts are downloaded; a chart already given as a local path is copied
// in as it is. Both end up in the same place, so the build directory is the
// whole story either way and there is no second rule to remember. Local paths
// are resolved relative to the manifest, like values and stores are.
//
// Charts are keyed by chart, not by release, so releases sharing one share a
// single copy. The directory is rebuilt from scratch on every run — a chart
// dropped from the manifest leaves nothing behind, an edited local chart is
// picked up, and a floating version constraint is re-resolved rather than
// pinned by an old build.
func Charts(p *plan.Plan, baseDir, outDir string, logger *zap.Logger) error {
	chartsDir := filepath.Join(outDir, plan.ChartsDir)
	if err := os.RemoveAll(chartsDir); err != nil {
		return fmt.Errorf("clean %q: %w", chartsDir, err)
	}

	// OCI credentials reach the helm getter through a Docker config.json, the
	// same way they do at apply time.
	registryConfig, cleanup, err := repo.DockerConfig(p.Repositories)
	if err != nil {
		return err
	}
	defer cleanup()

	// Sequential. Downloads are network-bound and would parallelise cleanly, but
	// a build already talks to every datasource one at a time, and the dedup
	// cache below is worth more than the wall clock here.
	cache := make(map[string]string)
	dirs := make(map[string]string)

	for _, key := range p.ReleaseNames() {
		rel := p.Releases[key]
		log := logger.With(zap.String("release", key), zap.String("chart", rel.Chart.Name))

		remote := chart.IsRemote(rel.Chart.Name)

		// A local chart is identified by where it comes from, a remote one by
		// the repository and reference it resolves to. Either way the directory
		// belongs to the chart and the cache entry to one version of it, so two
		// versions of the same chart sit side by side in one directory.
		var (
			res    repo.ChartResolution
			src    string
			dirKey string
			label  = rel.Chart.Name
		)
		if remote {
			res = repo.Resolve(rel.Chart.Name, p.Repositories)
			dirKey = res.RepoURL + "|" + res.Ref
		} else {
			src = localSource(baseDir, rel.Chart.Name)
			dirKey = src
			// "../charts/api" and "./charts/api.tgz" both read as "api" here;
			// where they collide, chartDir keeps them apart.
			label = strings.TrimSuffix(filepath.Base(src), ".tgz")
		}
		cacheKey := dirKey + "|" + rel.Chart.Version

		path, done := cache[cacheKey]
		if !done {
			dir := filepath.Join(chartsDir, chartDir(dirs, label, dirKey))
			if remote {
				path, err = download(res, rel.Chart.Version, registryConfig, dir, outDir)
			} else {
				path, err = copyLocal(src, dir, outDir)
			}
			if err != nil {
				return fmt.Errorf("release %q: %w", key, err)
			}
			cache[cacheKey] = path
			log.Info("chart added to the build directory", zap.String("path", path), zap.Bool("downloaded", remote))
		} else {
			log.Debug("chart already in the build directory", zap.String("path", path))
		}

		rel.ChartFile = path
		p.Releases[key] = rel
	}
	return nil
}

// download fetches one resolved chart into dir and returns the archive's path
// relative to outDir, which is what the plan records.
func download(res repo.ChartResolution, version, registryConfig, dir, outDir string) (string, error) {
	// Validated by config.Validate before the plan was projected.
	var timeout time.Duration
	if res.RequestTimeout != "" {
		var err error
		if timeout, err = time.ParseDuration(res.RequestTimeout); err != nil {
			return "", fmt.Errorf("invalid repository requestTimeout %q: %w", res.RequestTimeout, err)
		}
	}

	path, err := chart.Download(chart.Options{
		Ref:                res.Ref,
		Version:            version,
		RepoURL:            res.RepoURL,
		Username:           res.Username,
		Password:           res.Password,
		PassCredentials:    res.PassCredentials,
		SkipTLSVerify:      res.SkipTLSVerify,
		CAFile:             res.CAFile,
		CertFile:           res.CertFile,
		KeyFile:            res.KeyFile,
		PlainHTTP:          res.OCIPlainHTTP,
		RequestTimeout:     timeout,
		ProvenanceStrategy: res.ProvenanceStrategy,
		ProvenanceKeyring:  res.ProvenanceKeyring,
		RegistryConfigPath: registryConfig,
	}, dir)
	if err != nil {
		return "", err
	}
	return planPath(path, outDir)
}

// localSource turns a local chart reference into a path on disk. Relative
// references are read from the manifest's directory, exactly as values and
// stores are — the manifest is the project, not whatever directory a command
// happened to run in.
func localSource(baseDir, ref string) string {
	path := filepath.FromSlash(ref)
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(baseDir, path))
}

// copyLocal copies a chart that is already on disk into dir and returns its
// path relative to outDir. A packaged chart lands beside its directory like a
// downloaded one; an unpacked chart becomes the directory itself, so what the
// plan points at is the chart as the manifest pointed at it.
func copyLocal(src, dir, outDir string) (string, error) {
	info, err := os.Stat(src)
	if err != nil {
		return "", fmt.Errorf("local chart %q (a relative path is resolved from the manifest's directory): %w", src, err)
	}

	if !info.IsDir() {
		data, err := os.ReadFile(src)
		if err != nil {
			return "", fmt.Errorf("read local chart %q: %w", src, err)
		}
		dest := filepath.Join(dir, filepath.Base(src))
		if err := writeFile(dest, data); err != nil {
			return "", err
		}
		return planPath(dest, outDir)
	}

	// A directory with no Chart.yaml is not a chart, and saying so here beats
	// letting nelm discover it once the cluster is already involved.
	if _, err := os.Stat(filepath.Join(src, "Chart.yaml")); err != nil {
		return "", fmt.Errorf("local chart %q has no Chart.yaml", src)
	}
	if err := copyTree(src, dir); err != nil {
		return "", fmt.Errorf("copy local chart %q: %w", src, err)
	}
	return planPath(dir, outDir)
}

// copyTree copies a directory recursively, keeping regular files and the
// directories holding them. Anything else — sockets, devices, dangling
// symlinks — is not part of a chart and is skipped rather than reproduced.
func copyTree(src, dest string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return writeFile(target, data)
	})
}

// planPath expresses a written artifact the way the planfile records it:
// relative to the build directory, with forward slashes.
func planPath(path, outDir string) (string, error) {
	rel, err := filepath.Rel(outDir, path)
	if err != nil {
		return "", fmt.Errorf("locate chart %q under %q: %w", path, outDir, err)
	}
	return filepath.ToSlash(rel), nil
}

// chartDir picks the directory a chart's archives live in: the reference made
// safe as one path segment, so charts/ stays readable next to a planfile that
// names the same chart. Two references that sanitize alike get a numeric suffix
// rather than sharing a directory; dirs remembers which chart owns what.
func chartDir(dirs map[string]string, ref, dirKey string) string {
	base := sanitizeSegment(strings.TrimPrefix(strings.TrimPrefix(ref, config.OCIPlainHTTPScheme), config.OCIScheme))
	if base == "" {
		base = "chart"
	}
	for i := 1; ; i++ {
		name := base
		if i > 1 {
			name = fmt.Sprintf("%s-%d", base, i)
		}
		owner, taken := dirs[name]
		if !taken {
			dirs[name] = dirKey
			return name
		}
		if owner == dirKey {
			return name
		}
	}
}

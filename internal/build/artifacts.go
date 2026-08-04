// Package build resolves a validated config's datasources into concrete
// artifacts under .nelmwave/: merged per-release values and copied store files.
// It runs during `nelmwave build`, after config validation and plan projection.
package build

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"

	"github.com/helmwave/nelmwave/internal/config"
	"github.com/helmwave/nelmwave/internal/datasource"
	"github.com/helmwave/nelmwave/internal/plan"
)

// Artifacts resolves values and store files for every release in p, writing
// them under outDir and recording each release's merged values path in
// p.Releases[...].ValuesFile. Datasource references resolve relative to baseDir.
//
// Global values/labels are applied earlier via confijer type-defaults, so each
// release's Values already carry any inherited defaults.
//
// Within a release, stores are resolved first, then values. Each resolved
// artifact is registered as a gomplate datasource ("stores/<name>" or
// "values/<name>") so a later *.tpl artifact can pull an earlier one via
// ds/include. Ordering is backward-only: an item sees only artifacts resolved
// before it, within the same release.
func Artifacts(ctx context.Context, cfg *config.Config, p *plan.Plan, baseDir, outDir string, logger *zap.Logger) error {
	res := datasource.NewResolver(baseDir)
	valuesDir := filepath.Join(outDir, plan.ValuesDir)
	storeDir := filepath.Join(outDir, plan.StoreDir)

	// Start clean so removed releases/sources don't leave stale artifacts.
	for _, dir := range []string{valuesDir, storeDir} {
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("clean %q: %w", dir, err)
		}
	}

	// A shared empty file backs datasources for skipped optional artifacts, so a
	// reference to one renders empty instead of erroring. It is written on first
	// use, so a manifest without optional sources leaves no stray file behind.
	emptyURL := lazyEmptyPlaceholder(outDir)

	for _, key := range p.ReleaseNames() {
		rc := cfg.Releases[key]
		log := logger.With(zap.String("release", key))

		// sources accumulates this release's resolved artifact datasources.
		sources := map[string]string{}

		if _, err := resolveList(ctx, res, rc.Stores, key, outDir, plan.StoreDir, "stores", sources, emptyURL, log); err != nil {
			return err
		}
		files, err := resolveList(ctx, res, rc.Values, key, outDir, plan.ValuesDir, "values", sources, emptyURL, log)
		if err != nil {
			return err
		}
		if len(files) > 0 {
			rel := p.Releases[key]
			rel.ValuesFiles = files
			p.Releases[key] = rel
		}
	}
	return nil
}

// resolveList resolves one ordered list of refs (values or store) for a release,
// writing each artifact under outDir/subDir/<uniqname>/ and registering it in
// sources under "<ns>/<name>". It returns the plan-relative paths of the written
// files (used for values). A skipped optional is registered to emptyURL.
func resolveList(ctx context.Context, res *datasource.Resolver, refs []config.FileRef, key, outDir, subDir, ns string, sources map[string]string, emptyURL func() (string, error), log *zap.Logger) ([]string, error) {
	relDir := filepath.Join(outDir, subDir, sanitize(key))
	seen := make(map[string]struct{})
	var files []string
	for i, ref := range refs {
		name, err := artifactName(i, ref.Src, ref.Name)
		if err != nil {
			return nil, fmt.Errorf("release %q: %s: %w", key, ns, err)
		}
		if _, dup := seen[name]; dup {
			return nil, fmt.Errorf("release %q: duplicate %s name %q", key, ns, name)
		}
		seen[name] = struct{}{}
		dsKey := ns + "/" + name

		data, err := res.Resolve(ctx, ref.Src, sources)
		if err != nil {
			if ref.Optional && isMissing(err) {
				log.Warn("optional source skipped", zap.String("src", ref.Src), zap.String("kind", ns))
				placeholder, err := emptyURL()
				if err != nil {
					return nil, err
				}
				sources[dsKey] = placeholder
				continue
			}
			return nil, fmt.Errorf("release %q: resolve %s %q: %w", key, ns, ref.Src, err)
		}

		path := filepath.Join(relDir, filepath.FromSlash(name))
		if err := writeFile(path, data); err != nil {
			return nil, err
		}
		sources[dsKey], err = fileURL(path)
		if err != nil {
			return nil, err
		}
		files = append(files, filepath.ToSlash(filepath.Join(subDir, sanitize(key), name)))
		log.Debug("artifact resolved", zap.String("datasource", dsKey))
	}
	return files, nil
}

// lazyEmptyPlaceholder returns a func that creates the shared empty placeholder
// under outDir on first call and returns its file:// URL, memoizing the result.
// Builds are single-threaded, so no locking is needed.
func lazyEmptyPlaceholder(outDir string) func() (string, error) {
	var url string
	return func() (string, error) {
		if url != "" {
			return url, nil
		}
		path := filepath.Join(outDir, ".empty")
		if err := writeFile(path, nil); err != nil {
			return "", err
		}
		u, err := fileURL(path)
		if err != nil {
			return "", err
		}
		url = u
		return url, nil
	}
}

// fileURL returns an absolute file:// URL for a local path.
func fileURL(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve path %q: %w", path, err)
	}
	return "file://" + filepath.ToSlash(abs), nil
}

// artifactName returns the file name for a resolved values/store artifact: the
// caller-provided name (a relative path under the release's artifact dir) when
// set, otherwise an index-prefixed sanitized basename for deterministic,
// collision-free ordering.
func artifactName(index int, src, name string) (string, error) {
	if name != "" {
		if !safeRelPath(name) {
			return "", fmt.Errorf("name %q escapes the artifact directory", name)
		}
		return name, nil
	}
	return indexedBasename(index, src), nil
}

// indexedBasename builds an ordered, collision-free file name from a source: a
// zero-padded index prefix plus a sanitized basename derived from the source.
func indexedBasename(index int, src string) string {
	label := src
	if i := strings.IndexAny(label, "?#"); i >= 0 {
		label = label[:i]
	}
	label = strings.TrimSuffix(strings.TrimSuffix(label, ".tpl"), ".tmpl")
	label = pathBase(label)
	label = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			return r
		default:
			return '_'
		}
	}, label)
	if label == "" {
		label = "values"
	}
	return fmt.Sprintf("%02d-%s", index, label)
}

// pathBase returns the last '/'-separated segment of s (URLs and manifest paths
// both use '/'), without importing path just for this.
func pathBase(s string) string {
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}

// isMissing reports whether err is a "source not found" error, so an optional
// source can be skipped without masking real failures.
func isMissing(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}

// safeRelPath rejects absolute paths and any that escape via "..".
func safeRelPath(p string) bool {
	if filepath.IsAbs(p) {
		return false
	}
	clean := filepath.Clean(filepath.FromSlash(p))
	return clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
}

// sanitize makes a release uniqname safe as a single path segment.
func sanitize(key string) string {
	return strings.NewReplacer("/", "_", string(filepath.Separator), "_").Replace(key)
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create dir for %q: %w", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %q: %w", path, err)
	}
	return nil
}

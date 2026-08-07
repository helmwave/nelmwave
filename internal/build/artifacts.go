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
	"sync"

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
//
// Resolution is sequential, and has to be: gomplate v5 records every render in a
// package-level Metrics struct without synchronisation, so concurrent Render
// calls crash with "concurrent map writes" (gomplate/v5@v5.2.0 render.go:226).
// Releases are otherwise independent and would parallelise cleanly — revisit
// this once gomplate is safe for concurrent use.
func Artifacts(ctx context.Context, cfg *config.Config, p *plan.Plan, baseDir, outDir string, logger *zap.Logger) error {
	res := datasource.NewResolver(baseDir)

	out, err := openOut(outDir)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	// Start clean so removed releases/sources don't leave stale artifacts.
	for _, dir := range []string{plan.ValuesDir, plan.StoreDir} {
		if err := out.RemoveAll(dir); err != nil {
			return fmt.Errorf("clean %q: %w", filepath.Join(outDir, dir), err)
		}
	}

	// A shared empty file backs datasources for skipped optional artifacts, so a
	// reference to one renders empty instead of erroring. It is written on first
	// use, so a manifest without optional sources leaves no stray file behind.
	emptyURL := lazyEmptyPlaceholder(out)

	for _, key := range p.ReleaseNames() {
		rc := cfg.Releases[key]
		log := logger.With(zap.String("release", key))

		// sources accumulates this release's resolved artifact datasources.
		sources := map[string]string{}

		if _, err := resolveList(ctx, res, rc.Stores, key, out, plan.StoreDir, "stores", sources, emptyURL, log); err != nil {
			return err
		}
		files, err := resolveList(ctx, res, rc.Values, key, out, plan.ValuesDir, "values", sources, emptyURL, log)
		if err != nil {
			return err
		}
		if len(files) > 0 {
			rel := p.Releases[key]
			rel.ValuesFiles = files
			p.Releases[key] = rel
		}
	}

	warnAboutDecryptedSecrets(cfg, outDir, logger)
	return nil
}

// warnAboutDecryptedSecrets points out that sops sources have just been written
// out in cleartext. The build directory is gitignored, but nothing stops it from
// being archived as a CI artifact — and the manifest gives no hint that it now
// holds secrets.
func warnAboutDecryptedSecrets(cfg *config.Config, outDir string, logger *zap.Logger) {
	var n int
	for _, rc := range cfg.Releases {
		for _, ref := range append(append([]config.FileRef{}, rc.Stores...), rc.Values...) {
			if datasource.IsEncrypted(ref.Src) {
				n++
			}
		}
	}
	if n == 0 {
		return
	}
	logger.Warn("decrypted secrets written in cleartext",
		zap.Int("sources", n),
		zap.String("dir", outDir),
		zap.String("hint", "treat this directory as sensitive: do not publish it as a build artifact"))
}

// resolveList resolves one ordered list of refs (values or store) for a release,
// writing each artifact under <build dir>/subDir/<uniqname>/ and registering it
// in sources under "<ns>/<name>". It returns the plan-relative paths of the
// written files (used for values). A skipped optional is registered to emptyURL.
func resolveList(ctx context.Context, res *datasource.Resolver, refs []config.FileRef, key string, out *os.Root, subDir, ns string, sources map[string]string, emptyURL func() (string, error), log *zap.Logger) ([]string, error) {
	relDir := filepath.Join(subDir, sanitize(key))
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
		if err := writeFile(out, path, data); err != nil {
			return nil, err
		}
		sources[dsKey], err = fileURL(filepath.Join(out.Name(), path))
		if err != nil {
			return nil, err
		}
		files = append(files, filepath.ToSlash(path))
		log.Debug("artifact resolved", zap.String("datasource", dsKey))
	}
	return files, nil
}

// lazyEmptyPlaceholder returns a func that creates the shared empty placeholder
// in the build directory on first call and returns its file:// URL, memoizing
// the result. The sync.Once keeps it to a single write even though resolution is
// currently sequential, so this stays correct if that ever changes.
func lazyEmptyPlaceholder(out *os.Root) func() (string, error) {
	var (
		once sync.Once
		url  string
		err  error
	)
	return func() (string, error) {
		once.Do(func() {
			const name = ".empty"
			if err = writeFile(out, name, nil); err != nil {
				return
			}
			url, err = fileURL(filepath.Join(out.Name(), name))
		})
		return url, err
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
	// Drop the suffixes that named a processing step, in the order they were
	// applied: what lands on disk is rendered cleartext, and a name ending in
	// .sops or .tpl would claim otherwise.
	label = strings.TrimSuffix(label, ".sops")
	label = strings.TrimSuffix(strings.TrimSuffix(label, ".tpl"), ".tmpl")
	label = sanitizeSegment(pathBase(label))
	if label == "" {
		label = "values"
	}
	return fmt.Sprintf("%02d-%s", index, label)
}

// sanitizeSegment makes an arbitrary string usable as one path segment, mapping
// everything outside [A-Za-z0-9._-] to '_'.
func sanitizeSegment(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			return r
		default:
			return '_'
		}
	}, s)
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

// openOut opens the build directory as an os.Root, creating it if needed. Every
// artifact below is written through that root, so the kernel resolves each path
// inside the build directory: a name that came from the manifest cannot escape
// it, and neither can a symlink planted between two writes. safeRelPath still
// rejects the obvious escapes up front — this is the backstop that does not
// depend on getting the string handling right.
func openOut(outDir string) (*os.Root, error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, fmt.Errorf("create build dir %q: %w", outDir, err)
	}
	root, err := os.OpenRoot(outDir)
	if err != nil {
		return nil, fmt.Errorf("open build dir %q: %w", outDir, err)
	}
	return root, nil
}

// writeFile writes one artifact at name, a path relative to the build directory.
func writeFile(out *os.Root, name string, data []byte) error {
	if dir := filepath.Dir(name); dir != "." {
		if err := out.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create dir for %q: %w", name, err)
		}
	}
	if err := out.WriteFile(name, data, 0o644); err != nil {
		return fmt.Errorf("write %q: %w", name, err)
	}
	return nil
}

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

	for _, key := range p.ReleaseNames() {
		rc := cfg.Releases[key]
		log := logger.With(zap.String("release", key))

		if err := resolveValues(ctx, res, rc, key, p, valuesDir, log); err != nil {
			return err
		}
		if err := resolveStore(ctx, res, rc, key, storeDir, log); err != nil {
			return err
		}
	}
	return nil
}

func resolveValues(ctx context.Context, res *datasource.Resolver, rc config.Release, key string, p *plan.Plan, valuesDir string, log *zap.Logger) error {
	relDir := filepath.Join(valuesDir, sanitize(key))
	var files []string
	for _, ref := range rc.Values {
		data, err := res.Resolve(ctx, ref.Src)
		if err != nil {
			if ref.Optional && isMissing(err) {
				log.Warn("optional values source skipped", zap.String("src", ref.Src))
				continue
			}
			return fmt.Errorf("release %q: resolve values %q: %w", key, ref.Src, err)
		}
		name, err := artifactName(len(files), ref.Src, ref.Alias)
		if err != nil {
			return fmt.Errorf("release %q: values: %w", key, err)
		}
		if err := writeFile(filepath.Join(relDir, filepath.FromSlash(name)), data); err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(filepath.Join(plan.ValuesDir, sanitize(key), name)))
	}
	if len(files) == 0 {
		return nil
	}

	rel := p.Releases[key]
	rel.ValuesFiles = files
	p.Releases[key] = rel
	log.Debug("values resolved", zap.Int("files", len(files)))
	return nil
}

// artifactName returns the file name for a resolved values/store artifact: the
// caller-provided alias (a relative path under the release's artifact dir) when
// set, otherwise an index-prefixed sanitized basename for deterministic,
// collision-free ordering.
func artifactName(index int, src, alias string) (string, error) {
	if alias != "" {
		if !safeRelPath(alias) {
			return "", fmt.Errorf("alias %q escapes the artifact directory", alias)
		}
		return alias, nil
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

func resolveStore(ctx context.Context, res *datasource.Resolver, rc config.Release, key, storeDir string, log *zap.Logger) error {
	written := 0
	for _, s := range rc.Store {
		data, err := res.Resolve(ctx, s.Src)
		if err != nil {
			if s.Optional && isMissing(err) {
				log.Warn("optional store source skipped", zap.String("src", s.Src))
				continue
			}
			return fmt.Errorf("release %q: resolve store %q: %w", key, s.Src, err)
		}
		name, err := artifactName(written, s.Src, s.Alias)
		if err != nil {
			return fmt.Errorf("release %q: store: %w", key, err)
		}
		path := filepath.Join(storeDir, sanitize(key), filepath.FromSlash(name))
		if err := writeFile(path, data); err != nil {
			return err
		}
		written++
		log.Debug("store written", zap.String("src", s.Src), zap.String("name", name))
	}
	return nil
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

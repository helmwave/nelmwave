package build

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/helmwave/nelmwave/internal/config"
	"github.com/helmwave/nelmwave/internal/plan"
)

func TestArtifacts_WritesOrderedValuesAndStore(t *testing.T) {
	base := t.TempDir()
	mustWrite(t, base, "common.yml", "resources:\n  requests:\n    cpu: 50m\n")
	mustWrite(t, base, "pg.yml.tpl", "replicas: [[ getenv \"N\" \"3\" ]]\n")
	mustWrite(t, base, "netpol.yml", "kind: NetworkPolicy\n")

	cfg := &config.Config{
		Releases: map[string]config.Release{
			"db@data": {
				Chart:  config.Chart{Name: "r/db"},
				Values: []config.FileRef{{Src: "common.yml"}, {Src: "pg.yml.tpl"}},
				Stores: []config.FileRef{{Src: "netpol.yml", Name: "custom-netpol.yml"}},
			},
		},
	}
	p := plan.FromConfig(cfg)
	out := filepath.Join(t.TempDir(), "out")

	if err := Artifacts(context.Background(), cfg, p, base, out, zap.NewNop()); err != nil {
		t.Fatalf("Artifacts: %v", err)
	}

	// Plan lists the values files in declared order.
	got := p.Releases["db@data"].ValuesFiles
	want := []string{"values/db@data/00-common.yml", "values/db@data/01-pg.yml"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("ValuesFiles = %v, want %v", got, want)
	}

	// The per-release template was rendered when written (no merge performed).
	data, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(got[1])))
	if err != nil {
		t.Fatalf("read values file: %v", err)
	}
	if strings.TrimSpace(string(data)) != "replicas: 3" {
		t.Errorf("rendered values = %q", data)
	}

	// Store file written under its alias.
	if _, err := os.Stat(filepath.Join(out, "stores", "db@data", "custom-netpol.yml")); err != nil {
		t.Errorf("store file missing: %v", err)
	}
}

func TestArtifacts_OptionalMissingSkipped(t *testing.T) {
	base := t.TempDir()
	cfg := &config.Config{
		Releases: map[string]config.Release{
			"a@n": {
				Chart:  config.Chart{Name: "r/a"},
				Values: []config.FileRef{{Src: "gone.yml", Optional: true}},
			},
		},
	}
	p := plan.FromConfig(cfg)
	out := filepath.Join(t.TempDir(), "out")

	if err := Artifacts(context.Background(), cfg, p, base, out, zap.NewNop()); err != nil {
		t.Fatalf("optional-missing should not fail: %v", err)
	}
	if len(p.Releases["a@n"].ValuesFiles) != 0 {
		t.Errorf("no values files expected when the only source is a skipped optional")
	}
	// The skip is what creates the placeholder that backs its datasource.
	if _, err := os.Stat(filepath.Join(out, ".empty")); err != nil {
		t.Errorf("placeholder should back the skipped optional's datasource: %v", err)
	}
}

func TestArtifacts_NoPlaceholderWithoutOptionalSkips(t *testing.T) {
	base := t.TempDir()
	mustWrite(t, base, "v.yml", "a: 1\n")
	cfg := &config.Config{
		Releases: map[string]config.Release{
			"a@n": {
				Chart:  config.Chart{Name: "r/a"},
				Values: []config.FileRef{{Src: "v.yml"}},
			},
		},
	}
	p := plan.FromConfig(cfg)
	out := filepath.Join(t.TempDir(), "out")

	if err := Artifacts(context.Background(), cfg, p, base, out, zap.NewNop()); err != nil {
		t.Fatalf("build failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, ".empty")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("placeholder should not exist when nothing was skipped (stat err: %v)", err)
	}
}

func TestArtifacts_RequiredMissingFails(t *testing.T) {
	base := t.TempDir()
	cfg := &config.Config{
		Releases: map[string]config.Release{
			"a@n": {
				Chart:  config.Chart{Name: "r/a"},
				Values: []config.FileRef{{Src: "gone.yml"}},
			},
		},
	}
	p := plan.FromConfig(cfg)
	if err := Artifacts(context.Background(), cfg, p, base, filepath.Join(t.TempDir(), "out"), zap.NewNop()); err == nil {
		t.Fatal("required missing source should fail the build")
	}
}

func mustWrite(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestArtifacts_StoreWithoutAliasUsesIndexedBasename(t *testing.T) {
	base := t.TempDir()
	mustWrite(t, base, "a.yml", "x: 1\n")
	mustWrite(t, base, "b.yml", "y: 2\n")
	cfg := &config.Config{
		Releases: map[string]config.Release{
			"r@n": {
				Chart:  config.Chart{Name: "c/r"},
				Stores: []config.FileRef{{Src: "a.yml"}, {Src: "b.yml", Name: "named.yml"}},
			},
		},
	}
	p := plan.FromConfig(cfg)
	out := filepath.Join(t.TempDir(), "out")
	if err := Artifacts(context.Background(), cfg, p, base, out, zap.NewNop()); err != nil {
		t.Fatalf("Artifacts: %v", err)
	}
	// no alias -> index-prefixed basename
	if _, err := os.Stat(filepath.Join(out, "stores", "r@n", "00-a.yml")); err != nil {
		t.Errorf("aliasless store file should be 00-a.yml: %v", err)
	}
	// alias -> exact name
	if _, err := os.Stat(filepath.Join(out, "stores", "r@n", "named.yml")); err != nil {
		t.Errorf("aliased store file should be named.yml: %v", err)
	}
}

func TestArtifacts_DuplicateNameRejected(t *testing.T) {
	base := t.TempDir()
	mustWrite(t, base, "a.yml", "x: 1\n")
	mustWrite(t, base, "b.yml", "y: 2\n")
	cfg := &config.Config{
		Releases: map[string]config.Release{
			"r@n": {
				Chart:  config.Chart{Name: "c/r"},
				Values: []config.FileRef{{Src: "a.yml", Name: "dup.yml"}, {Src: "b.yml", Name: "dup.yml"}},
			},
		},
	}
	p := plan.FromConfig(cfg)
	err := Artifacts(context.Background(), cfg, p, base, filepath.Join(t.TempDir(), "out"), zap.NewNop())
	if err == nil || !strings.Contains(err.Error(), "duplicate values name") {
		t.Fatalf("expected duplicate-name error, got: %v", err)
	}
}

func TestArtifacts_DatasourceCrossReferences(t *testing.T) {
	base := t.TempDir()
	mustWrite(t, base, "data.yml", "foo: bar\n")
	// store #1 (.tpl) pulls store #0 that was resolved earlier.
	mustWrite(t, base, "combined.yml.tpl", `wrapped: [[ (ds "stores/data.yml").foo ]]`)
	// a value pulls a store (parsed) and references a skipped optional (empty).
	mustWrite(t, base, "vals.yml.tpl", `fromStore: [[ (ds "stores/data.yml").foo ]] opt=[[ include "stores/gone.yml" ]]end`)

	cfg := &config.Config{
		Releases: map[string]config.Release{
			"r@n": {
				Chart: config.Chart{Name: "c/r"},
				Stores: []config.FileRef{
					{Src: "data.yml", Name: "data.yml"},
					{Src: "combined.yml.tpl", Name: "combined.yml"},
					{Src: "gone.yml", Name: "gone.yml", Optional: true}, // missing -> empty datasource
				},
				Values: []config.FileRef{{Src: "vals.yml.tpl", Name: "vals.yml"}},
			},
		},
	}
	p := plan.FromConfig(cfg)
	out := filepath.Join(t.TempDir(), "out")
	if err := Artifacts(context.Background(), cfg, p, base, out, zap.NewNop()); err != nil {
		t.Fatalf("Artifacts: %v", err)
	}

	// store -> store reference
	got := readFile(t, filepath.Join(out, "stores", "r@n", "combined.yml"))
	if got != "wrapped: bar" {
		t.Errorf("store->store = %q", got)
	}
	// value -> store (parsed) + missing optional renders empty
	got = readFile(t, filepath.Join(out, "values", "r@n", "vals.yml"))
	if got != "fromStore: bar opt=end" {
		t.Errorf("value->store / empty-optional = %q", got)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	return strings.TrimSpace(string(b))
}

func TestIndexedBasename_DropsProcessingSuffixes(t *testing.T) {
	// The artifact on disk is decrypted and rendered, so its name must not keep
	// suffixes that claim otherwise.
	cases := map[string]string{
		"values/db.yml":          "00-db.yml",
		"values/db.yml.tpl":      "00-db.yml",
		"values/db.yml.sops":     "00-db.yml",
		"values/db.yml.tpl.sops": "00-db.yml",
	}
	for src, want := range cases {
		if got := indexedBasename(0, src); got != want {
			t.Errorf("indexedBasename(%q) = %q, want %q", src, got, want)
		}
	}
}

func TestArtifacts_WarnsWhenSecretsWereDecrypted(t *testing.T) {
	base := t.TempDir()
	mustWrite(t, base, "plain.yml", "a: 1\n")

	cfg := &config.Config{
		Releases: map[string]config.Release{
			"a@n": {
				Chart:  config.Chart{Name: "r/a"},
				Values: []config.FileRef{{Src: "plain.yml"}},
			},
		},
	}
	core, logs := observer.New(zap.WarnLevel)
	if err := Artifacts(context.Background(), cfg, plan.FromConfig(cfg), base, filepath.Join(t.TempDir(), "out"), zap.New(core)); err != nil {
		t.Fatalf("build: %v", err)
	}
	if n := logs.FilterMessage("decrypted secrets written in cleartext").Len(); n != 0 {
		t.Errorf("no sops sources, so no warning expected, got %d", n)
	}

	// The same build with an encrypted source warns once, counting the sources.
	// Resolution itself is not exercised here (that needs keys) — the source is
	// optional so the build still succeeds.
	cfg.Releases["a@n"] = config.Release{
		Chart:  config.Chart{Name: "r/a"},
		Values: []config.FileRef{{Src: "gone.yml.sops", Optional: true}},
	}
	core, logs = observer.New(zap.WarnLevel)
	if err := Artifacts(context.Background(), cfg, plan.FromConfig(cfg), base, filepath.Join(t.TempDir(), "out"), zap.New(core)); err != nil {
		t.Fatalf("build: %v", err)
	}
	entries := logs.FilterMessage("decrypted secrets written in cleartext").All()
	if len(entries) != 1 {
		t.Fatalf("want exactly one warning, got %d", len(entries))
	}
	if got := entries[0].ContextMap()["sources"]; got != int64(1) {
		t.Errorf("sources = %v, want 1", got)
	}
}

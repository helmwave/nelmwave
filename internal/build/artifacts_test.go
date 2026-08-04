package build

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"

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
				Store:  []config.FileRef{{Src: "netpol.yml", Name: "custom-netpol.yml"}},
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
	if _, err := os.Stat(filepath.Join(out, "store", "db@data", "custom-netpol.yml")); err != nil {
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
				Chart: config.Chart{Name: "c/r"},
				Store: []config.FileRef{{Src: "a.yml"}, {Src: "b.yml", Name: "named.yml"}},
			},
		},
	}
	p := plan.FromConfig(cfg)
	out := filepath.Join(t.TempDir(), "out")
	if err := Artifacts(context.Background(), cfg, p, base, out, zap.NewNop()); err != nil {
		t.Fatalf("Artifacts: %v", err)
	}
	// no alias -> index-prefixed basename
	if _, err := os.Stat(filepath.Join(out, "store", "r@n", "00-a.yml")); err != nil {
		t.Errorf("aliasless store file should be 00-a.yml: %v", err)
	}
	// alias -> exact name
	if _, err := os.Stat(filepath.Join(out, "store", "r@n", "named.yml")); err != nil {
		t.Errorf("aliased store file should be named.yml: %v", err)
	}
}

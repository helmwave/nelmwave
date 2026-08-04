package build

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"

	"github.com/helmwave/nelmwave/internal/config"
	"github.com/helmwave/nelmwave/internal/plan"
)

func TestArtifacts_MergesValuesAndWritesStore(t *testing.T) {
	base := t.TempDir()
	mustWrite(t, base, "common.yml", "resources:\n  requests:\n    cpu: 50m\n    memory: 64Mi\n")
	mustWrite(t, base, "pg.yml.tpl", "resources:\n  requests:\n    cpu: [[ getenv \"CPU\" \"250m\" ]]\n")
	mustWrite(t, base, "netpol.yml", "kind: NetworkPolicy\n")

	cfg := &config.Config{
		Values: []config.FileRef{{Src: "common.yml"}},
		Releases: map[string]config.Release{
			"db@data": {
				Chart:  config.Chart{Name: "r/db"},
				Values: []config.FileRef{{Src: "pg.yml.tpl"}},
				Store:  []config.FileRef{{Src: "netpol.yml", Dst: "manifests/netpol.yml"}},
			},
		},
	}
	p := plan.FromConfig(cfg)
	out := filepath.Join(t.TempDir(), "out")

	if err := Artifacts(context.Background(), cfg, p, base, out, zap.NewNop()); err != nil {
		t.Fatalf("Artifacts: %v", err)
	}

	// values merged: global memory kept, per-release cpu overrides.
	data, err := os.ReadFile(filepath.Join(out, "values", "db@data.yml"))
	if err != nil {
		t.Fatalf("read merged values: %v", err)
	}
	var v map[string]any
	if err := yaml.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	req := v["resources"].(map[string]any)["requests"].(map[string]any)
	if req["cpu"] != "250m" || req["memory"] != "64Mi" {
		t.Errorf("unexpected merged requests: %v", req)
	}

	// plan records the values path.
	if got := p.Releases["db@data"].ValuesFile; got != "values/db@data.yml" {
		t.Errorf("ValuesFile = %q", got)
	}

	// store file written at its dst.
	if _, err := os.Stat(filepath.Join(out, "store", "db@data", "manifests", "netpol.yml")); err != nil {
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
	if p.Releases["a@n"].ValuesFile != "" {
		t.Errorf("no values file expected when the only source is a skipped optional")
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

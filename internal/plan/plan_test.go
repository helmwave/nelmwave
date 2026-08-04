package plan

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/helmwave/nelmwave/internal/config"
)

func TestFromConfig_AndRoundTrip(t *testing.T) {
	cfg := &config.Config{
		Project: "demo",
		Repositories: map[string]config.Repository{
			"bitnami": {URL: "https://charts.example.com", ForceUpdate: true},
		},
		Releases: map[string]config.Release{
			"api@app": {
				Labels:  map[string]string{"app": "api"},
				Needs:   []string{"db@data"},
				Chart:   config.Chart{Name: "oci://r/api", Version: "1.0.0"},
				Values:  []config.FileRef{{Src: "file://v.yml"}},
				Options: config.ReleaseOptions{CreateNamespace: true},
			},
		},
	}

	p := FromConfig(cfg)
	if _, ok := p.Releases["api@app"]; !ok || len(p.Releases) != 1 {
		t.Fatalf("FromConfig produced %+v", p.Releases)
	}

	dir := t.TempDir()
	if err := p.Write(dir); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := Read(filepath.Join(dir, "missing")); err == nil {
		t.Errorf("Read of missing dir should fail")
	}

	got, err := Read(dir)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !reflect.DeepEqual(p, got) {
		t.Errorf("round-trip mismatch:\n want %+v\n got  %+v", p, got)
	}
	if !got.Repositories["bitnami"].ForceUpdate {
		t.Errorf("force_update lost across round-trip")
	}
}

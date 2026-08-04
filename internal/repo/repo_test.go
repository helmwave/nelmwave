package repo

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"testing"

	"github.com/helmwave/nelmwave/internal/config"
)

func repos() map[string]config.Repository {
	return map[string]config.Repository{
		"bitnami":              {URL: "https://charts.bitnami.com/bitnami", Username: "u", Password: "p"},
		"registry.example.com": {URL: "oci://registry.example.com", Username: "ou", Password: "op"},
	}
}

func TestResolve_HelmRepoAlias(t *testing.T) {
	r := Resolve("bitnami/postgresql", repos())
	if r.Ref != "postgresql" {
		t.Errorf("Ref = %q, want chart name postgresql", r.Ref)
	}
	if r.RepoURL != "https://charts.bitnami.com/bitnami" {
		t.Errorf("RepoURL = %q", r.RepoURL)
	}
	if r.Username != "u" || r.Password != "p" {
		t.Errorf("creds not carried: %+v", r)
	}
}

func TestResolve_OCIPassthrough(t *testing.T) {
	r := Resolve("oci://registry.example.com/charts/api", repos())
	if r.Ref != "oci://registry.example.com/charts/api" {
		t.Errorf("OCI Ref should pass through, got %q", r.Ref)
	}
	if r.RepoURL != "" {
		t.Errorf("OCI must not set RepoURL, got %q", r.RepoURL)
	}
}

func TestResolve_UnknownAliasIsLocal(t *testing.T) {
	r := Resolve("./charts/local", repos())
	if r.Ref != "./charts/local" || r.RepoURL != "" {
		t.Errorf("local path should pass through, got %+v", r)
	}
	// A repo/chart form whose alias isn't declared is left untouched too.
	r = Resolve("unknown/chart", repos())
	if r.Ref != "unknown/chart" || r.RepoURL != "" {
		t.Errorf("undeclared alias should pass through, got %+v", r)
	}
}

func TestDockerConfig_OnlyForOCIWithCreds(t *testing.T) {
	// No OCI creds -> no file.
	path, cleanup, err := DockerConfig(map[string]config.Repository{
		"bitnami": {URL: "https://x", Username: "u", Password: "p"}, // helm, ignored
		"anon":    {URL: "oci://anon.example.com"},                  // OCI without creds
	})
	defer cleanup()
	if err != nil {
		t.Fatal(err)
	}
	if path != "" {
		t.Errorf("expected no docker config, got %q", path)
	}

	// OCI with creds -> a valid docker config.json.
	path, cleanup2, err := DockerConfig(repos())
	defer cleanup2()
	if err != nil || path == "" {
		t.Fatalf("expected a docker config, path=%q err=%v", path, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Auths map[string]struct {
			Username, Password, Auth string
		} `json:"auths"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("invalid docker config json: %v", err)
	}
	entry, ok := cfg.Auths["registry.example.com"]
	if !ok {
		t.Fatalf("registry host missing; auths = %v", cfg.Auths)
	}
	want := base64.StdEncoding.EncodeToString([]byte("ou:op"))
	if entry.Auth != want || entry.Username != "ou" {
		t.Errorf("auth entry wrong: %+v", entry)
	}
}

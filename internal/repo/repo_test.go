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

// Transport settings describe how to reach the repository, so they must survive
// the resolve for both kinds of source — including OCI, which is matched by URL
// rather than by alias.
func TestResolve_TransportSettings(t *testing.T) {
	full := config.Repository{
		InsecureSkipTLSVerify: true,
		CAFile:                "/tls/ca.pem",
		CertFile:              "/tls/client.pem",
		KeyFile:               "/tls/client-key.pem",
		SkipUpdate:            true,
		RequestTimeout:        "30s",
	}

	check := func(t *testing.T, what string, r ChartResolution) {
		t.Helper()
		if !r.SkipTLSVerify || r.CAFile != "/tls/ca.pem" || r.CertFile != "/tls/client.pem" ||
			r.KeyFile != "/tls/client-key.pem" || !r.SkipUpdate || r.RequestTimeout != "30s" {
			t.Errorf("%s: transport settings lost: %+v", what, r)
		}
	}

	helm := full
	helm.URL = "https://charts.example.com"
	check(t, "helm repo", Resolve("internal/api", map[string]config.Repository{"internal": helm}))

	oci := full
	oci.URL = "oci://registry.example.com"
	check(t, "oci registry", Resolve("oci://registry.example.com/api", map[string]config.Repository{"reg": oci}))
}

// The registry's transport is part of its address: oci+http:// means no TLS.
// nelm only understands oci://, so the ref is rewritten and the choice travels
// as a flag.
func TestResolve_OCIPlainHTTPFromScheme(t *testing.T) {
	rs := map[string]config.Repository{
		"dev":  {URL: "oci+http://registry:5000"},
		"prod": {URL: "oci://registry.example.com"},
	}

	// Declared plain-HTTP registry, chart written either way.
	for _, chart := range []string{"oci+http://registry:5000/api", "oci://registry:5000/api"} {
		got := Resolve(chart, rs)
		if !got.OCIPlainHTTP {
			t.Errorf("%s: plain HTTP not detected: %+v", chart, got)
		}
		if got.Ref != "oci://registry:5000/api" {
			t.Errorf("%s: ref = %q, want the oci:// spelling nelm accepts", chart, got.Ref)
		}
	}

	// A TLS registry stays TLS.
	if got := Resolve("oci://registry.example.com/api", rs); got.OCIPlainHTTP {
		t.Errorf("oci:// registry must not be plain HTTP: %+v", got)
	}

	// An undeclared registry addressed as oci+http:// is still plain HTTP: the
	// chart reference alone is enough to say so.
	if got := Resolve("oci+http://localhost:5000/api", rs); !got.OCIPlainHTTP {
		t.Errorf("undeclared oci+http:// chart must be plain HTTP: %+v", got)
	}
}

// Basic auth is a helm-repo concept here: for OCI the credentials travel in a
// generated Docker config.json instead, so they must not leak into the spec.
func TestResolve_OCICarriesNoBasicAuth(t *testing.T) {
	r := Resolve("oci://registry.example.com/charts/api", repos())
	if r.Username != "" || r.Password != "" || r.PassCredentials {
		t.Errorf("OCI resolution must not carry basic auth: %+v", r)
	}
}

func TestResolve_ProvenanceFromHelmRepo(t *testing.T) {
	rs := repos()
	rs["bitnami"] = config.Repository{
		URL:                "https://charts.bitnami.com/bitnami",
		ProvenanceStrategy: "always",
		ProvenanceKeyring:  "/keys/pubring.gpg",
	}
	r := Resolve("bitnami/postgresql", rs)
	if r.ProvenanceStrategy != "always" || r.ProvenanceKeyring != "/keys/pubring.gpg" {
		t.Errorf("provenance not carried from the helm repo: %+v", r)
	}
}

// An OCI chart is addressed by URL, not by alias, so the repository it belongs
// to has to be found by prefix — otherwise provenance settings declared on an
// OCI registry would silently do nothing.
func TestResolve_ProvenanceFromOCIRegistryByPrefix(t *testing.T) {
	rs := map[string]config.Repository{
		"broad":  {URL: "oci://ghcr.io", ProvenanceStrategy: "if-possible"},
		"narrow": {URL: "oci://ghcr.io/acme/", ProvenanceStrategy: "always"},
		"other":  {URL: "oci://quay.io", ProvenanceStrategy: "later"},
	}

	// Longest matching prefix wins, trailing slash and all.
	if r := Resolve("oci://ghcr.io/acme/api", rs); r.ProvenanceStrategy != "always" {
		t.Errorf("strategy = %q, want always (from the narrower registry)", r.ProvenanceStrategy)
	}
	// Falls back to the broader registry when the narrow one does not match.
	if r := Resolve("oci://ghcr.io/other/api", rs); r.ProvenanceStrategy != "if-possible" {
		t.Errorf("strategy = %q, want if-possible", r.ProvenanceStrategy)
	}
	// An undeclared registry carries nothing.
	if r := Resolve("oci://docker.io/x/y", rs); r.ProvenanceStrategy != "" {
		t.Errorf("undeclared registry got strategy %q", r.ProvenanceStrategy)
	}
	// A host that merely shares a prefix is not a match.
	if r := Resolve("oci://ghcr.io.evil.com/x", rs); r.ProvenanceStrategy != "" {
		t.Errorf("lookalike host matched: %q", r.ProvenanceStrategy)
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

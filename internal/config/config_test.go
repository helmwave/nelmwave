package config

import (
	"strings"
	"testing"
)

func TestParse_DefaultsAndJSONTagBinding(t *testing.T) {
	data := []byte(`
project: demo
repositories:
  bitnami:
    url: https://charts.bitnami.com/bitnami
    pass_credentials: true
releases:
  cache@app:
    chart:
      name: bitnami/redis
      version: 20.x
`)
	cfg, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	repo, ok := cfg.Repositories["bitnami"]
	if !ok || !repo.PassCredentials {
		t.Errorf("pass_credentials should bind to true via json tag, got %+v", cfg.Repositories)
	}
	r, ok := cfg.Releases["cache@app"]
	if !ok {
		t.Fatalf("release %q missing; got %v", "cache@app", cfg.Releases)
	}
	if !r.Namespace.Create {
		t.Errorf("namespace.create should default to true, got false")
	}
	if r.Chart.Name != "bitnami/redis" || r.Chart.Version != "20.x" {
		t.Errorf("chart not parsed: %+v", r.Chart)
	}
}

func TestParse_RepositoryForms(t *testing.T) {
	cfg := mustParseValid(t, `
repositories:
  bitnami: https://charts.bitnami.com/bitnami
  ghcr.io: oci://ghcr.io
  private:
    url: oci://registry.example.com
    username: u
    password: p
releases:
  a: { chart: { name: bitnami/x } }
`)
	if got := cfg.Repositories["bitnami"].URL; got != "https://charts.bitnami.com/bitnami" {
		t.Errorf("bare-string repo url = %q", got)
	}
	if cfg.Repositories["bitnami"].IsOCI() {
		t.Errorf("https repo should not be OCI")
	}
	if !cfg.Repositories["ghcr.io"].IsOCI() {
		t.Errorf("oci:// repo should be OCI")
	}
	priv := cfg.Repositories["private"]
	if priv.URL != "oci://registry.example.com" || priv.Username != "u" || priv.Password != "p" {
		t.Errorf("full-form repo not parsed: %+v", priv)
	}
	if err := Validate(cfg); err != nil {
		t.Errorf("expected valid, got %v", err)
	}
}

func TestValidate_RepositoryURLRequired(t *testing.T) {
	cfg := mustParseValid(t, `
repositories:
  broken:
    username: u
releases:
  a: { chart: { name: r/a } }
`)
	if err := Validate(cfg); err == nil || !strings.Contains(err.Error(), "url is required") {
		t.Fatalf("expected url-required error, got: %v", err)
	}
}

// A bad strategy must be caught here: helm's downloader panics on an unknown
// value instead of erroring, so an unvalidated typo would surface as a crash
// halfway through an apply.
func TestValidate_ProvenanceStrategy(t *testing.T) {
	cfg := mustParseValid(t, `
repositories:
  bad:
    url: https://charts.example.com
    provenance_strategy: alwais
releases:
  a: { chart: { name: bad/a } }
`)
	err := Validate(cfg)
	if err == nil || !strings.Contains(err.Error(), "unknown provenance_strategy") {
		t.Fatalf("expected provenance_strategy error, got: %v", err)
	}

	for _, s := range append([]string{""}, ProvenanceStrategies...) {
		cfg := mustParseValid(t, `
repositories:
  ok:
    url: https://charts.example.com
    provenance_strategy: `+s+`
releases:
  a: { chart: { name: ok/a } }
`)
		if err := Validate(cfg); err != nil {
			t.Errorf("strategy %q should be accepted, got %v", s, err)
		}
	}
}

// The two policies nelmwave states positively must default to nelm's behaviour,
// since nelm spells them as negations (NoRemoveManualChanges, NoInstallCRDs).
func TestParse_ResourcePolicyDefaults(t *testing.T) {
	cfg := mustParseValid(t, `
releases:
  a: { chart: { name: r/a } }
  b:
    chart: { name: r/b }
    removeManualChanges: false
    installCRDs: false
    forceAdoption: true
    deletePropagation: Orphan
    historyLimit: 3
`)
	a := cfg.Releases["a"]
	if !a.RemoveManualChanges || !a.InstallCRDs {
		t.Errorf("removeManualChanges and installCRDs must default to true, got %+v", a)
	}
	if a.ForceAdoption || a.DeletePropagation != "" || a.HistoryLimit != 0 {
		t.Errorf("release a picked up unexpected policies: %+v", a)
	}

	b := cfg.Releases["b"]
	if b.RemoveManualChanges || b.InstallCRDs {
		t.Errorf("explicit false must survive the default:\"true\" tag, got %+v", b)
	}
	if !b.ForceAdoption || b.DeletePropagation != "Orphan" || b.HistoryLimit != 3 {
		t.Errorf("policies not parsed: %+v", b)
	}
	if err := Validate(cfg); err != nil {
		t.Errorf("expected valid, got %v", err)
	}
}

// nelm casts the string straight to metav1.DeletionPropagation, so the wrong
// case has to be caught here rather than by the API server.
func TestValidate_DeletePropagation(t *testing.T) {
	cfg := mustParseValid(t, `
releases:
  a:
    chart: { name: r/a }
    deletePropagation: foreground
`)
	if err := Validate(cfg); err == nil || !strings.Contains(err.Error(), "unknown deletePropagation") {
		t.Fatalf("expected deletePropagation error, got: %v", err)
	}

	for _, p := range DeletePropagations {
		cfg := mustParseValid(t, `
releases:
  a:
    chart: { name: r/a }
    deletePropagation: `+p+`
`)
		if err := Validate(cfg); err != nil {
			t.Errorf("propagation %q should be accepted, got %v", p, err)
		}
	}
}

// On a helm repository the scheme is already in the url, so oci_plain_http there
// would do nothing at all — nelm reads it only on the OCI path.
func TestValidate_OCIPlainHTTPIsOCIOnly(t *testing.T) {
	cfg := mustParseValid(t, `
repositories:
  helm:
    url: https://charts.example.com
    oci_plain_http: true
releases:
  a: { chart: { name: helm/a } }
`)
	if err := Validate(cfg); err == nil || !strings.Contains(err.Error(), "oci_plain_http only applies to oci://") {
		t.Fatalf("expected oci_plain_http error, got: %v", err)
	}

	ok := mustParseValid(t, `
repositories:
  reg:
    url: oci://registry:5000
    oci_plain_http: true
releases:
  a: { chart: { name: "oci://registry:5000/a" } }
`)
	if err := Validate(ok); err != nil {
		t.Errorf("oci_plain_http on an OCI registry should be accepted, got %v", err)
	}
}

func TestValidate_RepositoryRequestTimeout(t *testing.T) {
	cfg := mustParseValid(t, `
repositories:
  bad:
    url: https://charts.example.com
    request_timeout: 30 seconds
releases:
  a: { chart: { name: bad/a } }
`)
	if err := Validate(cfg); err == nil || !strings.Contains(err.Error(), "invalid request_timeout") {
		t.Fatalf("expected request_timeout error, got: %v", err)
	}

	ok := mustParseValid(t, `
repositories:
  ok:
    url: https://charts.example.com
    request_timeout: 30s
releases:
  a: { chart: { name: ok/a } }
`)
	if err := Validate(ok); err != nil {
		t.Errorf("30s should be accepted, got %v", err)
	}
}

func mustParseValid(t *testing.T, yml string) *Config {
	t.Helper()
	cfg, err := Parse([]byte(yml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return cfg
}

func TestValidate_OK(t *testing.T) {
	cfg := mustParseValid(t, `
releases:
  db@data:
    chart: { name: bitnami/postgresql }
  api@app:
    needs:
      releases:
        db@data: { optional: true }
    chart: { name: oci://r/api }
`)
	if err := Validate(cfg); err != nil {
		t.Fatalf("expected valid, got: %v", err)
	}
	if !cfg.Releases["api@app"].Needs.Releases["db@data"].Optional {
		t.Errorf("optional flag not parsed on needs.releases")
	}
}

func TestValidate_Errors(t *testing.T) {
	cases := map[string]struct {
		yml  string
		want string
	}{
		"missing chart.name": {
			yml:  "releases:\n  a: {}\n",
			want: "chart.name is required",
		},
		"unknown need": {
			yml:  "releases:\n  a: {chart: { name: r/a}, needs: {releases: {ghost: {}}}}\n",
			want: `needs unknown release "ghost"`,
		},
		"self need": {
			yml:  "releases:\n  a: {chart: { name: r/a}, needs: {releases: {a: {}}}}\n",
			want: "needs itself",
		},
		"need wrong namespace": {
			yml: `
releases:
  db@data: {chart: {name: r/db}}
  api@app: {chart: {name: r/api}, needs: {releases: {db@other: {}}}}
`,
			want: `needs unknown release "db@other"`,
		},
		"invalid matchLabelsExpressions operator": {
			yml: `
releases:
  a:
    chart: {name: r/a}
    needs:
      matchLabelsExpressions:
        - { key: env, operator: Bogus, values: [x] }
`,
			want: "needs labels selector",
		},
		"invalid label key": {
			yml: `
releases:
  a:
    chart: { name: r/a }
    labels: { "bad label": v }
`,
			want: "label key",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := Validate(mustParseValid(t, tc.yml))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}

func TestValidate_DetectsCycle(t *testing.T) {
	cfg := mustParseValid(t, `
releases:
  a: {chart: { name: r/a}, needs: {releases: {b: {}}}}
  b: {chart: { name: r/b}, needs: {releases: {c: {}}}}
  c: {chart: { name: r/c}, needs: {releases: {a: {}}}}
`)
	err := Validate(cfg)
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected cycle error, got: %v", err)
	}
}

func TestNeeds_LabelsResolveAndCycle(t *testing.T) {
	cfg := mustParseValid(t, `
releases:
  db@data:
    labels: { tier: db }
    chart: { name: r/db }
  api@app:
    labels: { tier: backend }
    needs:
      matchLabels: { tier: db }
    chart: { name: r/api }
`)
	if err := Validate(cfg); err != nil {
		t.Fatalf("expected valid, got: %v", err)
	}
	got, err := cfg.DirectNeeds("api@app", cfg.Releases["api@app"])
	if err != nil {
		t.Fatalf("DirectNeeds: %v", err)
	}
	if len(got) != 1 || got[0] != "db@data" {
		t.Errorf("label need should resolve to [db@data], got %v", got)
	}

	// Two releases selecting each other by label form a cycle.
	loop := mustParseValid(t, `
releases:
  a@n:
    labels: { k: a }
    needs: { matchLabels: { k: b } }
    chart: { name: r/a }
  b@n:
    labels: { k: b }
    needs: { matchLabels: { k: a } }
    chart: { name: r/b }
`)
	if err := Validate(loop); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected label cycle error, got: %v", err)
	}
}

func TestSelector_ParseAndMatch(t *testing.T) {
	sel, err := ParseSelector("app=api,env in (prod,stg),tier!=db")
	if err != nil {
		t.Fatalf("ParseSelector: %v", err)
	}
	match := Release{Labels: map[string]string{"app": "api", "env": "stg", "tier": "backend"}}
	nomatch := Release{Labels: map[string]string{"app": "api", "env": "dev", "tier": "backend"}}
	if !match.Matches(sel) {
		t.Errorf("expected match for %v", match.Labels)
	}
	if nomatch.Matches(sel) {
		t.Errorf("expected no match for %v", nomatch.Labels)
	}

	all, err := ParseSelector("")
	if err != nil {
		t.Fatalf("empty selector: %v", err)
	}
	if !match.Matches(all) {
		t.Errorf("empty selector should match everything")
	}
}

func TestParse_FileRefForms(t *testing.T) {
	cfg := mustParseValid(t, `
releases:
  a@n:
    chart: { name: r/a }
    values:
      - src: file://values/pg.yml.tpl
      - file://values/pg.yml.tpl
      - src: values/pg.yml.tpl
      - values/pg.yml.tpl
      - env:PG_VALUES
      - { src: values/opt.yml, optional: true }
`)
	vals := cfg.Releases["a@n"].Values
	if len(vals) != 6 {
		t.Fatalf("want 6 value refs, got %d: %+v", len(vals), vals)
	}
	// The first four spellings must all canonicalize to the same local path.
	for i := 0; i < 4; i++ {
		if vals[i].Src != "values/pg.yml.tpl" {
			t.Errorf("form %d: want src %q, got %q", i, "values/pg.yml.tpl", vals[i].Src)
		}
	}
	// Non-file schemes are preserved verbatim.
	if vals[4].Src != "env:PG_VALUES" {
		t.Errorf("env scheme should be preserved, got %q", vals[4].Src)
	}
	// Mapping form keeps its extra fields.
	if vals[5].Src != "values/opt.yml" || !vals[5].Optional {
		t.Errorf("mapping form lost fields: %+v", vals[5])
	}
}

func TestParse_StoreFileRefBareForm(t *testing.T) {
	cfg := mustParseValid(t, `
releases:
  a@n:
    chart: { name: r/a }
    stores:
      - file://extra/netpol.yml
`)
	stores := cfg.Releases["a@n"].Stores
	if len(stores) != 1 || stores[0].Src != "extra/netpol.yml" {
		t.Fatalf("bare store form not normalized: %+v", stores)
	}
}

func TestParse_ReleaseTypeDefaults(t *testing.T) {
	cfg := mustParseValid(t, `
Release:
  labels:
    common: true
    team: platform
  values:
    - common.yml
releases:
  a@ns:
    labels: { app: a, team: apps }
    values: [ own.yml ]
    chart: { name: r/a }
  b@ns:
    chart: { name: r/b }
`)
	// Labels deep-merge: own label wins, defaults fill the rest; bool coerces.
	a := cfg.Releases["a@ns"].Labels
	if a["common"] != "true" {
		t.Errorf("default bool label should coerce to string, got %q", a["common"])
	}
	if a["team"] != "apps" {
		t.Errorf("per-release label must win, got team=%q", a["team"])
	}
	if a["app"] != "a" {
		t.Errorf("own label lost, got %v", a)
	}
	b := cfg.Releases["b@ns"].Labels
	if b["common"] != "true" || b["team"] != "platform" {
		t.Errorf("release without own labels should inherit defaults, got %v", b)
	}

	// Values are a slice default: replaced by a release's own, used when absent.
	if av := cfg.Releases["a@ns"].Values; len(av) != 1 || av[0].Src != "own.yml" {
		t.Errorf("a values should be its own, got %v", av)
	}
	if bv := cfg.Releases["b@ns"].Values; len(bv) != 1 || bv[0].Src != "common.yml" {
		t.Errorf("b values should inherit the default, got %v", bv)
	}
}

func TestParse_NamespaceBlock(t *testing.T) {
	cfg := mustParseValid(t, `
releases:
  api@prod:
    chart: { name: r/api }
    namespace:
      create: false
      labels:
        pod-security.kubernetes.io/enforce: restricted
      annotations:
        owner: platform
`)
	ns := cfg.Releases["api@prod"].Namespace
	if ns.Create {
		t.Error("namespace.create: explicit false must survive the default:\"true\" tag")
	}
	if ns.Delete {
		t.Error("namespace.delete must default to false — it is not the mirror of create")
	}
	if ns.Labels["pod-security.kubernetes.io/enforce"] != "restricted" {
		t.Errorf("namespace labels = %v", ns.Labels)
	}
	if ns.Annotations["owner"] != "platform" {
		t.Errorf("namespace annotations = %v", ns.Annotations)
	}
	if !ns.HasMetadata() {
		t.Error("HasMetadata should be true when labels or annotations are set")
	}
}

func TestParse_NamespaceAsScalarIsRejected(t *testing.T) {
	// The namespace name lives in the release key. Written as a scalar the field
	// used to be silently dropped, which looked like it worked.
	_, err := Parse([]byte(`
releases:
  api:
    namespace: production
    chart: { name: r/api }
`))
	if err == nil {
		t.Fatal("a scalar namespace should be rejected, not ignored")
	}
	if !strings.Contains(err.Error(), "api@production") {
		t.Errorf("error should suggest the release key form, got: %v", err)
	}
}

// A top-level Namespace: block is a confijer type default, so one policy can
// cover every release without repeating it.
func TestParse_NamespaceTypeDefaultAppliesToAllReleases(t *testing.T) {
	cfg := mustParseValid(t, `
Namespace:
  labels:
    managed-by: nelmwave

releases:
  api@prod:
    chart: { name: r/api }
  web@prod:
    chart: { name: r/web }
    namespace:
      labels:
        tier: front
`)
	if got := cfg.Releases["api@prod"].Namespace.Labels["managed-by"]; got != "nelmwave" {
		t.Errorf("api should inherit the type default, got %q", got)
	}
	web := cfg.Releases["web@prod"].Namespace.Labels
	if web["tier"] != "front" || web["managed-by"] != "nelmwave" {
		t.Errorf("web labels should merge its own with the default, got %v", web)
	}
}

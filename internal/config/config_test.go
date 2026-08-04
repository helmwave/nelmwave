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
    force_update: true
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
	if !ok || !repo.ForceUpdate {
		t.Errorf("force_update should bind to true via json tag, got %+v", cfg.Repositories)
	}
	r, ok := cfg.Releases["cache@app"]
	if !ok {
		t.Fatalf("release %q missing; got %v", "cache@app", cfg.Releases)
	}
	if !r.Options.CreateNamespace {
		t.Errorf("createNamespace should default to true, got false")
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
        db@data: { strict: true }
    chart: { name: oci://r/api }
`)
	if err := Validate(cfg); err != nil {
		t.Fatalf("expected valid, got: %v", err)
	}
	if !cfg.Releases["api@app"].Needs.Releases["db@data"].Strict {
		t.Errorf("strict flag not parsed on needs.releases")
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
  a:
    namespace: n
    chart: { name: r/a }
    values:
      - src: file://values/pg.yml.tpl
      - file://values/pg.yml.tpl
      - src: values/pg.yml.tpl
      - values/pg.yml.tpl
      - env:PG_VALUES
      - { src: values/opt.yml, optional: true }
`)
	vals := cfg.Releases["a"].Values
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
  a:
    namespace: n
    chart: { name: r/a }
    stores:
      - file://extra/netpol.yml
`)
	stores := cfg.Releases["a"].Stores
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

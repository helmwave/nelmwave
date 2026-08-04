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
    store:
      - file://extra/netpol.yml
`)
	store := cfg.Releases["a"].Store
	if len(store) != 1 || store[0].Src != "extra/netpol.yml" {
		t.Fatalf("bare store form not normalized: %+v", store)
	}
}

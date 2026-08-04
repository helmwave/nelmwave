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
  cache:
    namespace: app
    universal:
      image: redis:7
      service:
        port: 6379
`)
	cfg, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	repo, ok := cfg.Repositories["bitnami"]
	if !ok || !repo.ForceUpdate {
		t.Errorf("force_update should bind to true via json tag, got %+v", cfg.Repositories)
	}
	r, ok := cfg.Releases["cache"]
	if !ok {
		t.Fatalf("release %q missing; got %v", "cache", cfg.Releases)
	}
	if !r.Options.CreateNamespace {
		t.Errorf("createNamespace should default to true, got false")
	}
	if r.Universal == nil || r.Universal.Service == nil || r.Universal.Service.Port != 6379 {
		t.Errorf("universal.service.port not parsed: %+v", r.Universal)
	}
	if r.Universal.Replicas != 1 {
		t.Errorf("universal.replicas should default to 1, got %d", r.Universal.Replicas)
	}
	if !r.UsesUniversalChart() {
		t.Errorf("release with universal block and no chart.name should use universal chart")
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
  db:
    namespace: data
    chart: { name: bitnami/postgresql }
  api:
    namespace: app
    needs: [db]
    chart: { name: oci://r/api }
`)
	if err := Validate(cfg); err != nil {
		t.Fatalf("expected valid, got: %v", err)
	}
}

func TestValidate_Errors(t *testing.T) {
	cases := map[string]struct {
		yml  string
		want string
	}{
		"no chart source": {
			yml:  "releases:\n  a: {namespace: n}\n",
			want: "either chart.name or a universal block",
		},
		"both chart and universal": {
			yml: `
releases:
  a:
    namespace: n
    chart: { name: r/a }
    universal: { image: x }
`,
			want: "mutually exclusive",
		},
		"unknown need": {
			yml:  "releases:\n  a: {namespace: n, chart: { name: r/a}, needs: [ghost]}\n",
			want: `needs unknown release "ghost"`,
		},
		"self need": {
			yml:  "releases:\n  a: {namespace: n, chart: { name: r/a}, needs: [a]}\n",
			want: "needs itself",
		},
		"missing namespace": {
			yml:  "releases:\n  a: {chart: { name: r/a}}\n",
			want: "namespace is required",
		},
		"invalid label key": {
			yml: `
releases:
  a:
    namespace: n
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
  a: {namespace: n, chart: { name: r/a}, needs: [b]}
  b: {namespace: n, chart: { name: r/b}, needs: [c]}
  c: {namespace: n, chart: { name: r/c}, needs: [a]}
`)
	err := Validate(cfg)
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected cycle error, got: %v", err)
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

func TestParse_ValueRefForms(t *testing.T) {
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

func TestParse_StoreRefBareForm(t *testing.T) {
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

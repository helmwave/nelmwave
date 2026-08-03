package config

import (
	"strings"
	"testing"
)

func TestParse_DefaultsAndJSONTagBinding(t *testing.T) {
	data := []byte(`
project: demo
repositories:
  - name: bitnami
    url: https://charts.bitnami.com/bitnami
    force_update: true
releases:
  - name: cache
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

	if len(cfg.Repositories) != 1 || !cfg.Repositories[0].ForceUpdate {
		t.Errorf("force_update should bind to true via json tag, got %+v", cfg.Repositories)
	}
	if len(cfg.Releases) != 1 {
		t.Fatalf("want 1 release, got %d", len(cfg.Releases))
	}
	r := cfg.Releases[0]
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
		t.Errorf("release with universal block and no chart.ref should use universal chart")
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
  - name: db
    namespace: data
    chart: { ref: bitnami/postgresql }
  - name: api
    namespace: app
    needs: [db]
    chart: { ref: oci://r/api }
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
		"duplicate name": {
			yml: `
releases:
  - {name: a, namespace: n, chart: {ref: r/a}}
  - {name: a, namespace: n, chart: {ref: r/a}}
`,
			want: "duplicate release name",
		},
		"no chart source": {
			yml:  "releases:\n  - {name: a, namespace: n}\n",
			want: "either chart.ref or a universal block",
		},
		"both chart and universal": {
			yml: `
releases:
  - name: a
    namespace: n
    chart: { ref: r/a }
    universal: { image: x }
`,
			want: "mutually exclusive",
		},
		"unknown need": {
			yml:  "releases:\n  - {name: a, namespace: n, chart: {ref: r/a}, needs: [ghost]}\n",
			want: `needs unknown release "ghost"`,
		},
		"self need": {
			yml:  "releases:\n  - {name: a, namespace: n, chart: {ref: r/a}, needs: [a]}\n",
			want: "needs itself",
		},
		"missing namespace": {
			yml:  "releases:\n  - {name: a, chart: {ref: r/a}}\n",
			want: "namespace is required",
		},
		"invalid label key": {
			yml: `
releases:
  - name: a
    namespace: n
    chart: { ref: r/a }
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
  - {name: a, namespace: n, chart: {ref: r/a}, needs: [b]}
  - {name: b, namespace: n, chart: {ref: r/b}, needs: [c]}
  - {name: c, namespace: n, chart: {ref: r/c}, needs: [a]}
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

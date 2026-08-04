package config

import "testing"

func TestParseUniqname(t *testing.T) {
	cases := []struct {
		key                  string
		name, namespace, ctx string
		canonical            string
		wantErr              bool
	}{
		{key: "api", name: "api", canonical: "api"},
		{key: "api@app", name: "api", namespace: "app", canonical: "api@app"},
		{key: "api@app@prod", name: "api", namespace: "app", ctx: "prod", canonical: "api@app@prod"},
		// kube-context may itself contain '@'.
		{key: "api@app@admin@cluster", name: "api", namespace: "app", ctx: "admin@cluster", canonical: "api@app@admin@cluster"},
		// namespace omitted but context set round-trips through "@@".
		{key: "api@@prod", name: "api", ctx: "prod", canonical: "api@@prod"},
		// trailing empty segments collapse.
		{key: "api@", name: "api", canonical: "api"},
		{key: "", wantErr: true},
		{key: "@app", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			u, err := ParseUniqname(tc.key)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error for %q, got %+v", tc.key, u)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseUniqname(%q): %v", tc.key, err)
			}
			if u.Name != tc.name || u.Namespace != tc.namespace || u.KubeContext != tc.ctx {
				t.Errorf("parsed %+v, want name=%q ns=%q ctx=%q", u, tc.name, tc.namespace, tc.ctx)
			}
			if got := u.String(); got != tc.canonical {
				t.Errorf("String() = %q, want %q", got, tc.canonical)
			}
		})
	}
}

func TestParse_CanonicalizesKeysAndNeeds(t *testing.T) {
	// "api@" collapses to "api"; the need "postgres@data@" collapses to
	// "postgres@data" and must resolve.
	cfg := mustParseValid(t, `
releases:
  postgres@data:
    chart: { name: r/pg }
  api@:
    chart: { name: r/api }
    needs:
      releases:
        postgres@data@: {}
`)
	if _, ok := cfg.Releases["api"]; !ok {
		t.Fatalf(`key "api@" should normalize to "api"; got keys %v`, keys(cfg.Releases))
	}
	if _, ok := cfg.Releases["api"].Needs.Releases["postgres@data"]; !ok {
		t.Errorf("need key not canonicalized, got %v", cfg.Releases["api"].Needs.Releases)
	}
	if err := Validate(cfg); err != nil {
		t.Errorf("expected valid after canonicalization, got: %v", err)
	}
}

func TestParse_DuplicateUniqnameCollision(t *testing.T) {
	_, err := Parse([]byte(`
releases:
  api: {chart: {name: r/a}}
  api@: {chart: {name: r/b}}
`))
	if err == nil {
		t.Fatal("expected collision error for api and api@")
	}
}

func keys(m map[string]Release) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

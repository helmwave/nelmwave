package config

import (
	"strings"
	"testing"
)

func TestParseDriverURL(t *testing.T) {
	cases := map[string]struct {
		raw         string
		driver      string
		conn        string
		hasPassword bool
	}{
		"empty means nelm's default": {raw: "", driver: ""},
		"secrets":                    {raw: "kubernetes://secrets", driver: "secrets"},
		"secret singular":            {raw: "kubernetes://secret", driver: "secrets"},
		"configmaps":                 {raw: "kubernetes://configmaps", driver: "configmaps"},
		"configmap singular":         {raw: "kubernetes://configmap", driver: "configmaps"},
		"uppercase object":           {raw: "kubernetes://Secrets", driver: "secrets"},
		// psql:// is nelmwave's spelling; lib/pq only understands postgres://.
		"psql is rewritten": {
			raw:    "psql://nelm@db.internal:5432/nelm?sslmode=require",
			driver: "sql",
			conn:   "postgres://nelm@db.internal:5432/nelm?sslmode=require",
		},
		"postgres passes through": {
			raw:    "postgres://nelm@db/nelm",
			driver: "sql",
			conn:   "postgres://nelm@db/nelm",
		},
		"postgresql alias": {
			raw:    "postgresql://nelm@db/nelm",
			driver: "sql",
			conn:   "postgres://nelm@db/nelm",
		},
		"embedded password is reported": {
			raw:         "psql://nelm:hunter2@db/nelm",
			driver:      "sql",
			conn:        "postgres://nelm:hunter2@db/nelm",
			hasPassword: true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := ParseDriverURL(tc.raw)
			if err != nil {
				t.Fatalf("ParseDriverURL(%q): %v", tc.raw, err)
			}
			if got.Driver != tc.driver {
				t.Errorf("driver = %q, want %q", got.Driver, tc.driver)
			}
			if got.SQLConnection != tc.conn {
				t.Errorf("connection = %q, want %q", got.SQLConnection, tc.conn)
			}
			if got.HasPassword != tc.hasPassword {
				t.Errorf("hasPassword = %v, want %v", got.HasPassword, tc.hasPassword)
			}
		})
	}
}

func TestParseDriverURL_Errors(t *testing.T) {
	cases := map[string]string{
		"kubernetes://pods": "unknown kubernetes object",
		"mysql://db/nelm":   "unknown scheme",
		"secrets":           "unknown scheme",
		"kubernetes://":     "unknown kubernetes object",
		// nelm has a memory driver, but state that dies with the process cannot
		// work across nelmwave invocations, so it is not offered.
		"memory://":           "unknown scheme",
		"kubernetes://memory": "unknown kubernetes object",
	}

	for raw, want := range cases {
		t.Run(raw, func(t *testing.T) {
			_, err := ParseDriverURL(raw)
			if err == nil {
				t.Fatalf("ParseDriverURL(%q) should fail", raw)
			}
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not mention %q", err, want)
			}
		})
	}
}

func TestValidate_DriverURL(t *testing.T) {
	cfg := mustParseValid(t, `
releases:
  a:
    chart: { name: r/a }
    driverURL: kubernetes://pods
`)
	if err := Validate(cfg); err == nil || !strings.Contains(err.Error(), "unknown kubernetes object") {
		t.Fatalf("expected driverURL error, got: %v", err)
	}

	// Set once for every release through the type-default block.
	ok := mustParseValid(t, `
Release:
  driverURL: kubernetes://configmaps
releases:
  a: { chart: { name: r/a } }
  b: { chart: { name: r/b } }
`)
	if err := Validate(ok); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
	for _, key := range []string{"a", "b"} {
		if got := ok.Releases[key].DriverURL; got != "kubernetes://configmaps" {
			t.Errorf("release %q driverURL = %q, want the Release: default", key, got)
		}
	}
}

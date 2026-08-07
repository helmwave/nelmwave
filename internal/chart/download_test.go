package chart

import (
	"testing"

	helmdownloader "github.com/werf/nelm/pkg/helm/pkg/downloader"
)

func TestIsRemote(t *testing.T) {
	// The rule is nelm's own: only a path spelled as one is local. A bare name
	// and an alias/chart pair are repository references, not directories.
	cases := map[string]bool{
		"bitnami/redis":            true,
		"redis":                    true,
		"oci://ghcr.io/acme/redis": true,
		"oci+http://reg:5000/x":    true,
		"./charts/mine":            false,
		"../mine":                  false,
		".":                        false,
		"/srv/charts/mine":         false,
	}
	for ref, want := range cases {
		if got := IsRemote(ref); got != want {
			t.Errorf("IsRemote(%q) = %v, want %v", ref, got, want)
		}
	}
}

func TestVerificationStrategy_EmptyMeansNever(t *testing.T) {
	// helm's own mapping panics on an unknown value, empty string included, and
	// an unset provenanceStrategy is the common case.
	if got := verificationStrategy(""); got != helmdownloader.VerifyNever {
		t.Errorf("empty strategy = %v, want VerifyNever", got)
	}
	if got := verificationStrategy("always"); got != helmdownloader.VerifyAlways {
		t.Errorf("always = %v, want VerifyAlways", got)
	}
}

func TestSameHost(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"https://charts.example.com", "https://charts.example.com/redis-1.0.0.tgz", true},
		{"https://charts.example.com", "https://cdn.example.com/redis-1.0.0.tgz", false},
		{"https://charts.example.com", "http://charts.example.com/redis-1.0.0.tgz", false},
		{"://nonsense", "https://charts.example.com", false},
	}
	for _, c := range cases {
		if got := sameHost(c.a, c.b); got != c.want {
			t.Errorf("sameHost(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

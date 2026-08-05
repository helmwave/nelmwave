// Package repo resolves a release's chart reference against the declared
// repositories, producing the connection settings nelm needs to fetch it.
//
// nelm has no repositories.yaml step: a helm-repo chart is fetched helm
// "--repo" style (chart name + repo URL), while an OCI chart is fetched by its
// oci:// URL with credentials supplied through a Docker config.json.
package repo

import (
	"strings"

	"github.com/helmwave/nelmwave/internal/config"
)

// ChartResolution is how to fetch one release's chart.
type ChartResolution struct {
	// Ref is what to hand nelm as the chart (chart name for a helm repo, the
	// oci:// URL for OCI, or the original path for a local chart).
	Ref string
	// RepoURL is the helm chart-repository URL (empty for OCI/local).
	RepoURL string
	// Username / Password authenticate to a helm repo. For OCI these are carried
	// via a Docker config.json instead (see DockerConfig).
	Username string
	Password string
	// PassCredentials forwards basic auth beyond the repo host. Helm repos only:
	// OCI never sees Username/Password here.
	PassCredentials bool
	// Transport and verification settings of the repository the chart comes
	// from. These apply to OCI too: nelm downloads both kinds of chart through
	// the same helm getter, so the TLS material and timeout are shared.
	SkipTLSVerify      bool
	CAFile             string
	CertFile           string
	KeyFile            string
	OCIPlainHTTP       bool
	SkipUpdate         bool
	RequestTimeout     string
	ProvenanceStrategy string
	ProvenanceKeyring  string
}

// transport copies the settings that describe how to reach a repository, as
// opposed to who we log in as. Shared by the helm-repo and OCI paths.
func (c *ChartResolution) transport(r config.Repository) {
	c.SkipTLSVerify = r.InsecureSkipTLSVerify
	c.CAFile = r.CAFile
	c.CertFile = r.CertFile
	c.KeyFile = r.KeyFile
	c.OCIPlainHTTP = c.OCIPlainHTTP || r.IsOCIPlainHTTP()
	c.SkipUpdate = r.SkipUpdate
	c.RequestTimeout = r.RequestTimeout
	c.ProvenanceStrategy = r.ProvenanceStrategy
	c.ProvenanceKeyring = r.ProvenanceKeyring
}

// Resolve maps chartName plus the declared repositories to a ChartResolution:
//   - oci://host/... or oci+http://host/... -> OCI. Ref is normalized to oci://
//     (nelm knows no other scheme) and OCIPlainHTTP records which one it was.
//     The registry is found by address, ignoring the scheme, and contributes its
//     transport settings — credentials excepted, they reach nelm through a
//     Docker config.json (see DockerConfig).
//   - alias/chart, where alias is a declared helm repo -> Ref = chart name,
//     RepoURL + auth + transport from that repository.
//   - anything else -> passed through unchanged (local path or bare name).
func Resolve(chartName string, repos map[string]config.Repository) ChartResolution {
	res := ChartResolution{Ref: chartName}
	if addr, ok := ociAddress(chartName); ok {
		// nelm and helm accept only oci://, so the plain-HTTP spelling is
		// rewritten and carried as a flag instead.
		res.Ref = config.OCIScheme + addr
		res.OCIPlainHTTP = strings.HasPrefix(chartName, config.OCIPlainHTTPScheme)
		if r, found := matchOCI(addr, repos); found {
			res.transport(r)
		}
		return res
	}
	if alias, chart, ok := strings.Cut(chartName, "/"); ok {
		if r, found := repos[alias]; found && !r.IsOCI() {
			res.Ref = chart
			res.RepoURL = r.URL
			res.Username = r.Username
			res.Password = r.Password
			res.PassCredentials = r.PassCredentials
			res.transport(r)
			return res
		}
	}
	return res
}

// ociAddress strips an OCI scheme, returning the bare host/path and whether the
// reference was an OCI one at all. Comparing addresses rather than whole URLs
// lets a chart written as oci:// match a registry declared as oci+http:// — the
// scheme is transport, not identity.
func ociAddress(ref string) (string, bool) {
	if rest, ok := strings.CutPrefix(ref, config.OCIScheme); ok {
		return rest, true
	}
	if rest, ok := strings.CutPrefix(ref, config.OCIPlainHTTPScheme); ok {
		return rest, true
	}
	return "", false
}

// matchOCI finds the declared OCI registry a chart address belongs to. An OCI
// chart is addressed by its URL rather than by an alias, so the registry is
// found by address prefix. The longest match wins, so a registry declared as
// oci://ghcr.io/acme beats a broader oci://ghcr.io; ties are broken by name,
// since map iteration order is not stable.
func matchOCI(addr string, repos map[string]config.Repository) (config.Repository, bool) {
	var best config.Repository
	var bestName string
	bestLen := -1
	for name, r := range repos {
		repoAddr, ok := ociAddress(r.URL)
		if !ok {
			continue
		}
		prefix := strings.TrimSuffix(repoAddr, "/")
		if addr != prefix && !strings.HasPrefix(addr, prefix+"/") {
			continue
		}
		switch {
		case len(prefix) > bestLen:
			best, bestName, bestLen = r, name, len(prefix)
		case len(prefix) == bestLen && name < bestName:
			best, bestName = r, name
		}
	}
	return best, bestLen >= 0
}

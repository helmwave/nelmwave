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
	c.OCIPlainHTTP = r.OCIPlainHTTP
	c.SkipUpdate = r.SkipUpdate
	c.RequestTimeout = r.RequestTimeout
	c.ProvenanceStrategy = r.ProvenanceStrategy
	c.ProvenanceKeyring = r.ProvenanceKeyring
}

// Resolve maps chartName plus the declared repositories to a ChartResolution:
//   - oci://host/... -> OCI (Ref = full URL); the registry is found by URL
//     prefix and contributes its transport settings. Credentials are the
//     exception: they reach nelm through a Docker config.json (see DockerConfig).
//   - alias/chart, where alias is a declared helm repo -> Ref = chart name,
//     RepoURL + auth + transport from that repository.
//   - anything else -> passed through unchanged (local path or bare name).
func Resolve(chartName string, repos map[string]config.Repository) ChartResolution {
	res := ChartResolution{Ref: chartName}
	if strings.HasPrefix(chartName, "oci://") {
		if r, found := matchOCI(chartName, repos); found {
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

// matchOCI finds the declared OCI repository an oci:// chart reference belongs
// to. An OCI chart is addressed by its full URL rather than by an alias, so the
// repository is found by URL prefix. The longest match wins, so a repository
// declared as oci://ghcr.io/acme beats a broader oci://ghcr.io; ties are broken
// by name, since map iteration order is not stable.
func matchOCI(chartName string, repos map[string]config.Repository) (config.Repository, bool) {
	var best config.Repository
	var bestName string
	found := false
	for name, r := range repos {
		if !r.IsOCI() {
			continue
		}
		prefix := strings.TrimSuffix(r.URL, "/")
		if chartName != prefix && !strings.HasPrefix(chartName, prefix+"/") {
			continue
		}
		switch {
		case !found, len(prefix) > len(strings.TrimSuffix(best.URL, "/")):
			best, bestName, found = r, name, true
		case len(prefix) == len(strings.TrimSuffix(best.URL, "/")) && name < bestName:
			best, bestName = r, name
		}
	}
	return best, found
}

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
	// TLS / credential-passing knobs for helm repos.
	SkipTLSVerify   bool
	PassCredentials bool
	CAFile          string
}

// Resolve maps chartName plus the declared repositories to a ChartResolution:
//   - oci://host/... -> OCI (Ref = full URL); credentials come from a matching
//     repository via DockerConfig, not from ChartResolution.
//   - alias/chart, where alias is a declared helm repo -> Ref = chart name,
//     RepoURL + auth from that repository.
//   - anything else -> passed through unchanged (local path or bare name).
func Resolve(chartName string, repos map[string]config.Repository) ChartResolution {
	if strings.HasPrefix(chartName, "oci://") {
		return ChartResolution{Ref: chartName}
	}
	if alias, chart, ok := strings.Cut(chartName, "/"); ok {
		if r, found := repos[alias]; found && !r.IsOCI() {
			return ChartResolution{
				Ref:             chart,
				RepoURL:         r.URL,
				Username:        r.Username,
				Password:        r.Password,
				SkipTLSVerify:   r.InsecureSkipTLSVerify,
				PassCredentials: r.PassCredentials,
				CAFile:          r.CAFile,
			}
		}
	}
	return ChartResolution{Ref: chartName}
}

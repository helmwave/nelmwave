// Package chart fetches a remote chart into the build artifact, so that a plan
// built where the registries are reachable can be applied where they are not.
//
// It is `helm pull` done through the very getters nelm uses at apply time (the
// helm SDK vendored inside nelm), so a chart that resolves during build resolves
// the same way here. The published archive is written as-is: whatever it carries
// in charts/ travels with it, but a chart that expects its dependencies to be
// fetched while rendering still needs its repositories reachable.
package chart

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/werf/nelm/pkg/helm/pkg/cli"
	helmdownloader "github.com/werf/nelm/pkg/helm/pkg/downloader"
	helmgetter "github.com/werf/nelm/pkg/helm/pkg/getter"
	"github.com/werf/nelm/pkg/helm/pkg/helmpath"
	helmregistry "github.com/werf/nelm/pkg/helm/pkg/registry"
	helmrepo "github.com/werf/nelm/pkg/helm/pkg/repo"
)

// Options describe one chart to fetch and how to reach the repository it comes
// from. They mirror repo.ChartResolution — the same settings nelm would be
// handed at apply time — plus the version and the OCI credentials file.
type Options struct {
	// Ref is the chart as nelm would receive it: a bare chart name when RepoURL
	// is set, an oci:// URL for a registry, a plain name otherwise.
	Ref string
	// Version is a version or constraint ("15.x"); empty means latest.
	Version string

	// RepoURL is the helm chart-repository URL; empty for OCI.
	RepoURL  string
	Username string
	Password string
	// PassCredentials forwards basic auth beyond the repository host.
	PassCredentials bool

	SkipTLSVerify  bool
	CAFile         string
	CertFile       string
	KeyFile        string
	PlainHTTP      bool
	RequestTimeout time.Duration

	// ProvenanceStrategy / ProvenanceKeyring verify the chart's PGP signature
	// while it is fetched. Empty strategy means "never", as in nelm.
	ProvenanceStrategy string
	ProvenanceKeyring  string

	// RegistryConfigPath is a Docker config.json with OCI registry credentials;
	// empty falls back to helm's default (~/.docker/config.json).
	RegistryConfigPath string

	// Out receives the downloader's warnings; nil discards them.
	Out io.Writer
}

// Download fetches the chart into dir, creating dir if needed, and returns the
// path of the file it wrote (dir/<name>-<version>.tgz). With a provenance
// strategy set, the .prov file lands next to it.
func Download(o Options, dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create chart dir %q: %w", dir, err)
	}
	// helm's index cache is written to unconditionally while a repository index
	// is fetched, and it is not created on demand.
	cacheDir := cli.EnvOr("HELM_REPOSITORY_CACHE", helmpath.CachePath("repository"))
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", fmt.Errorf("create repository cache %q: %w", cacheDir, err)
	}

	dl, ref, err := downloader(o, cacheDir)
	if err != nil {
		return "", err
	}
	path, _, err := dl.DownloadTo(ref, o.Version, dir)
	if err != nil {
		return "", fmt.Errorf("download chart %q: %w", ref, err)
	}
	return path, nil
}

// downloader assembles helm's ChartDownloader for o and returns it together
// with the reference to hand DownloadTo. It mirrors nelm's own chart downloader
// (nelm/pkg/chart/chart_download.go), which is unexported.
func downloader(o Options, cacheDir string) (*helmdownloader.ChartDownloader, string, error) {
	out := o.Out
	if out == nil {
		out = io.Discard
	}

	regOpts := []helmregistry.ClientOption{
		helmregistry.ClientOptWriter(out),
		helmregistry.ClientOptCredentialsFile(o.RegistryConfigPath),
	}
	if o.PlainHTTP {
		regOpts = append(regOpts, helmregistry.ClientOptPlainHTTP())
	}
	registryClient, err := helmregistry.NewClient(regOpts...)
	if err != nil {
		return nil, "", fmt.Errorf("construct registry client: %w", err)
	}

	getters := helmgetter.Providers{helmgetter.HttpProvider, helmgetter.OCIProvider}
	dl := &helmdownloader.ChartDownloader{
		Out: out,
		// ToVerificationStrategy panics on anything it does not know, empty
		// string included; nelm defaults it the same way.
		Verify:  verificationStrategy(o.ProvenanceStrategy),
		Keyring: o.ProvenanceKeyring,
		Getters: getters,
		Options: []helmgetter.Option{
			helmgetter.WithPassCredentialsAll(o.PassCredentials),
			helmgetter.WithTLSClientConfig(o.CertFile, o.KeyFile, o.CAFile),
			helmgetter.WithInsecureSkipVerifyTLS(o.SkipTLSVerify),
			helmgetter.WithPlainHTTP(o.PlainHTTP),
			helmgetter.WithRegistryClient(registryClient),
			helmgetter.WithTimeout(o.RequestTimeout),
		},
		RegistryClient:   registryClient,
		RepositoryConfig: cli.EnvOr("HELM_REPOSITORY_CONFIG", helmpath.ConfigPath("repositories.yaml")),
		RepositoryCache:  cacheDir,
	}

	if o.RepoURL == "" {
		dl.Options = append(dl.Options, helmgetter.WithBasicAuth(o.Username, o.Password))
		return dl, o.Ref, nil
	}

	// A helm repository is addressed by name plus URL, so the chart's own URL
	// has to come out of the repository index first.
	chartURL, err := helmrepo.FindChartInAuthAndTLSAndPassRepoURL(o.RepoURL, o.Username, o.Password,
		o.Ref, o.Version, o.CertFile, o.KeyFile, o.CAFile, o.SkipTLSVerify, o.PassCredentials, getters)
	if err != nil {
		return nil, "", fmt.Errorf("find chart %q in repository %q: %w", o.Ref, o.RepoURL, err)
	}

	// Credentials follow the chart only while it stays on the repository's own
	// host, unless passCredentials says otherwise: an index may well point at a
	// third-party download URL.
	if o.PassCredentials || sameHost(o.RepoURL, chartURL) {
		dl.Options = append(dl.Options, helmgetter.WithBasicAuth(o.Username, o.Password))
	} else {
		dl.Options = append(dl.Options, helmgetter.WithBasicAuth("", ""))
	}
	return dl, chartURL, nil
}

// verificationStrategy maps a manifest provenance strategy onto helm's, turning
// the empty default into "never" rather than letting helm panic on it.
func verificationStrategy(strategy string) helmdownloader.VerificationStrategy {
	if strategy == "" {
		strategy = string(helmdownloader.VerificationStrategyStringNever)
	}
	return helmdownloader.VerificationStrategyString(strategy).ToVerificationStrategy()
}

// sameHost reports whether two URLs share scheme and host. Unparsable input
// counts as "not the same host", the safe answer: credentials stay put.
func sameHost(a, b string) bool {
	ua, err := url.Parse(a)
	if err != nil {
		return false
	}
	ub, err := url.Parse(b)
	if err != nil {
		return false
	}
	return ua.Scheme == ub.Scheme && ua.Host == ub.Host
}

// IsRemote reports whether a chart reference has to be fetched, i.e. whether it
// is anything other than a filesystem path. It follows nelm's own rule
// (nelm/pkg/chart: isLocalChart): only an absolute path or one spelled
// "./"/"../" is local — "repo/chart" and "oci://..." are not.
func IsRemote(ref string) bool {
	return !filepath.IsAbs(ref) && !strings.HasPrefix(ref, ".")
}

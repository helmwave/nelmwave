package config

import "strings"

// Repository is a chart source keyed by alias/host in Config.Repositories. The
// URL scheme says what it is and how to reach it: https:// (or http://) is a
// classic Helm repository, oci:// an OCI registry over TLS, oci+http:// one
// without.
//
// In the manifest a repository may be written as a bare URL string or as a full
// object; the bare form is expanded to {url: "..."} during parsing:
//
//	repositories:
//	  bitnami: https://charts.bitnami.com/bitnami   # bare URL
//	  ghcr.io: oci://ghcr.io                         # bare OCI URL
//	  dev: oci+http://registry:5000                  # OCI without TLS
//	  private:                                       # full form (needs auth)
//	    url: oci://registry.example.com
//	    username: [[ .Env.REGISTRY_USER ]]
//	    password: [[ .Env.REGISTRY_PASS ]]
type Repository struct {
	// URL is the repository index URL (https://...) or OCI registry
	// (oci://... / oci+http://...).
	URL string `json:"url" yaml:"url"`
	// Username / Password are optional basic-auth credentials.
	Username string `json:"username" yaml:"username,omitempty"`
	Password string `json:"password" yaml:"password,omitempty"`
	// InsecureSkipTLSVerify disables TLS verification for this repo.
	InsecureSkipTLSVerify bool `json:"insecureSkipTLSVerify" yaml:"insecureSkipTLSVerify,omitempty"`
	// PassCredentials forwards credentials to all domains, not just the repo host.
	PassCredentials bool `json:"passCredentials" yaml:"passCredentials,omitempty"`
	// CAFile is a path to a CA bundle for this repo.
	CAFile string `json:"caFile" yaml:"caFile,omitempty"`
	// CertFile / KeyFile are the client TLS certificate and key presented to the
	// repository (mTLS). CAFile says whom we trust; these say who we are.
	CertFile string `json:"certFile" yaml:"certFile,omitempty"`
	KeyFile  string `json:"keyFile" yaml:"keyFile,omitempty"`
	// SkipUpdate stops the chart's declared dependencies from being refreshed
	// against the repository before they are pulled. It only affects charts with
	// a dependencies: section — the chart itself is fetched either way.
	SkipUpdate bool `json:"skipUpdate" yaml:"skipUpdate,omitempty"`
	// RequestTimeout bounds a single request to the repository, e.g. "30s".
	// Empty means no per-request limit (the release timeout still applies).
	RequestTimeout string `json:"requestTimeout" yaml:"requestTimeout,omitempty"`
	// ProvenanceStrategy decides whether a chart's PGP signature (its .prov
	// file) is verified before the chart is used: never (nelm's default),
	// if-possible, always, later. Empty means never.
	ProvenanceStrategy string `json:"provenanceStrategy" yaml:"provenanceStrategy,omitempty"`
	// ProvenanceKeyring is the path to a keyring with the public keys the
	// signature is checked against. Empty leaves helm's default
	// (~/.gnupg/pubring.gpg).
	ProvenanceKeyring string `json:"provenanceKeyring" yaml:"provenanceKeyring,omitempty"`
}

// ProvenanceStrategies are the values ProvenanceStrategy accepts, mirroring
// nelm's (helm's) verification strategies. Empty is allowed too and leaves
// nelm's default in place.
var ProvenanceStrategies = []string{"never", "if-possible", "always", "later"}

// OCI URL schemes. nelm and helm only know oci://; oci+http:// is nelmwave's
// spelling for "same thing, no TLS", so that a registry's transport is part of
// its address instead of a separate flag.
const (
	OCIScheme          = "oci://"
	OCIPlainHTTPScheme = "oci+http://"
)

// IsOCI reports whether this repository is an OCI registry, with or without TLS.
func (r Repository) IsOCI() bool {
	return strings.HasPrefix(r.URL, OCIScheme) || strings.HasPrefix(r.URL, OCIPlainHTTPScheme)
}

// IsOCIPlainHTTP reports whether this registry is reached over http://.
func (r Repository) IsOCIPlainHTTP() bool {
	return strings.HasPrefix(r.URL, OCIPlainHTTPScheme)
}

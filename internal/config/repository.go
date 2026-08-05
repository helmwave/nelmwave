package config

import "strings"

// Repository is a chart source keyed by alias/host in Config.Repositories. It
// covers both a classic Helm repository (https://) and an OCI registry
// (oci://); the URL scheme decides which (see IsOCI).
//
// In the manifest a repository may be written as a bare URL string or as a full
// object; the bare form is expanded to {url: "..."} during parsing:
//
//	repositories:
//	  bitnami: https://charts.bitnami.com/bitnami   # bare URL
//	  ghcr.io: oci://ghcr.io                         # bare OCI URL
//	  private:                                       # full form (needs auth)
//	    url: oci://registry.example.com
//	    username: [[ .Env.REGISTRY_USER ]]
//	    password: [[ .Env.REGISTRY_PASS ]]
type Repository struct {
	// URL is the repository index URL (https://...) or OCI registry (oci://...).
	URL string `json:"url" yaml:"url"`
	// Username / Password are optional basic-auth credentials.
	Username string `json:"username" yaml:"username,omitempty"`
	Password string `json:"password" yaml:"password,omitempty"`
	// InsecureSkipTLSVerify disables TLS verification for this repo.
	InsecureSkipTLSVerify bool `json:"insecure_skip_tls_verify" yaml:"insecure_skip_tls_verify,omitempty"`
	// PassCredentials forwards credentials to all domains, not just the repo host.
	PassCredentials bool `json:"pass_credentials" yaml:"pass_credentials,omitempty"`
	// CAFile is a path to a CA bundle for this repo.
	CAFile string `json:"ca_file" yaml:"ca_file,omitempty"`
	// CertFile / KeyFile are the client TLS certificate and key presented to the
	// repository (mTLS). CAFile says whom we trust; these say who we are.
	CertFile string `json:"cert_file" yaml:"cert_file,omitempty"`
	KeyFile  string `json:"key_file" yaml:"key_file,omitempty"`
	// OCIPlainHTTP reaches an OCI registry over http:// instead of https://.
	// The name says oci_ because that is the only place it applies, and not for
	// lack of plumbing: "oci://" addresses an artifact, it does not name a
	// transport, so the client defaults to HTTPS and needs telling otherwise. A
	// helm repository carries its scheme in the URL, which is why Validate
	// rejects this field there rather than ignoring it.
	OCIPlainHTTP bool `json:"oci_plain_http" yaml:"oci_plain_http,omitempty"`
	// SkipUpdate stops the chart's declared dependencies from being refreshed
	// against the repository before they are pulled. It only affects charts with
	// a dependencies: section — the chart itself is fetched either way.
	SkipUpdate bool `json:"skip_update" yaml:"skip_update,omitempty"`
	// RequestTimeout bounds a single request to the repository, e.g. "30s".
	// Empty means no per-request limit (the release timeout still applies).
	RequestTimeout string `json:"request_timeout" yaml:"request_timeout,omitempty"`
	// ProvenanceStrategy decides whether a chart's PGP signature (its .prov
	// file) is verified before the chart is used: never (nelm's default),
	// if-possible, always, later. Empty means never.
	ProvenanceStrategy string `json:"provenance_strategy" yaml:"provenance_strategy,omitempty"`
	// ProvenanceKeyring is the path to a keyring with the public keys the
	// signature is checked against. Empty leaves helm's default
	// (~/.gnupg/pubring.gpg).
	ProvenanceKeyring string `json:"provenance_keyring" yaml:"provenance_keyring,omitempty"`
}

// ProvenanceStrategies are the values ProvenanceStrategy accepts, mirroring
// nelm's (helm's) verification strategies. Empty is allowed too and leaves
// nelm's default in place.
var ProvenanceStrategies = []string{"never", "if-possible", "always", "later"}

// IsOCI reports whether this repository is an OCI registry (oci:// URL).
func (r Repository) IsOCI() bool {
	return strings.HasPrefix(r.URL, "oci://")
}

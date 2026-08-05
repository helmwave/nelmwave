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
}

// IsOCI reports whether this repository is an OCI registry (oci:// URL).
func (r Repository) IsOCI() bool {
	return strings.HasPrefix(r.URL, "oci://")
}

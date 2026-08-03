package config

// Repository is a classic Helm chart repository.
type Repository struct {
	// Name is the local repo alias used in chart refs (name/chart).
	Name string `json:"name" yaml:"name"`
	// URL is the repository index URL.
	URL string `json:"url" yaml:"url"`
	// Username / Password are optional basic-auth credentials.
	Username string `json:"username" yaml:"username"`
	Password string `json:"password" yaml:"password"`
	// ForceUpdate re-fetches the repo index even if cached.
	ForceUpdate bool `json:"force_update" yaml:"force_update"`
	// InsecureSkipTLSVerify disables TLS verification for this repo.
	InsecureSkipTLSVerify bool `json:"insecure_skip_tls_verify" yaml:"insecure_skip_tls_verify"`
	// PassCredentials forwards credentials to all domains, not just the repo host.
	PassCredentials bool `json:"pass_credentials" yaml:"pass_credentials"`
	// CAFile is a path to a CA bundle for this repo.
	CAFile string `json:"ca_file" yaml:"ca_file"`
}

package config

// Registry is an OCI registry used to pull oci:// charts. It is keyed by host
// in Config.Registries, so it carries no host field of its own.
type Registry struct {
	// Username / Password are optional; empty means anonymous access.
	Username string `json:"username" yaml:"username"`
	Password string `json:"password" yaml:"password"`
}

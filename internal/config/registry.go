package config

// Registry is an OCI registry used to pull oci:// charts.
type Registry struct {
	// Host is the registry host, e.g. registry.example.com.
	Host string `json:"host" yaml:"host"`
	// Username / Password are optional; empty means anonymous access.
	Username string `json:"username" yaml:"username"`
	Password string `json:"password" yaml:"password"`
}

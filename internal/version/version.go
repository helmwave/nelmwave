// Package version exposes build-time version information for the nelmwave binary.
package version

// These variables are overridden at build time via -ldflags, e.g.:
//
//	go build -ldflags "-X github.com/helmwave/nelmwave/internal/version.Version=v0.1.0"
var (
	// Version is the semantic version of the binary.
	Version = "dev"
	// Commit is the git commit the binary was built from.
	Commit = "none"
	// Date is the build date. When not set via -ldflags it falls back to today.
	Date = "unknown"
)

// String returns a human-readable version line.
func String() string {
	return Version + " (commit " + Commit + ", built " + Date + ")"
}

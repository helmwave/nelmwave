// Package config defines the nelmwave.yml schema and loads it, after gomplate
// rendering, into Go structures via confijer (type-aware defaults).
//
// confijer binds config keys via the `json` struct tag (case-insensitively)
// and ignores `yaml` tags; the `yaml` tags here are used only when the plan
// package serializes these structs to .nelmwave/planfile.yml. Keep both tags in
// sync on every field.
//
// Repositories and Releases are maps keyed by identity (repo name / release
// uniqname) rather than lists — the key is the single source of truth for the
// name, so the value structs carry no name field.
package config

// Config is the root of a nelmwave manifest.
type Config struct {
	// Project is a free-form name for the whole platform.
	Project string `json:"project" yaml:"project"`
	// Repositories are chart sources keyed by alias/host. Helm repos (https://)
	// and OCI registries (oci://) live together, distinguished by URL scheme;
	// a value may be a bare URL string or a full object (see Repository).
	Repositories map[string]Repository `json:"repositories" yaml:"repositories"`
	// Releases are the units nelmwave deploys, keyed by release name.
	Releases map[string]Release `json:"releases" yaml:"releases"`
	// Values are global value sources merged beneath every release's own
	// values (lowest precedence). See the merge order in the datasource layer.
	Values []FileRef `json:"values" yaml:"values"`
}

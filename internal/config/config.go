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
//
// Global defaults for releases (e.g. common labels or values) are expressed via
// confijer's type-default mechanism: a top-level "Release:" block applies to
// every release. Maps (labels) deep-merge with a release's own values winning;
// slices (values) act as a default used only when the release omits its own.
type Config struct {
	// Project is a free-form name for the whole platform.
	Project string `json:"project" yaml:"project"`
	// Repositories are chart sources keyed by alias/host. Helm repos (https://)
	// and OCI registries (oci://) live together, distinguished by URL scheme;
	// a value may be a bare URL string or a full object (see Repository).
	Repositories map[string]Repository `json:"repositories" yaml:"repositories"`
	// Releases are the units nelmwave deploys, keyed by release name.
	Releases map[string]Release `json:"releases" yaml:"releases"`

	// needsCache memoizes ResolveNeeds per release. Resolution compares every
	// release against every other one (label selectors), and both Validate and
	// plan.FromConfig ask for the same answers, so without this the whole O(n²)
	// pass runs twice. Unexported, so confijer ignores it.
	//
	// Valid only while Releases is unchanged: mutate the config and the cache is
	// stale. Everything after Parse treats it as read-only. Not safe for
	// concurrent use.
	needsCache map[string][]ResolvedNeed
}

package config

// Release is a single deployable unit: one nelm release. Its identity — name,
// namespace and kube-context — lives entirely in the Config.Releases map key
// (see Uniqname), so the struct carries none of those fields.
type Release struct {
	// Labels are used for k8s-style selection (-l) and are free-form.
	Labels map[string]string `json:"labels" yaml:"labels"`
	// Needs declares the releases that must be applied before this one (DAG
	// edges), by explicit uniqname and/or by label selector.
	Needs Needs `json:"needs" yaml:"needs,omitempty"`

	// Chart points at a helm-repo or OCI chart. Always required: nelmwave
	// orchestrates external charts only and ships no templates of its own.
	Chart Chart `json:"chart" yaml:"chart"`

	// Values are per-release value sources, merged on top of global Values.
	Values []FileRef `json:"values" yaml:"values"`
	// Sets are inline chart value overrides applied on top of Values (highest
	// precedence). Keys are dotted paths (like helm --set, e.g. "image.tag");
	// values keep their YAML type (int/string/bool/...). Passed to nelm as
	// type-preserving JSON overrides.
	Sets map[string]any `json:"sets" yaml:"sets,omitempty"`
	// Stores are companion files resolved and stored alongside the plan.
	Stores []FileRef `json:"stores" yaml:"stores"`

	// nelm option passthrough.
	Options ReleaseOptions `json:"options" yaml:"options"`
}

// Chart identifies a chart source.
type Chart struct {
	// Name is a helm-repo chart (repo/chart) or an OCI ref (oci://host/chart).
	Name string `json:"name" yaml:"name"`
	// Version is a chart version or constraint.
	Version string `json:"version" yaml:"version"`
}

// ReleaseOptions carries a subset of nelm ReleaseInstall/Uninstall options that
// make sense to express per release. Names map onto nelm's action options in
// the release adapter (added in a later milestone).
type ReleaseOptions struct {
	// Timeout for the operation, e.g. "5m". Empty means nelm's default.
	Timeout string `json:"timeout" yaml:"timeout,omitempty"`
	// CreateNamespace controls namespace creation (nelm NoCreateNamespace = !this).
	CreateNamespace bool `json:"createNamespace" yaml:"createNamespace" default:"true"`
	// AutoRollback rolls back to the last deployed revision on failure
	// (nelm AutoRollback, akin to helm --atomic).
	AutoRollback bool `json:"autoRollback" yaml:"autoRollback"`
}

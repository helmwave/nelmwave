package config

// Release is a single deployable unit: one nelm release. It is keyed by name in
// Config.Releases, so it carries no name field of its own.
type Release struct {
	// Namespace is the target Kubernetes namespace.
	Namespace string `json:"namespace" yaml:"namespace"`
	// Labels are used for k8s-style selection (-l) and are free-form.
	Labels map[string]string `json:"labels" yaml:"labels"`
	// Needs lists release names that must be applied before this one (DAG edges).
	Needs []string `json:"needs" yaml:"needs"`

	// Chart points at a helm-repo or OCI chart. Mutually exclusive with Universal.
	Chart Chart `json:"chart" yaml:"chart"`
	// Universal, when set, deploys the built-in universal chart instead of Chart.
	Universal *UniversalValues `json:"universal" yaml:"universal"`

	// Values are per-release value sources, merged on top of global Values.
	Values []ValueRef `json:"values" yaml:"values"`
	// Store are companion files resolved and stored alongside the plan.
	Store []StoreRef `json:"store" yaml:"store"`

	// nelm option passthrough.
	Options ReleaseOptions `json:"options" yaml:"options"`
}

// Chart identifies a chart source.
type Chart struct {
	// Ref is a helm-repo ref (repo/chart) or an OCI ref (oci://host/chart).
	// Empty Ref together with a Universal block selects the built-in chart.
	Ref string `json:"ref" yaml:"ref"`
	// Version is a chart version or constraint.
	Version string `json:"version" yaml:"version"`
}

// ReleaseOptions carries a subset of nelm ReleaseInstall/Uninstall options that
// make sense to express per release. Names map onto nelm's action options in
// the release adapter (added in a later milestone).
type ReleaseOptions struct {
	// Timeout for the operation, e.g. "5m". Empty means nelm's default.
	Timeout string `json:"timeout" yaml:"timeout"`
	// CreateNamespace controls namespace creation (nelm NoCreateNamespace = !this).
	CreateNamespace bool `json:"createNamespace" yaml:"createNamespace" default:"true"`
	// AutoRollback rolls back to the last deployed revision on failure
	// (nelm AutoRollback, akin to helm --atomic).
	AutoRollback bool `json:"autoRollback" yaml:"autoRollback"`
}

// UsesUniversalChart reports whether this release deploys the built-in chart:
// it has a Universal block and no explicit chart ref.
func (r Release) UsesUniversalChart() bool {
	return r.Universal != nil && r.Chart.Ref == ""
}

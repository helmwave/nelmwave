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

	// Namespace configures the release's namespace — not which namespace it is
	// (that comes from the release key), but whether nelmwave creates it and what
	// metadata it carries.
	Namespace Namespace `json:"namespace" yaml:"namespace,omitempty"`

	// Timeout bounds the operation, e.g. "5m". Empty means nelm's default.
	Timeout string `json:"timeout" yaml:"timeout,omitempty"`
	// AutoRollback rolls back to the last deployed revision on failure
	// (nelm AutoRollback, akin to helm --atomic).
	AutoRollback bool `json:"autoRollback" yaml:"autoRollback,omitempty"`

	// ForceAdoption takes over a resource that another Helm release claims via
	// meta.helm.sh/release-name. Without it nelm refuses to touch it, which is
	// what you want everywhere except migrations and release renames.
	ForceAdoption bool `json:"forceAdoption" yaml:"forceAdoption,omitempty"`
	// RemoveManualChanges reclaims fields added to a resource by hand (kubectl
	// edit) that the manifest does not mention. On by default, as in nelm; set
	// it to false to leave such fields alone.
	RemoveManualChanges bool `json:"removeManualChanges" yaml:"removeManualChanges" default:"true"`
	// InstallCRDs installs the CRDs shipped in the chart's crds/ directory. On
	// by default; turn it off where CRDs are managed by a separate pipeline.
	InstallCRDs bool `json:"installCRDs" yaml:"installCRDs" default:"true"`
	// DeletePropagation is the default deletion strategy for this release's
	// resources: Foreground (nelm's default), Background or Orphan. A single
	// resource can still override it with werf.io/delete-propagation.
	DeletePropagation string `json:"deletePropagation" yaml:"deletePropagation,omitempty"`
	// HistoryLimit caps how many revisions of this release are kept in storage.
	// 0 leaves nelm's default of 10.
	HistoryLimit int `json:"historyLimit" yaml:"historyLimit,omitempty"`
}

// DeletePropagations are the values DeletePropagation accepts. They are
// Kubernetes' own DeletionPropagation values and are case-sensitive: nelm casts
// the string straight to metav1.DeletionPropagation without checking it.
var DeletePropagations = []string{"Foreground", "Background", "Orphan"}

// Namespace holds the settings for a release's namespace. The namespace *name*
// is part of the release key ("api@production"), never a field here.
//
// As a distinct type it also gets a confijer type-default bucket, so a top-level
// "Namespace:" block applies the same creation policy and metadata to every
// release.
type Namespace struct {
	// Create makes nelmwave ensure the namespace exists before applying
	// (nelm's NoCreateNamespace = !Create).
	Create bool `json:"create" yaml:"create" default:"true"`
	// Delete removes the namespace after the release is uninstalled (nelm's
	// DeleteReleaseNamespace). It is deliberately not the mirror of Create: the
	// namespace is not owned by the release, so deleting it takes everything
	// else living there with it. Off unless asked for.
	Delete bool `json:"delete" yaml:"delete,omitempty"`
	// Annotations are applied to the namespace itself. They are merged into
	// whatever is already there; nelmwave never removes annotations it does not
	// manage.
	Annotations map[string]string `json:"annotations" yaml:"annotations,omitempty"`
	// Labels are applied to the namespace itself, with the same merge semantics
	// as Annotations. Useful for policy selectors such as
	// pod-security.kubernetes.io/enforce or istio-injection.
	Labels map[string]string `json:"labels" yaml:"labels,omitempty"`
}

// HasMetadata reports whether any namespace metadata was declared, i.e. whether
// nelmwave has to touch the namespace object beyond letting nelm create it.
func (n Namespace) HasMetadata() bool {
	return len(n.Annotations) > 0 || len(n.Labels) > 0
}

// Chart identifies a chart source.
type Chart struct {
	// Name is a helm-repo chart (repo/chart) or an OCI ref (oci://host/chart).
	Name string `json:"name" yaml:"name"`
	// Version is a chart version or constraint.
	Version string `json:"version" yaml:"version"`
}

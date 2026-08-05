// Package release adapts nelmwave's plan to the nelm deploy engine. The Applier
// interface keeps the graph executor independent of nelm so it can be faked in
// tests; NelmApplier is the real implementation.
package release

import (
	"context"
	"time"
)

// Spec is the resolved, engine-agnostic description of one release operation.
type Spec struct {
	// Name / Namespace / KubeContext identify the release (from its uniqname).
	Name        string
	Namespace   string
	KubeContext string
	// Kube is how to reach the cluster — a kubeconfig, or the connection spelled
	// out directly. Shared by every release of a run; only KubeContext varies.
	Kube KubeConnection

	// Chart is the resolved chart reference (chart name for a helm repo, oci://
	// URL for OCI, or a local path), ChartVersion its version.
	Chart        string
	ChartVersion string
	// ValuesFiles are absolute paths to values files, in merge order.
	ValuesFiles []string
	// SetJSON are inline overrides in nelm's "key=json" form (type-preserving),
	// applied on top of ValuesFiles.
	SetJSON []string

	// Chart-repository connection (helm repos). RepoURL empty means the chart is
	// OCI or local and needs no repo lookup.
	RepoURL       string
	RepoUsername  string
	RepoPassword  string
	RepoSkipTLS   bool
	RepoPassCreds bool
	RepoCAFile    string
	// RepoCertFile / RepoKeyFile are the client certificate for mTLS to the
	// repository; RepoOCIPlainHTTP drops TLS altogether (http:// registries).
	RepoCertFile     string
	RepoKeyFile      string
	RepoOCIPlainHTTP bool
	// RepoSkipUpdate stops the chart's declared dependencies from being
	// refreshed before they are pulled.
	RepoSkipUpdate bool
	// RepoRequestTimeout bounds a single request to the repository (0 = none).
	RepoRequestTimeout time.Duration
	// RegistryConfigPath is a Docker config.json with OCI registry credentials
	// (empty falls back to nelm's default, ~/.docker/config.json).
	RegistryConfigPath string
	// ProvenanceStrategy / ProvenanceKeyring control verification of the chart's
	// PGP signature. Empty strategy leaves nelm's default ("never").
	ProvenanceStrategy string
	ProvenanceKeyring  string

	// Timeout bounds the operation (0 = no timeout).
	Timeout time.Duration
	// CreateNamespace creates the namespace if missing (install only).
	CreateNamespace bool
	// DeleteNamespace deletes the namespace after the release is removed
	// (uninstall only), along with anything else that happens to live in it.
	DeleteNamespace bool
	// NamespaceAnnotations / NamespaceLabels are merged onto the namespace object
	// before the release is applied, so policy labels (pod-security,
	// istio-injection) are in place by the time workloads land. Metadata not
	// listed here is left alone.
	NamespaceAnnotations map[string]string
	NamespaceLabels      map[string]string
	// AutoRollback rolls back to the last deployed revision on failure (install only).
	AutoRollback bool

	// Labels are the release's manifest labels. Besides selection they are put
	// on the release storage object (Secret/ConfigMap), so a release can be
	// found in the cluster by the same labels it is selected by. Helm's own
	// name/owner/status/version are applied after these and win on collision.
	Labels map[string]string
	// Annotations are stored inside each revision of the release (nelm's
	// ReleaseInfoAnnotations), not on any Kubernetes object, so they cannot be
	// selected on — they are read back with `nelm release get`.
	Annotations map[string]string

	// ForceAdoption takes over resources claimed by another Helm release.
	ForceAdoption bool
	// RemoveManualChanges reclaims manually added fields (nelm's
	// NoRemoveManualChanges = !RemoveManualChanges).
	RemoveManualChanges bool
	// InstallCRDs installs the chart's crds/ directory (nelm's
	// NoInstallStandaloneCRDs = !InstallCRDs).
	InstallCRDs bool
	// DeletePropagation is the default deletion strategy (empty = nelm's
	// Foreground).
	DeletePropagation string
	// HistoryLimit caps stored revisions (0 = nelm's default of 10).
	HistoryLimit int
	// StorageDriver / StorageSQLConnection say where the release's state lives,
	// resolved from the manifest's driverURL. Empty driver = nelm's default.
	StorageDriver        string
	StorageSQLConnection string
}

// DiffOptions control how planned changes are rendered. They describe the
// view, not the release, so they come from the command line rather than the
// manifest.
type DiffOptions struct {
	// ShowVerbose prints the whole manifest of a resource that is created or
	// deleted outright, instead of a "<hidden verbose changes>" placeholder.
	// nelm's own CLI defaults this to true; DefaultDiffOptions matches it.
	ShowVerbose bool
	// ShowVerboseCRD does the same for CRDs, which are kept separate because
	// their manifests are large enough to drown the rest of the diff.
	ShowVerboseCRD bool
	// ShowInsignificant keeps helm.sh/werf.io annotations and managedFields in
	// the compared manifests. Without it a change confined to them shows up as
	// "<hidden insignificant changes>".
	ShowInsignificant bool
	// ShowSensitive prints the contents of Secrets and resources marked
	// werf.io/sensitive in the clear. Local debugging only — this lands in CI
	// logs otherwise.
	ShowSensitive bool
	// ContextLines is the unified-diff context size (0 leaves nelm's 3).
	ContextLines int
}

// DefaultDiffOptions is the view nelm's CLI shows by default.
func DefaultDiffOptions() DiffOptions {
	return DiffOptions{ShowVerbose: true}
}

// PlanOptions are the per-invocation knobs of Plan.
type PlanOptions struct {
	// ErrorIfChanges makes Plan report planned changes through its changed
	// return value instead of letting nelm turn them into an error.
	ErrorIfChanges bool
	// Diff controls how the changes are rendered.
	Diff DiffOptions
}

// Applier installs, uninstalls and plans releases through a deploy engine.
type Applier interface {
	Install(ctx context.Context, s Spec) error
	Uninstall(ctx context.Context, s Spec) error
	// Plan computes the changes an install would make, without applying them.
	Plan(ctx context.Context, s Spec, o PlanOptions) (changed bool, err error)
}

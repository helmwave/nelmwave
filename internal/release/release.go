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
	// KubeConfig is an optional kubeconfig path; empty uses nelm's default.
	KubeConfig string

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
	// RegistryConfigPath is a Docker config.json with OCI registry credentials
	// (empty falls back to nelm's default, ~/.docker/config.json).
	RegistryConfigPath string

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
}

// Applier installs, uninstalls and plans releases through a deploy engine.
type Applier interface {
	Install(ctx context.Context, s Spec) error
	Uninstall(ctx context.Context, s Spec) error
	// Plan computes the changes an install would make, without applying them.
	// When errorIfChanges is set, it reports whether any changes are planned via
	// the returned changed flag (instead of as an error).
	Plan(ctx context.Context, s Spec, errorIfChanges bool) (changed bool, err error)
}

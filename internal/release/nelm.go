package release

import (
	"context"

	"github.com/werf/nelm/pkg/action"
)

// NelmApplier is the production Applier backed by github.com/werf/nelm.
type NelmApplier struct{}

// Install deploys or upgrades a release via action.ReleaseInstall.
func (NelmApplier) Install(ctx context.Context, s Spec) error {
	opts := action.ReleaseInstallOptions{
		Chart:             s.Chart,
		ChartVersion:      s.ChartVersion,
		AutoRollback:      s.AutoRollback,
		NoCreateNamespace: !s.CreateNamespace,
		Timeout:           s.Timeout,
	}
	opts.ValuesFiles = s.ValuesFiles
	opts.KubeConfigPaths = kubeConfigPaths(s.KubeConfig)
	opts.KubeContextCurrent = s.KubeContext
	return action.ReleaseInstall(ctx, s.Name, s.Namespace, opts)
}

// Uninstall removes a release via action.ReleaseUninstall.
func (NelmApplier) Uninstall(ctx context.Context, s Spec) error {
	opts := action.ReleaseUninstallOptions{
		Timeout: s.Timeout,
	}
	opts.KubeConfigPaths = kubeConfigPaths(s.KubeConfig)
	opts.KubeContextCurrent = s.KubeContext
	return action.ReleaseUninstall(ctx, s.Name, s.Namespace, opts)
}

// kubeConfigPaths turns an optional kubeconfig path into nelm's slice form;
// nil lets nelm fall back to its default (~/.kube/config or in-cluster).
func kubeConfigPaths(path string) []string {
	if path == "" {
		return nil
	}
	return []string{path}
}

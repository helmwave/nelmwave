package release

import (
	"context"
	"errors"

	"github.com/werf/nelm/pkg/action"
	nelmlog "github.com/werf/nelm/pkg/log"
)

// NelmApplier is the production Applier backed by github.com/werf/nelm.
type NelmApplier struct{}

// nelmContext binds nelm's logboek logger to ctx; nelm's action functions panic
// if the context has no logger bound.
func nelmContext(ctx context.Context) context.Context {
	return nelmlog.SetupLogging(ctx, nelmlog.InfoLevel, nelmlog.SetupLoggingOptions{})
}

// Install deploys or upgrades a release via action.ReleaseInstall.
func (NelmApplier) Install(ctx context.Context, s Spec) error {
	ctx = nelmContext(ctx)
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
	ctx = nelmContext(ctx)
	opts := action.ReleaseUninstallOptions{
		Timeout: s.Timeout,
	}
	opts.KubeConfigPaths = kubeConfigPaths(s.KubeConfig)
	opts.KubeContextCurrent = s.KubeContext
	return action.ReleaseUninstall(ctx, s.Name, s.Namespace, opts)
}

// Plan computes an install diff via action.ReleasePlanInstall without applying.
// With errorIfChanges, nelm returns a changes-planned sentinel when a diff
// exists; Plan translates that into changed=true, err=nil.
func (NelmApplier) Plan(ctx context.Context, s Spec, errorIfChanges bool) (bool, error) {
	ctx = nelmContext(ctx)
	opts := action.ReleasePlanInstallOptions{
		Chart:                 s.Chart,
		ChartVersion:          s.ChartVersion,
		ErrorIfChangesPlanned: errorIfChanges,
	}
	opts.ValuesFiles = s.ValuesFiles
	opts.KubeConfigPaths = kubeConfigPaths(s.KubeConfig)
	opts.KubeContextCurrent = s.KubeContext

	err := action.ReleasePlanInstall(ctx, s.Name, s.Namespace, opts)
	if errorIfChanges && changesPlanned(err) {
		return true, nil
	}
	return false, err
}

// changesPlanned reports whether err is one of nelm's "changes planned"
// sentinels returned when ErrorIfChangesPlanned is set.
func changesPlanned(err error) bool {
	return errors.Is(err, action.ErrChangesPlanned) ||
		errors.Is(err, action.ErrResourceChangesPlanned) ||
		errors.Is(err, action.ErrReleaseInstallPlanned)
}

// kubeConfigPaths turns an optional kubeconfig path into nelm's slice form;
// nil lets nelm fall back to its default (~/.kube/config or in-cluster).
func kubeConfigPaths(path string) []string {
	if path == "" {
		return nil
	}
	return []string{path}
}

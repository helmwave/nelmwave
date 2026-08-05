package release

import (
	"context"
	"errors"

	"github.com/werf/nelm/pkg/action"
	"github.com/werf/nelm/pkg/common"
	"github.com/werf/nelm/pkg/featgate"
	nelmlog "github.com/werf/nelm/pkg/log"
)

func init() {
	// nelmwave charts are almost always remote (repo/chart or oci://), which nelm
	// gates behind a feature flag that is off by default. Enable it up front.
	featgate.FeatGateRemoteCharts.Enable()
}

// applyRepo copies the spec's helm chart-repository connection settings into
// nelm's ChartRepoConnectionOptions.
func applyRepo(o *common.ChartRepoConnectionOptions, s Spec) {
	o.ChartRepoURL = s.RepoURL
	o.ChartRepoBasicAuthUsername = s.RepoUsername
	o.ChartRepoBasicAuthPassword = s.RepoPassword
	o.ChartRepoSkipTLSVerify = s.RepoSkipTLS
	o.ChartRepoPassCreds = s.RepoPassCreds
	o.ChartRepoCAPath = s.RepoCAFile
}

// NelmApplier is the production Applier backed by github.com/werf/nelm.
type NelmApplier struct {
	// LogLevel is nelmwave's own log level (debug|info|warn|error), passed
	// through so that nelm's output follows --log-level too. Empty means info.
	LogLevel string
}

// context binds nelm's logboek logger to ctx; nelm's action functions panic if
// the context has no logger bound.
func (a NelmApplier) context(ctx context.Context) context.Context {
	return nelmlog.SetupLogging(ctx, nelmLevel(a.LogLevel), nelmlog.SetupLoggingOptions{})
}

// nelmLevel maps a nelmwave log level onto nelm's own. The vocabularies differ
// only in "warn" vs "warning"; anything unknown falls back to info, since the
// level was already validated when the zap logger was built.
func nelmLevel(level string) nelmlog.Level {
	switch level {
	case "debug":
		return nelmlog.DebugLevel
	case "warn", "warning":
		return nelmlog.WarningLevel
	case "error":
		return nelmlog.ErrorLevel
	default:
		return nelmlog.InfoLevel
	}
}

// Install deploys or upgrades a release via action.ReleaseInstall, first making
// sure the namespace carries any declared metadata.
func (a NelmApplier) Install(ctx context.Context, s Spec) error {
	// Before nelm, not after: namespace labels such as istio-injection or
	// pod-security only affect workloads created once they are in place.
	if err := applyNamespaceMetadata(ctx, s); err != nil {
		return err
	}

	ctx = a.context(ctx)
	opts := action.ReleaseInstallOptions{
		Chart:                   s.Chart,
		ChartVersion:            s.ChartVersion,
		AutoRollback:            s.AutoRollback,
		NoCreateNamespace:       !s.CreateNamespace,
		Timeout:                 s.Timeout,
		RegistryCredentialsPath: s.RegistryConfigPath,
	}
	opts.ValuesFiles = s.ValuesFiles
	opts.ValuesSetJSON = s.SetJSON
	opts.KubeConfigPaths = kubeConfigPaths(s.KubeConfig)
	opts.KubeContextCurrent = s.KubeContext
	applyRepo(&opts.ChartRepoConnectionOptions, s)
	return action.ReleaseInstall(ctx, s.Name, s.Namespace, opts)
}

// Uninstall removes a release via action.ReleaseUninstall.
func (a NelmApplier) Uninstall(ctx context.Context, s Spec) error {
	ctx = a.context(ctx)
	opts := action.ReleaseUninstallOptions{
		Timeout:                s.Timeout,
		DeleteReleaseNamespace: s.DeleteNamespace,
	}
	opts.KubeConfigPaths = kubeConfigPaths(s.KubeConfig)
	opts.KubeContextCurrent = s.KubeContext
	return action.ReleaseUninstall(ctx, s.Name, s.Namespace, opts)
}

// Plan computes an install diff via action.ReleasePlanInstall without applying.
// With errorIfChanges, nelm returns a changes-planned sentinel when a diff
// exists; Plan translates that into changed=true, err=nil.
func (a NelmApplier) Plan(ctx context.Context, s Spec, errorIfChanges bool) (bool, error) {
	ctx = a.context(ctx)
	opts := action.ReleasePlanInstallOptions{
		Chart:                   s.Chart,
		ChartVersion:            s.ChartVersion,
		ErrorIfChangesPlanned:   errorIfChanges,
		RegistryCredentialsPath: s.RegistryConfigPath,
		Timeout:                 s.Timeout,
	}
	opts.ValuesFiles = s.ValuesFiles
	opts.ValuesSetJSON = s.SetJSON
	opts.KubeConfigPaths = kubeConfigPaths(s.KubeConfig)
	opts.KubeContextCurrent = s.KubeContext
	applyRepo(&opts.ChartRepoConnectionOptions, s)

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

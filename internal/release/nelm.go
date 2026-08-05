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
	o.ChartRepoCertPath = s.RepoCertFile
	o.ChartRepoKeyPath = s.RepoKeyFile
	o.ChartRepoInsecure = s.RepoOCIPlainHTTP
	o.ChartRepoRequestTimeout = s.RepoRequestTimeout
}

// applyRuntime copies the resource-handling policies into nelm's runtime
// options, which install and plan share. Two of them are inverted: nelmwave
// states what it does, nelm what it skips.
func applyRuntime(o *common.ReleaseInstallRuntimeOptions, s Spec) {
	o.ForceAdoption = s.ForceAdoption
	o.NoRemoveManualChanges = !s.RemoveManualChanges
	o.NoInstallStandaloneCRDs = !s.InstallCRDs
	o.DefaultDeletePropagation = s.DeletePropagation
	o.ReleaseHistoryLimit = s.HistoryLimit
	// The manifest's labels double as the storage object's labels. Safe against
	// collisions: helm writes name/owner/status/version after these.
	o.ReleaseLabels = s.Labels
	o.ReleaseInfoAnnotations = s.Annotations
	o.ReleaseStorageDriver = s.StorageDriver
	o.ReleaseStorageSQLConnection = s.StorageSQLConnection
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
		ChartProvenanceStrategy: s.ProvenanceStrategy,
		ChartProvenanceKeyring:  s.ProvenanceKeyring,
		// Not part of ChartRepoConnectionOptions, unlike its siblings.
		ChartRepoSkipUpdate: s.RepoSkipUpdate,
	}
	opts.ValuesFiles = s.ValuesFiles
	opts.ValuesSetJSON = s.SetJSON
	applyKube(&opts.KubeConnectionOptions, s)
	applyRepo(&opts.ChartRepoConnectionOptions, s)
	applyRuntime(&opts.ReleaseInstallRuntimeOptions, s)
	return action.ReleaseInstall(ctx, s.Name, s.Namespace, opts)
}

// Uninstall removes a release via action.ReleaseUninstall.
func (a NelmApplier) Uninstall(ctx context.Context, s Spec) error {
	ctx = a.context(ctx)
	opts := action.ReleaseUninstallOptions{
		Timeout:                s.Timeout,
		DeleteReleaseNamespace: s.DeleteNamespace,
		// Uninstall keeps these as plain fields rather than embedding the
		// runtime options, so applyRuntime does not fit here. ForceAdoption and
		// CRD installation have no meaning when removing a release.
		NoRemoveManualChanges:       !s.RemoveManualChanges,
		DefaultDeletePropagation:    s.DeletePropagation,
		ReleaseHistoryLimit:         s.HistoryLimit,
		ReleaseStorageDriver:        s.StorageDriver,
		ReleaseStorageSQLConnection: s.StorageSQLConnection,
	}
	// Uninstall needs the connection just as much as install does: without it nelm
	// falls back to its own default kubeconfig, so `down` would delete from a
	// different cluster than the one `up` deployed to — and report success, since
	// a release missing from that cluster reads as nothing to do.
	applyKube(&opts.KubeConnectionOptions, s)
	return action.ReleaseUninstall(ctx, s.Name, s.Namespace, opts)
}

// Plan computes an install diff via action.ReleasePlanInstall without applying.
// With errorIfChanges, nelm returns a changes-planned sentinel when a diff
// exists; Plan translates that into changed=true, err=nil.
func (a NelmApplier) Plan(ctx context.Context, s Spec, o PlanOptions) (bool, error) {
	ctx = a.context(ctx)
	opts := action.ReleasePlanInstallOptions{
		Chart:                   s.Chart,
		ChartVersion:            s.ChartVersion,
		ErrorIfChangesPlanned:   o.ErrorIfChanges,
		RegistryCredentialsPath: s.RegistryConfigPath,
		Timeout:                 s.Timeout,
		ChartProvenanceStrategy: s.ProvenanceStrategy,
		ChartProvenanceKeyring:  s.ProvenanceKeyring,
		ChartRepoSkipUpdate:     s.RepoSkipUpdate,
	}
	opts.ValuesFiles = s.ValuesFiles
	opts.ValuesSetJSON = s.SetJSON
	applyKube(&opts.KubeConnectionOptions, s)
	applyRepo(&opts.ChartRepoConnectionOptions, s)
	applyRuntime(&opts.ReleaseInstallRuntimeOptions, s)
	applyDiff(&opts.ResourceDiffOptions, o.Diff)

	err := action.ReleasePlanInstall(ctx, s.Name, s.Namespace, opts)
	if o.ErrorIfChanges && changesPlanned(err) {
		return true, nil
	}
	return false, err
}

// applyDiff copies the rendering options into nelm's. ContextLines is passed
// through as-is: nelm's ApplyDefaults turns a non-positive value into 3.
func applyDiff(o *common.ResourceDiffOptions, d DiffOptions) {
	o.ShowVerboseDiffs = d.ShowVerbose
	o.ShowVerboseCRDDiffs = d.ShowVerboseCRD
	o.ShowInsignificantDiffs = d.ShowInsignificant
	o.ShowSensitiveDiffs = d.ShowSensitive
	o.DiffContextLines = d.ContextLines
}

// changesPlanned reports whether err is one of nelm's "changes planned"
// sentinels returned when ErrorIfChangesPlanned is set.
func changesPlanned(err error) bool {
	return errors.Is(err, action.ErrChangesPlanned) ||
		errors.Is(err, action.ErrResourceChangesPlanned) ||
		errors.Is(err, action.ErrReleaseInstallPlanned)
}

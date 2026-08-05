package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/helmwave/nelmwave/internal/config"
	"github.com/helmwave/nelmwave/internal/graph"
	"github.com/helmwave/nelmwave/internal/plan"
	"github.com/helmwave/nelmwave/internal/release"
	"github.com/helmwave/nelmwave/internal/repo"
)

type operation int

const (
	opInstall operation = iota
	opUninstall
)

func (op operation) verb() string {
	if op == opUninstall {
		return "uninstall"
	}
	return "install"
}

// deployOptions carries the inputs shared by up and down.
type deployOptions struct {
	output       string
	selector     string
	concurrency  int
	includeNeeds bool
	kubeContext  string
	// kube is the cluster connection built from the global flags.
	kube release.KubeConnection
	// registryConfigPath is set internally to a generated Docker config.json for
	// OCI credentials (see repo.DockerConfig).
	registryConfigPath string
	// diff controls how planned changes are rendered; used by diff and by
	// up --dry-run.
	diff release.DiffOptions
}

// deploy selects releases by label, resolves the dependency graph within the
// selection, and applies op across it with bounded concurrency. It is engine-
// agnostic via the Applier, so it can be driven by a fake in tests.
func deploy(ctx context.Context, logger *zap.Logger, p *plan.Plan, o deployOptions, applier release.Applier, op operation) error {
	active, err := selectActive(p, o.selector)
	if err != nil {
		return err
	}
	if len(active) == 0 {
		logger.Warn("no releases match the selector", zap.String("selector", o.selector))
		return nil
	}

	// Remembered before the selection is widened, so the log can say which
	// releases the selector actually named and which the flag dragged in.
	selected := keysOf(active)

	// --include-needs widens the selection along the dependency graph, in the
	// direction the operation travels: install pulls in what the selection needs,
	// uninstall pulls in what needs the selection. Without it, install rejects an
	// unsatisfied required need, while uninstall proceeds — narrowing a teardown
	// is a legitimate thing to want.
	switch {
	case o.includeNeeds && op == opInstall:
		expandNeeds(p, active)
	case o.includeNeeds && op == opUninstall:
		expandDependents(p, active)
	case op == opInstall:
		if err := checkRequiredNeeds(p, active, logger); err != nil {
			return err
		}
	}

	logSelection(logger, op.verb(), selected, active)

	deps := buildEdges(p, active)
	if op == opUninstall {
		deps = graph.Reverse(deps)
	}

	absOut, err := filepath.Abs(o.output)
	if err != nil {
		return err
	}

	cleanup, err := o.setupRegistryConfig(p)
	if err != nil {
		return err
	}
	defer cleanup()

	fn := func(ctx context.Context, key string) error {
		spec, err := buildSpec(key, p.Releases[key], p.Repositories, absOut, o)
		if err != nil {
			return err
		}
		l := logger.With(zap.String("release", key), zap.String("namespace", spec.Namespace))
		l.Info(op.verb() + " started")
		if op == opInstall {
			err = applier.Install(ctx, spec)
		} else {
			// The namespace is not the release's to own: whatever else lives
			// there goes with it. Say so before it happens, not after.
			if spec.DeleteNamespace {
				l.Warn("namespace will be deleted with the release, including anything else in it")
			}
			err = applier.Uninstall(ctx, spec)
		}
		if err != nil {
			return err
		}
		l.Info(op.verb() + " done")
		return nil
	}

	return summarize(logger, op.verb(), graph.Run(ctx, deps, o.concurrency, fn))
}

// selectActive returns the set of release keys whose labels match selector.
func selectActive(p *plan.Plan, selector string) (map[string]struct{}, error) {
	sel, err := config.ParseSelector(selector)
	if err != nil {
		return nil, err
	}
	active := make(map[string]struct{})
	for _, key := range p.ReleaseNames() {
		if sel.Matches(labels.Set(p.Releases[key].Labels)) {
			active[key] = struct{}{}
		}
	}
	return active, nil
}

// diffReleases plans (without applying) every selected release and prints the
// diff nelm produces. With detailedExitCode it returns an *exitError with code
// 2 when any release has planned changes.
func diffReleases(ctx context.Context, logger *zap.Logger, p *plan.Plan, o deployOptions, detailedExitCode bool, applier release.Applier) error {
	active, err := selectActive(p, o.selector)
	if err != nil {
		return err
	}
	if len(active) == 0 {
		logger.Warn("no releases match the selector", zap.String("selector", o.selector))
		return nil
	}

	selected := keysOf(active)

	// Same widening as up, so `diff --include-needs` previews exactly the set
	// `up --include-needs` would apply. No policy check: planning changes
	// nothing, so an unsatisfied need is not a reason to refuse.
	if o.includeNeeds {
		expandNeeds(p, active)
	}

	logSelection(logger, "plan", selected, active)

	deps := buildEdges(p, active)
	absOut, err := filepath.Abs(o.output)
	if err != nil {
		return err
	}

	cleanup, err := o.setupRegistryConfig(p)
	if err != nil {
		return err
	}
	defer cleanup()

	var mu sync.Mutex
	var changed []string
	fn := func(ctx context.Context, key string) error {
		spec, err := buildSpec(key, p.Releases[key], p.Repositories, absOut, o)
		if err != nil {
			return err
		}
		l := logger.With(zap.String("release", key), zap.String("namespace", spec.Namespace))
		l.Info("diff started")
		c, err := applier.Plan(ctx, spec, release.PlanOptions{
			ErrorIfChanges: detailedExitCode,
			Diff:           o.diff,
		})
		if err != nil {
			return err
		}
		if c {
			mu.Lock()
			changed = append(changed, key)
			mu.Unlock()
		}
		return nil
	}

	if err := summarize(logger, "diff", graph.Run(ctx, deps, o.concurrency, fn)); err != nil {
		return err
	}
	if detailedExitCode && len(changed) > 0 {
		sort.Strings(changed)
		return &exitError{code: 2, message: fmt.Sprintf("changes planned for %d release(s): %s", len(changed), strings.Join(changed, ", "))}
	}
	return nil
}

// buildEdges returns dependency edges among active releases only; edges to
// releases outside the selection are dropped.
func buildEdges(p *plan.Plan, active map[string]struct{}) map[string][]string {
	deps := make(map[string][]string, len(active))
	for key := range active {
		var e []string
		for _, n := range p.Releases[key].Needs {
			if _, ok := active[n.Uniqname]; ok {
				e = append(e, n.Uniqname)
			}
		}
		sort.Strings(e)
		deps[key] = e
	}
	return deps
}

// expandNeeds adds every transitive dependency of the active set into it.
func expandNeeds(p *plan.Plan, active map[string]struct{}) {
	queue := make([]string, 0, len(active))
	for k := range active {
		queue = append(queue, k)
	}
	for len(queue) > 0 {
		k := queue[0]
		queue = queue[1:]
		for _, n := range p.Releases[k].Needs {
			if _, ok := active[n.Uniqname]; ok {
				continue
			}
			if _, exists := p.Releases[n.Uniqname]; exists {
				active[n.Uniqname] = struct{}{}
				queue = append(queue, n.Uniqname)
			}
		}
	}
}

// logSelection reports what the run is about to touch, before it touches it.
// Releases the selector did not name are called out separately: --include-needs
// can widen a selection well past what was typed, and on uninstall that means
// deleting releases the user never listed.
func logSelection(logger *zap.Logger, verb string, selected []string, active map[string]struct{}) {
	final := keysOf(active)
	added := make([]string, 0, len(final)-len(selected))
	named := make(map[string]struct{}, len(selected))
	for _, k := range selected {
		named[k] = struct{}{}
	}
	for _, k := range final {
		if _, ok := named[k]; !ok {
			added = append(added, k)
		}
	}

	logger.Info(verb+" selection",
		zap.Int("count", len(final)),
		zap.Strings("releases", final))
	if len(added) > 0 {
		// Warn, not info: on uninstall these are deletions nobody asked for by
		// name, and on install they are extra releases about to be rolled out.
		logger.Warn("pulled in by --include-needs",
			zap.Int("count", len(added)),
			zap.Strings("releases", added))
	}
}

// keysOf returns a set's keys in sorted order, for deterministic output.
func keysOf(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// expandDependents adds every transitive dependent of the active set into it:
// the releases that need an active one, then the releases that need those. It is
// the uninstall counterpart of expandNeeds — removing a release without its
// dependents would leave them running against something that is gone.
func expandDependents(p *plan.Plan, active map[string]struct{}) {
	// Reverse the edges once: needs point from dependent to dependency, and here
	// we walk the other way.
	dependents := make(map[string][]string, len(p.Releases))
	for key, rel := range p.Releases {
		for _, n := range rel.Needs {
			dependents[n.Uniqname] = append(dependents[n.Uniqname], key)
		}
	}

	queue := make([]string, 0, len(active))
	for k := range active {
		queue = append(queue, k)
	}
	for len(queue) > 0 {
		k := queue[0]
		queue = queue[1:]
		for _, dep := range dependents[k] {
			if _, ok := active[dep]; ok {
				continue
			}
			active[dep] = struct{}{}
			queue = append(queue, dep)
		}
	}
}

// checkRequiredNeeds fails if any active release has a required need outside the
// selection; optional needs outside the selection are dropped with a warning.
func checkRequiredNeeds(p *plan.Plan, active map[string]struct{}, logger *zap.Logger) error {
	var unmet []string
	for key := range active {
		for _, n := range p.Releases[key].Needs {
			if _, ok := active[n.Uniqname]; ok {
				continue
			}
			if n.Optional {
				logger.Warn("optional need outside selection dropped",
					zap.String("release", key), zap.String("need", n.Uniqname))
			} else {
				unmet = append(unmet, fmt.Sprintf("release %q needs %q", key, n.Uniqname))
			}
		}
	}
	if len(unmet) > 0 {
		sort.Strings(unmet)
		return fmt.Errorf("unsatisfied needs outside the selection "+
			"(use --include-needs to pull them in, or mark them optional):\n  %s",
			strings.Join(unmet, "\n  "))
	}
	return nil
}

// setJSONArgs converts a sets map into nelm's "key=json" overrides, JSON-encoding
// each value so its YAML type is preserved. Keys are emitted in sorted order for
// determinism.
func setJSONArgs(sets map[string]any) ([]string, error) {
	if len(sets) == 0 {
		return nil, nil
	}
	keys := make([]string, 0, len(sets))
	for k := range sets {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	args := make([]string, 0, len(keys))
	for _, k := range keys {
		v, err := json.Marshal(sets[k])
		if err != nil {
			return nil, fmt.Errorf("set %q: %w", k, err)
		}
		args = append(args, k+"="+string(v))
	}
	return args, nil
}

// setupRegistryConfig generates a Docker config.json for OCI repositories that
// carry credentials and records its path on o; the returned func removes it.
func (o *deployOptions) setupRegistryConfig(p *plan.Plan) (func(), error) {
	path, cleanup, err := repo.DockerConfig(p.Repositories)
	if err != nil {
		return func() {}, err
	}
	o.registryConfigPath = path
	return cleanup, nil
}

// buildSpec turns a plan release into an engine-agnostic release.Spec, resolving
// its chart against the declared repositories.
func buildSpec(key string, rel plan.Release, repos map[string]config.Repository, absOut string, o deployOptions) (release.Spec, error) {
	id, err := config.ParseUniqname(key)
	if err != nil {
		return release.Spec{}, err
	}
	namespace := id.Namespace
	if namespace == "" {
		namespace = "default"
	}
	kubeContext := id.KubeContext
	if kubeContext == "" {
		kubeContext = o.kubeContext
	}

	var valuesFiles []string
	for _, vf := range rel.ValuesFiles {
		valuesFiles = append(valuesFiles, filepath.Join(absOut, filepath.FromSlash(vf)))
	}

	var timeout time.Duration
	if rel.Timeout != "" {
		if timeout, err = time.ParseDuration(rel.Timeout); err != nil {
			return release.Spec{}, fmt.Errorf("release %q: invalid timeout %q: %w", key, rel.Timeout, err)
		}
	}

	chart := repo.Resolve(rel.Chart.Name, repos)

	// Validated at build time, so a parse error here means someone hand-edited
	// the planfile.
	var repoTimeout time.Duration
	if chart.RequestTimeout != "" {
		if repoTimeout, err = time.ParseDuration(chart.RequestTimeout); err != nil {
			return release.Spec{}, fmt.Errorf("release %q: invalid repository requestTimeout %q: %w",
				key, chart.RequestTimeout, err)
		}
	}

	setJSON, err := setJSONArgs(rel.Sets)
	if err != nil {
		return release.Spec{}, fmt.Errorf("release %q: %w", key, err)
	}

	// Validated at build time as well; a failure here means a hand-edited plan.
	driver, err := config.ParseDriverURL(rel.DriverURL)
	if err != nil {
		return release.Spec{}, fmt.Errorf("release %q: %w", key, err)
	}

	return release.Spec{
		Name:                 id.Name,
		Namespace:            namespace,
		KubeContext:          kubeContext,
		Kube:                 o.kube,
		Chart:                chart.Ref,
		ChartVersion:         rel.Chart.Version,
		ValuesFiles:          valuesFiles,
		SetJSON:              setJSON,
		Timeout:              timeout,
		CreateNamespace:      rel.Namespace.Create,
		DeleteNamespace:      rel.Namespace.Delete,
		NamespaceAnnotations: rel.Namespace.Annotations,
		NamespaceLabels:      rel.Namespace.Labels,
		AutoRollback:         rel.AutoRollback,
		Labels:               rel.Labels,
		Annotations:          rel.Annotations,
		ForceAdoption:        rel.ForceAdoption,
		RemoveManualChanges:  rel.RemoveManualChanges,
		InstallCRDs:          rel.InstallCRDs,
		DeletePropagation:    rel.DeletePropagation,
		HistoryLimit:         rel.HistoryLimit,
		StorageDriver:        driver.Driver,
		StorageSQLConnection: driver.SQLConnection,
		RepoURL:              chart.RepoURL,
		RepoUsername:         chart.Username,
		RepoPassword:         chart.Password,
		RepoSkipTLS:          chart.SkipTLSVerify,
		RepoPassCreds:        chart.PassCredentials,
		RepoCAFile:           chart.CAFile,
		RepoCertFile:         chart.CertFile,
		RepoKeyFile:          chart.KeyFile,
		RepoOCIPlainHTTP:     chart.OCIPlainHTTP,
		RepoSkipUpdate:       chart.SkipUpdate,
		RepoRequestTimeout:   repoTimeout,
		RegistryConfigPath:   o.registryConfigPath,
		ProvenanceStrategy:   chart.ProvenanceStrategy,
		ProvenanceKeyring:    chart.ProvenanceKeyring,
	}, nil
}

// summarize logs per-release outcomes and returns an aggregated error if any
// release failed or was skipped.
func summarize(logger *zap.Logger, verb string, results map[string]graph.Result) error {
	var failed, skipped []string
	for key, r := range results {
		switch {
		case r.Skipped:
			skipped = append(skipped, key)
		case r.Err != nil:
			failed = append(failed, key)
		}
	}
	sort.Strings(failed)
	sort.Strings(skipped)

	for _, k := range failed {
		logger.Error(verb+" failed", zap.String("release", k), zap.Error(results[k].Err))
	}
	for _, k := range skipped {
		logger.Warn(verb+" skipped", zap.String("release", k), zap.Error(results[k].Err))
	}
	if len(failed) == 0 && len(skipped) == 0 {
		logger.Info(verb+" complete", zap.Int("releases", len(results)))
		return nil
	}
	return fmt.Errorf("%s: %d succeeded, %d failed, %d skipped",
		verb, len(results)-len(failed)-len(skipped), len(failed), len(skipped))
}

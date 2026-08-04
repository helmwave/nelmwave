package cli

import (
	"context"
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
	kubeConfig   string
	// registryConfigPath is set internally to a generated Docker config.json for
	// OCI credentials (see repo.DockerConfig).
	registryConfigPath string
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

	// Needs policy applies to install only: --include-needs pulls filtered-out
	// dependencies back in; otherwise an unsatisfied strict need is an error.
	if op == opInstall {
		if o.includeNeeds {
			expandNeeds(p, active)
		} else if err := checkStrictNeeds(p, active, logger); err != nil {
			return err
		}
	}

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
		c, err := applier.Plan(ctx, spec, detailedExitCode)
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

// checkStrictNeeds fails if any active release has a strict need outside the
// selection; non-strict needs outside the selection are dropped with a warning.
func checkStrictNeeds(p *plan.Plan, active map[string]struct{}, logger *zap.Logger) error {
	var unmet []string
	for key := range active {
		for _, n := range p.Releases[key].Needs {
			if _, ok := active[n.Uniqname]; ok {
				continue
			}
			if n.Strict {
				unmet = append(unmet, fmt.Sprintf("release %q strictly needs %q", key, n.Uniqname))
			} else {
				logger.Warn("need outside selection dropped",
					zap.String("release", key), zap.String("need", n.Uniqname))
			}
		}
	}
	if len(unmet) > 0 {
		sort.Strings(unmet)
		return fmt.Errorf("unsatisfied strict needs (use --include-needs to pull them in):\n  %s",
			strings.Join(unmet, "\n  "))
	}
	return nil
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
	if rel.Options.Timeout != "" {
		if timeout, err = time.ParseDuration(rel.Options.Timeout); err != nil {
			return release.Spec{}, fmt.Errorf("release %q: invalid timeout %q: %w", key, rel.Options.Timeout, err)
		}
	}

	chart := repo.Resolve(rel.Chart.Name, repos)

	return release.Spec{
		Name:               id.Name,
		Namespace:          namespace,
		KubeContext:        kubeContext,
		KubeConfig:         o.kubeConfig,
		Chart:              chart.Ref,
		ChartVersion:       rel.Chart.Version,
		ValuesFiles:        valuesFiles,
		Timeout:            timeout,
		CreateNamespace:    rel.Options.CreateNamespace,
		AutoRollback:       rel.Options.AutoRollback,
		RepoURL:            chart.RepoURL,
		RepoUsername:       chart.Username,
		RepoPassword:       chart.Password,
		RepoSkipTLS:        chart.SkipTLSVerify,
		RepoPassCreds:      chart.PassCredentials,
		RepoCAFile:         chart.CAFile,
		RegistryConfigPath: o.registryConfigPath,
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

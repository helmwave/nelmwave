package config

import (
	"errors"
	"fmt"
	"sort"
)

// Validate checks a parsed Config for structural correctness:
//   - every release has a chart.name (the built-in universal chart is deferred);
//   - labels are valid Kubernetes labels;
//   - needs reference existing releases, don't self-reference, and form a DAG
//     (no cycles).
//
// Release keys are non-empty and unique by construction (parsed and normalized
// in Parse). Namespace/kube-context are optional (taken from the current
// kube-context when omitted), so they are not checked here. All problems are
// collected and returned as a single joined error, in deterministic order.
func Validate(cfg *Config) error {
	var errs []error

	for _, key := range sortedReleaseNames(cfg.Releases) {
		r := cfg.Releases[key]
		if err := validateChartSource(key, r); err != nil {
			errs = append(errs, err)
		}
		if err := validateLabels(key, r.Labels); err != nil {
			errs = append(errs, err)
		}
		errs = append(errs, validateNeeds(cfg, key, r)...)
	}

	if cycle := findCycle(cfg); len(cycle) > 0 {
		errs = append(errs, fmt.Errorf("needs form a cycle: %s", formatCycle(cycle)))
	}

	return joinErrors(errs)
}

// validateNeeds checks a release's explicit needs (existence, no self-edge) and
// that every needs.labels selector parses.
func validateNeeds(cfg *Config, key string, r Release) []error {
	var errs []error
	for _, need := range sortedNeedKeys(r.Needs.Releases) {
		if need == key {
			errs = append(errs, fmt.Errorf("release %q: needs itself", key))
			continue
		}
		if _, ok := cfg.Releases[need]; !ok {
			errs = append(errs, fmt.Errorf("release %q: needs unknown release %q", key, need))
		}
	}
	if !r.Needs.Labels.Empty() {
		if _, err := r.Needs.Labels.Selector(); err != nil {
			errs = append(errs, fmt.Errorf("release %q: needs.labels: %w", key, err))
		}
	}
	return errs
}

func sortedNeedKeys(m map[string]NeedRelease) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func validateChartSource(name string, r Release) error {
	if r.Chart.Name == "" {
		return fmt.Errorf("release %q: chart.name is required", name)
	}
	return nil
}

// findCycle returns a cycle in the needs graph (list of release keys), or nil.
// Edges point from a release to each of its resolved needs (explicit releases
// plus label-matched releases). Invalid selectors are ignored here (reported by
// Validate). Nodes are visited in sorted order for deterministic output.
func findCycle(cfg *Config) []string {
	releases := cfg.Releases
	names := sortedReleaseNames(releases)

	needs := make(map[string][]string, len(releases))
	for _, name := range names {
		needs[name] = cfg.directNeedKeys(name, releases[name])
	}

	const (
		white = 0 // unvisited
		gray  = 1 // on the current DFS stack
		black = 2 // fully explored
	)
	color := make(map[string]int, len(releases))
	var stack []string

	var dfs func(node string) []string
	dfs = func(node string) []string {
		color[node] = gray
		stack = append(stack, node)
		for _, next := range needs[node] {
			switch color[next] {
			case white:
				if c := dfs(next); c != nil {
					return c
				}
			case gray:
				// Found a back edge; extract the cycle from the stack.
				for i, n := range stack {
					if n == next {
						return append(append([]string{}, stack[i:]...), next)
					}
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[node] = black
		return nil
	}

	for _, name := range names {
		if color[name] == white {
			if c := dfs(name); c != nil {
				return c
			}
		}
	}
	return nil
}

// sortedReleaseNames returns the release names in deterministic (sorted) order.
func sortedReleaseNames(releases map[string]Release) []string {
	names := make([]string, 0, len(releases))
	for name := range releases {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func formatCycle(cycle []string) string {
	out := ""
	for i, n := range cycle {
		if i > 0 {
			out += " -> "
		}
		out += n
	}
	return out
}

// joinErrors returns nil for an empty slice, otherwise a single joined error.
func joinErrors(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

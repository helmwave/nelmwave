package config

import (
	"errors"
	"fmt"
	"sort"
)

// Validate checks a parsed Config for structural correctness:
//   - release names (map keys) are non-empty;
//   - every release selects exactly one chart source (chart.name XOR universal);
//   - a namespace is set;
//   - labels are valid Kubernetes labels;
//   - needs reference existing releases, don't self-reference, and form a DAG
//     (no cycles).
//
// Release names are unique by construction (map keys). All problems are
// collected and returned as a single joined error, in deterministic order.
func Validate(cfg *Config) error {
	var errs []error

	for _, name := range sortedReleaseNames(cfg.Releases) {
		r := cfg.Releases[name]
		if name == "" {
			errs = append(errs, errors.New("release with empty name"))
			continue
		}
		if r.Namespace == "" {
			errs = append(errs, fmt.Errorf("release %q: namespace is required", name))
		}
		if err := validateChartSource(name, r); err != nil {
			errs = append(errs, err)
		}
		if err := validateLabels(name, r.Labels); err != nil {
			errs = append(errs, err)
		}
		for _, need := range r.Needs {
			if need == name {
				errs = append(errs, fmt.Errorf("release %q: needs itself", name))
				continue
			}
			if _, ok := cfg.Releases[need]; !ok {
				errs = append(errs, fmt.Errorf("release %q: needs unknown release %q", name, need))
			}
		}
	}

	if cycle := findCycle(cfg.Releases); len(cycle) > 0 {
		errs = append(errs, fmt.Errorf("needs form a cycle: %s", formatCycle(cycle)))
	}

	return joinErrors(errs)
}

func validateChartSource(name string, r Release) error {
	hasChart := r.Chart.Name != ""
	hasUniversal := r.Universal != nil
	switch {
	case !hasChart && !hasUniversal:
		return fmt.Errorf("release %q: needs either chart.name or a universal block", name)
	case hasChart && hasUniversal:
		return fmt.Errorf("release %q: chart.name and universal are mutually exclusive", name)
	default:
		return nil
	}
}

// findCycle returns a cycle in the needs graph (list of release names), or nil.
// Edges point from a release to each of its needs. Traversal is restricted to
// known releases so unknown-need errors aren't double-reported here. Nodes are
// visited in sorted order for deterministic output.
func findCycle(releases map[string]Release) []string {
	names := sortedReleaseNames(releases)

	needs := make(map[string][]string, len(releases))
	for _, name := range names {
		for _, n := range releases[name].Needs {
			if _, ok := releases[n]; ok && n != name {
				needs[name] = append(needs[name], n)
			}
		}
		sort.Strings(needs[name])
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

package config

import (
	"errors"
	"fmt"
)

// Validate checks a parsed Config for structural correctness:
//   - release names are present and unique;
//   - every release selects exactly one chart source (chart.ref XOR universal);
//   - a namespace is set;
//   - labels are valid Kubernetes labels;
//   - needs reference existing releases, don't self-reference, and form a DAG
//     (no cycles).
//
// All problems are collected and returned as a single joined error.
func Validate(cfg *Config) error {
	var errs []error

	byName := make(map[string]struct{}, len(cfg.Releases))
	for _, r := range cfg.Releases {
		if r.Name == "" {
			errs = append(errs, errors.New("release with empty name"))
			continue
		}
		if _, dup := byName[r.Name]; dup {
			errs = append(errs, fmt.Errorf("duplicate release name %q", r.Name))
			continue
		}
		byName[r.Name] = struct{}{}
	}

	for _, r := range cfg.Releases {
		if r.Name == "" {
			continue
		}
		if r.Namespace == "" {
			errs = append(errs, fmt.Errorf("release %q: namespace is required", r.Name))
		}
		if err := validateChartSource(r); err != nil {
			errs = append(errs, err)
		}
		if err := validateLabels(r.Name, r.Labels); err != nil {
			errs = append(errs, err)
		}
		for _, need := range r.Needs {
			if need == r.Name {
				errs = append(errs, fmt.Errorf("release %q: needs itself", r.Name))
				continue
			}
			if _, ok := byName[need]; !ok {
				errs = append(errs, fmt.Errorf("release %q: needs unknown release %q", r.Name, need))
			}
		}
	}

	if cycle := findCycle(cfg.Releases, byName); len(cycle) > 0 {
		errs = append(errs, fmt.Errorf("needs form a cycle: %s", formatCycle(cycle)))
	}

	return joinErrors(errs)
}

func validateChartSource(r Release) error {
	hasRef := r.Chart.Ref != ""
	hasUniversal := r.Universal != nil
	switch {
	case !hasRef && !hasUniversal:
		return fmt.Errorf("release %q: needs either chart.ref or a universal block", r.Name)
	case hasRef && hasUniversal:
		return fmt.Errorf("release %q: chart.ref and universal are mutually exclusive", r.Name)
	default:
		return nil
	}
}

// findCycle returns a cycle in the needs graph (list of release names), or nil.
// Edges point from a release to each of its needs. valid restricts traversal to
// known release names so unknown-need errors aren't double-reported here.
func findCycle(releases []Release, valid map[string]struct{}) []string {
	needs := make(map[string][]string, len(releases))
	for _, r := range releases {
		for _, n := range r.Needs {
			if _, ok := valid[n]; ok && n != r.Name {
				needs[r.Name] = append(needs[r.Name], n)
			}
		}
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

	for _, r := range releases {
		if r.Name != "" && color[r.Name] == white {
			if c := dfs(r.Name); c != nil {
				return c
			}
		}
	}
	return nil
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

package config

import (
	"fmt"
	"sort"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
)

// Needs declares what a release depends on. All parts are combined: a release
// waits for every release named in Releases plus every release matched by the
// inlined label selector (MatchLabels + MatchLabelsExpressions, Kubernetes
// semantics). An empty label selector adds no dependencies (it does NOT match
// everything).
type Needs struct {
	// Releases lists explicit dependencies keyed by uniqname
	// ("name[@namespace[@kubecontext]]"). The value carries per-dependency
	// options (currently Optional); it is a struct so more can be added.
	Releases map[string]NeedRelease `json:"releases" yaml:"releases,omitempty"`
	// MatchLabels selects dependency releases by exact label match.
	MatchLabels map[string]string `json:"matchLabels" yaml:"matchLabels,omitempty"`
	// MatchLabelsExpressions selects dependency releases by set-based label
	// requirements (operators In, NotIn, Exists, DoesNotExist).
	MatchLabelsExpressions []LabelSelectorRequirement `json:"matchLabelsExpressions" yaml:"matchLabelsExpressions,omitempty"`
}

// NeedRelease holds options for a single explicit release dependency.
type NeedRelease struct {
	// Optional lets the run proceed when this dependency is filtered out of the
	// selection: the edge is dropped with a warning instead of failing. By
	// default a declared dependency is required, matching what `optional` means
	// for values and stores.
	Optional bool `json:"optional" yaml:"optional,omitempty"`
}

// LabelSelectorRequirement is one matchLabelsExpressions entry (same shape as
// Kubernetes' metav1.LabelSelectorRequirement).
type LabelSelectorRequirement struct {
	Key      string   `json:"key" yaml:"key"`
	Operator string   `json:"operator" yaml:"operator"`
	Values   []string `json:"values" yaml:"values,omitempty"`
}

// labelsEmpty reports whether the inlined label selector constrains nothing.
func (n Needs) labelsEmpty() bool {
	return len(n.MatchLabels) == 0 && len(n.MatchLabelsExpressions) == 0
}

// labelSelector converts MatchLabels + MatchLabelsExpressions to an
// apimachinery labels.Selector using Kubernetes semantics. An empty selector
// yields labels.Nothing() so it adds no dependencies.
func (n Needs) labelSelector() (labels.Selector, error) {
	if n.labelsEmpty() {
		return labels.Nothing(), nil
	}
	sel := labels.NewSelector()

	keys := make([]string, 0, len(n.MatchLabels))
	for k := range n.MatchLabels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		req, err := labels.NewRequirement(k, selection.Equals, []string{n.MatchLabels[k]})
		if err != nil {
			return nil, err
		}
		sel = sel.Add(*req)
	}

	for _, e := range n.MatchLabelsExpressions {
		op, err := selectorOperator(e.Operator)
		if err != nil {
			return nil, err
		}
		req, err := labels.NewRequirement(e.Key, op, e.Values)
		if err != nil {
			return nil, err
		}
		sel = sel.Add(*req)
	}
	return sel, nil
}

// selectorOperator maps a matchLabelsExpressions operator string to its
// apimachinery selection.Operator, mirroring Kubernetes' allowed set.
func selectorOperator(op string) (selection.Operator, error) {
	switch op {
	case "In":
		return selection.In, nil
	case "NotIn":
		return selection.NotIn, nil
	case "Exists":
		return selection.Exists, nil
	case "DoesNotExist":
		return selection.DoesNotExist, nil
	default:
		return "", fmt.Errorf("invalid matchLabelsExpressions operator %q (want In, NotIn, Exists or DoesNotExist)", op)
	}
}

// ResolvedNeed is one resolved dependency edge: the target release uniqname and
// whether the dependency is optional (see NeedRelease.Optional). Label-matched
// dependencies are always optional — a selector casts a wide net, and failing
// because it happened to catch a filtered-out release would be surprising.
type ResolvedNeed struct {
	Uniqname string
	Optional bool
}

// ResolveNeeds resolves the concrete dependency edges of release `self` (body
// r): explicit Releases that exist in the config (carrying their Optional flag)
// plus releases matched by the inlined label selector (always optional). Self is
// excluded; the result is sorted by uniqname and deduplicated, with the
// required side winning when a target appears both ways. It errors on an
// invalid selector.
//
// The result is memoized per release (see Config.needsCache): callers ask for
// the same edges repeatedly — validation walks the graph for cycles, then the
// plan projects it — and each resolution is O(number of releases). r is expected
// to be c.Releases[self]; passing a different body returns whatever was cached
// for that key.
func (c *Config) ResolveNeeds(self string, r Release) ([]ResolvedNeed, error) {
	if cached, ok := c.needsCache[self]; ok {
		return cached, nil
	}

	optional := make(map[string]bool)
	for need, opt := range r.Needs.Releases {
		if need != self {
			if _, ok := c.Releases[need]; ok {
				// Key canonicalization can fold two spellings onto one uniqname,
				// so a repeat must not relax an already-required edge.
				if prev, seen := optional[need]; seen {
					optional[need] = prev && opt.Optional
				} else {
					optional[need] = opt.Optional
				}
			}
		}
	}
	if !r.Needs.labelsEmpty() {
		sel, err := r.Needs.labelSelector()
		if err != nil {
			return nil, fmt.Errorf("release %q: needs labels selector: %w", self, err)
		}
		for key, other := range c.Releases {
			if key != self && other.Matches(sel) {
				// An explicit entry already decided this edge; a label match only
				// adds edges nobody named.
				if _, ok := optional[key]; !ok {
					optional[key] = true
				}
			}
		}
	}

	keys := make([]string, 0, len(optional))
	for k := range optional {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]ResolvedNeed, len(keys))
	for i, k := range keys {
		out[i] = ResolvedNeed{Uniqname: k, Optional: optional[k]}
	}

	// Only successful resolutions are cached: a selector error must keep
	// surfacing on every call, not be swallowed after the first.
	if c.needsCache == nil {
		c.needsCache = make(map[string][]ResolvedNeed, len(c.Releases))
	}
	c.needsCache[self] = out
	return out, nil
}

// DirectNeeds returns just the resolved dependency uniqnames (sorted).
func (c *Config) DirectNeeds(self string, r Release) ([]string, error) {
	resolved, err := c.ResolveNeeds(self, r)
	if err != nil {
		return nil, err
	}
	keys := make([]string, len(resolved))
	for i, n := range resolved {
		keys[i] = n.Uniqname
	}
	return keys, nil
}

// directNeedKeys is the error-tolerant variant used by cycle detection: on an
// invalid selector (reported separately by Validate) it falls back to explicit
// needs only.
func (c *Config) directNeedKeys(self string, r Release) []string {
	if keys, err := c.DirectNeeds(self, r); err == nil {
		return keys
	}
	keys := make([]string, 0, len(r.Needs.Releases))
	for need := range r.Needs.Releases {
		if need != self {
			if _, ok := c.Releases[need]; ok {
				keys = append(keys, need)
			}
		}
	}
	sort.Strings(keys)
	return keys
}

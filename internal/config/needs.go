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
	// options (currently Strict); it is a struct so more can be added.
	Releases map[string]NeedRelease `json:"releases" yaml:"releases,omitempty"`
	// MatchLabels selects dependency releases by exact label match.
	MatchLabels map[string]string `json:"matchLabels" yaml:"matchLabels,omitempty"`
	// MatchLabelsExpressions selects dependency releases by set-based label
	// requirements (operators In, NotIn, Exists, DoesNotExist).
	MatchLabelsExpressions []LabelSelectorRequirement `json:"matchLabelsExpressions" yaml:"matchLabelsExpressions,omitempty"`
}

// NeedRelease holds options for a single explicit release dependency.
type NeedRelease struct {
	// Strict fails the dependent release if this dependency is missing from the
	// selected set instead of silently ignoring it. (Enforced from M3 onwards.)
	Strict bool `json:"strict" yaml:"strict,omitempty"`
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

// DirectNeeds resolves the concrete set of release keys that release `self`
// (with body r) depends on: explicit Releases that exist in the config, plus
// releases matched by the inlined label selector. Self is excluded, the result
// is sorted and deduplicated. It errors on an invalid label selector.
func (c *Config) DirectNeeds(self string, r Release) ([]string, error) {
	set := make(map[string]struct{})
	addExplicit(set, c, self, r)
	if !r.Needs.labelsEmpty() {
		sel, err := r.Needs.labelSelector()
		if err != nil {
			return nil, fmt.Errorf("release %q: needs labels selector: %w", self, err)
		}
		for key, other := range c.Releases {
			if key != self && other.Matches(sel) {
				set[key] = struct{}{}
			}
		}
	}
	return sortedSet(set), nil
}

// directNeedKeys is the error-tolerant variant used by cycle detection: it
// skips an invalid selector (its error is reported separately by Validate).
func (c *Config) directNeedKeys(self string, r Release) []string {
	keys, err := c.DirectNeeds(self, r)
	if err != nil {
		set := make(map[string]struct{})
		addExplicit(set, c, self, r)
		return sortedSet(set)
	}
	return keys
}

// addExplicit adds the existing, non-self explicit release dependencies to set.
func addExplicit(set map[string]struct{}, c *Config, self string, r Release) {
	for need := range r.Needs.Releases {
		if need != self {
			if _, ok := c.Releases[need]; ok {
				set[need] = struct{}{}
			}
		}
	}
}

func sortedSet(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

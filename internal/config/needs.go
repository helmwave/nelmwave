package config

import (
	"fmt"
	"sort"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
)

// Needs declares what a release depends on. Both parts are combined: a release
// waits for every release named in Releases plus every release matched by the
// Labels selector.
type Needs struct {
	// Releases lists explicit dependencies keyed by uniqname
	// ("name[@namespace[@kubecontext]]"). The value carries per-dependency
	// options (currently Strict); it is a struct so more can be added.
	Releases map[string]NeedRelease `json:"releases" yaml:"releases,omitempty"`
	// Labels is a Kubernetes label selector (matchLabels + matchExpressions).
	// Every release whose labels satisfy it becomes a dependency. A nil or empty
	// selector adds no dependencies (it does NOT match everything).
	Labels *LabelSelector `json:"labels" yaml:"labels,omitempty"`
}

// NeedRelease holds options for a single explicit release dependency.
type NeedRelease struct {
	// Strict fails the dependent release if this dependency is missing from the
	// selected set instead of silently ignoring it. (Enforced from M3 onwards.)
	Strict bool `json:"strict" yaml:"strict,omitempty"`
}

// LabelSelector mirrors Kubernetes' metav1.LabelSelector so manifests use the
// familiar matchLabels/matchExpressions shape. It carries both json (confijer
// load) and yaml (plan serialization) tags.
type LabelSelector struct {
	MatchLabels      map[string]string          `json:"matchLabels" yaml:"matchLabels,omitempty"`
	MatchExpressions []LabelSelectorRequirement `json:"matchExpressions" yaml:"matchExpressions,omitempty"`
}

// LabelSelectorRequirement is one matchExpressions entry.
type LabelSelectorRequirement struct {
	Key      string   `json:"key" yaml:"key"`
	Operator string   `json:"operator" yaml:"operator"`
	Values   []string `json:"values" yaml:"values,omitempty"`
}

// Empty reports whether the selector constrains nothing.
func (s *LabelSelector) Empty() bool {
	return s == nil || (len(s.MatchLabels) == 0 && len(s.MatchExpressions) == 0)
}

// Selector converts to an apimachinery labels.Selector using Kubernetes
// semantics: matchLabels become equality requirements and matchExpressions use
// the In/NotIn/Exists/DoesNotExist operators. An empty selector yields
// labels.Nothing() so it adds no dependencies.
func (s *LabelSelector) Selector() (labels.Selector, error) {
	if s.Empty() {
		return labels.Nothing(), nil
	}
	sel := labels.NewSelector()

	keys := make([]string, 0, len(s.MatchLabels))
	for k := range s.MatchLabels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		req, err := labels.NewRequirement(k, selection.Equals, []string{s.MatchLabels[k]})
		if err != nil {
			return nil, err
		}
		sel = sel.Add(*req)
	}

	for _, e := range s.MatchExpressions {
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

// selectorOperator maps a matchExpressions operator string to its apimachinery
// selection.Operator, mirroring Kubernetes' allowed set.
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
		return "", fmt.Errorf("invalid matchExpressions operator %q (want In, NotIn, Exists or DoesNotExist)", op)
	}
}

// DirectNeeds resolves the concrete set of release keys that release `self`
// (with body r) depends on: explicit Releases that exist in the config, plus
// releases matched by the Labels selector. Self is excluded, the result is
// sorted and deduplicated. It errors on an invalid label selector.
func (c *Config) DirectNeeds(self string, r Release) ([]string, error) {
	set := make(map[string]struct{})
	for need := range r.Needs.Releases {
		if need != self {
			if _, ok := c.Releases[need]; ok {
				set[need] = struct{}{}
			}
		}
	}
	if !r.Needs.Labels.Empty() {
		sel, err := r.Needs.Labels.Selector()
		if err != nil {
			return nil, fmt.Errorf("release %q: needs.labels: %w", self, err)
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
		for need := range r.Needs.Releases {
			if need != self {
				if _, ok := c.Releases[need]; ok {
					set[need] = struct{}{}
				}
			}
		}
		return sortedSet(set)
	}
	return keys
}

func sortedSet(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

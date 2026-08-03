package config

import (
	"fmt"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/validation"
)

// ParseSelector parses a Kubernetes-style label selector, e.g.
// "app=api,env in (prod,stg),tier!=db". An empty string matches everything.
func ParseSelector(expr string) (labels.Selector, error) {
	if expr == "" {
		return labels.Everything(), nil
	}
	sel, err := labels.Parse(expr)
	if err != nil {
		return nil, fmt.Errorf("parse selector %q: %w", expr, err)
	}
	return sel, nil
}

// Matches reports whether a release's labels satisfy sel.
func (r Release) Matches(sel labels.Selector) bool {
	return sel.Matches(labels.Set(r.Labels))
}

// validateLabels checks that every label key/value is a valid Kubernetes label.
// It returns a joined error describing all offending entries, or nil.
func validateLabels(release string, lbls map[string]string) error {
	var errs []error
	for k, v := range lbls {
		for _, msg := range validation.IsQualifiedName(k) {
			errs = append(errs, fmt.Errorf("release %q: label key %q: %s", release, k, msg))
		}
		for _, msg := range validation.IsValidLabelValue(v) {
			errs = append(errs, fmt.Errorf("release %q: label value %q (key %q): %s", release, v, k, msg))
		}
	}
	return joinErrors(errs)
}

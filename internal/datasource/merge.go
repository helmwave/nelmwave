package datasource

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"
)

// MergeValues deep-merges a sequence of YAML values documents (lowest
// precedence first) into a single YAML document. Maps merge recursively;
// scalars and sequences from a later document replace earlier ones. Empty
// documents are skipped.
func MergeValues(docs [][]byte) ([]byte, error) {
	acc := map[string]any{}
	for i, doc := range docs {
		if len(bytes.TrimSpace(doc)) == 0 {
			continue
		}
		var m map[string]any
		if err := yaml.Unmarshal(doc, &m); err != nil {
			return nil, fmt.Errorf("values document #%d: %w", i, err)
		}
		deepMerge(acc, m)
	}
	out, err := yaml.Marshal(acc)
	if err != nil {
		return nil, fmt.Errorf("marshal merged values: %w", err)
	}
	return out, nil
}

// deepMerge merges src into dst in place: nested maps recurse, everything else
// (scalars, sequences) is overwritten by src.
func deepMerge(dst, src map[string]any) {
	for k, v := range src {
		if sv, ok := asStringMap(v); ok {
			if dv, ok := asStringMap(dst[k]); ok {
				deepMerge(dv, sv)
				dst[k] = dv
				continue
			}
		}
		dst[k] = v
	}
}

// asStringMap normalizes a YAML-decoded mapping to map[string]any. yaml.v3
// decodes nested maps under `any` as map[string]interface{}, but guard the
// map[any]any shape too for safety.
func asStringMap(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case map[string]any:
		return m, true
	case map[any]any:
		out := make(map[string]any, len(m))
		for k, val := range m {
			ks, ok := k.(string)
			if !ok {
				return nil, false
			}
			out[ks] = val
		}
		return out, true
	default:
		return nil, false
	}
}

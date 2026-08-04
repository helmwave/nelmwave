package config

import (
	"fmt"
	"net/url"

	"github.com/helmwave/confijer"
	"gopkg.in/yaml.v3"
)

// Parse unmarshals already-rendered nelmwave.yml bytes into a Config.
//
// It runs normalizations that confijer cannot do itself (confijer silently
// drops a scalar where it expects a struct):
//
//  1. values/store entries written as a bare scalar are rewritten to {src: ...};
//  2. repository entries written as a bare URL string are rewritten to {url: ...};
//  3. every resolved Src is canonicalized so equivalent spellings collapse
//     (see canonicalizeSrc).
//
// It does not validate; call Validate after.
func Parse(data []byte) (*Config, error) {
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse nelmwave config: %w", err)
	}
	normalizeRefLists(raw)
	normalizeRepositories(raw)
	normalizeLabels(raw)

	normalized, err := yaml.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("normalize nelmwave config: %w", err)
	}

	var cfg Config
	if err := confijer.UnmarshalYAML(normalized, &cfg); err != nil {
		return nil, fmt.Errorf("decode nelmwave config: %w", err)
	}
	cfg.canonicalizeSources()
	if err := cfg.canonicalizeUniqnames(); err != nil {
		return nil, err
	}
	cfg.applyGlobalLabels()
	return &cfg, nil
}

// applyGlobalLabels merges Config.Labels into every release's labels; a
// release's own label wins on a key clash.
func (c *Config) applyGlobalLabels() {
	if len(c.Labels) == 0 {
		return
	}
	for key, r := range c.Releases {
		merged := make(map[string]string, len(c.Labels)+len(r.Labels))
		for k, v := range c.Labels {
			merged[k] = v
		}
		for k, v := range r.Labels {
			merged[k] = v
		}
		r.Labels = merged
		c.Releases[key] = r
	}
}

// canonicalizeUniqnames normalizes every release map key and every needs entry
// to its canonical "name[@namespace[@kubecontext]]" form, so equivalent
// spellings collapse and needs can be matched by exact key. It errors on an
// invalid key or on two keys that collapse to the same identity.
func (c *Config) canonicalizeUniqnames() error {
	if len(c.Releases) == 0 {
		return nil
	}
	canon := make(map[string]Release, len(c.Releases))
	for key, r := range c.Releases {
		u, err := ParseUniqname(key)
		if err != nil {
			return err
		}
		ck := u.String()
		if _, dup := canon[ck]; dup {
			return fmt.Errorf("release %q collides with another release after normalization to %q", key, ck)
		}
		if r.Needs.Releases != nil {
			canonNeeds := make(map[string]NeedRelease, len(r.Needs.Releases))
			for need, opts := range r.Needs.Releases {
				nu, err := ParseUniqname(need)
				if err != nil {
					return fmt.Errorf("release %q: %w", ck, err)
				}
				canonNeeds[nu.String()] = opts
			}
			r.Needs.Releases = canonNeeds
		}
		canon[ck] = r
	}
	c.Releases = canon
	return nil
}

// normalizeLabels coerces label-map values to strings so bare YAML scalars
// (true, 3, ...) are accepted where Kubernetes wants string labels. It covers
// the global labels, each release's labels, and needs.matchLabels selectors.
func normalizeLabels(root map[string]any) {
	stringifyValues(root["labels"])

	releases, ok := root["releases"].(map[string]any)
	if !ok {
		return
	}
	for _, rv := range releases {
		rel, ok := rv.(map[string]any)
		if !ok {
			continue
		}
		stringifyValues(rel["labels"])
		if needs, ok := rel["needs"].(map[string]any); ok {
			stringifyValues(needs["matchLabels"])
		}
	}
}

// stringifyValues rewrites scalar values of a map to their string form; strings
// and non-scalar values are left as is.
func stringifyValues(v any) {
	m, ok := v.(map[string]any)
	if !ok {
		return
	}
	for k, val := range m {
		switch val.(type) {
		case string, nil, map[string]any, []any:
			// leave strings and non-scalars alone
		default:
			m[k] = fmt.Sprintf("%v", val)
		}
	}
}

// normalizeRepositories rewrites bare-URL-string repository entries into
// {url: <string>} maps so confijer decodes them into Repository.
func normalizeRepositories(root map[string]any) {
	repos, ok := root["repositories"].(map[string]any)
	if !ok {
		return
	}
	for name, rv := range repos {
		if s, ok := rv.(string); ok {
			repos[name] = map[string]any{"url": s}
		}
	}
}

// normalizeRefLists rewrites bare-string entries in the top-level values list
// and in each release's values/store lists into {src: <string>} maps, so
// confijer decodes them into FileRef instead of dropping them.
func normalizeRefLists(root map[string]any) {
	if root == nil {
		return
	}
	root["values"] = normalizeRefList(root["values"])

	releases, ok := root["releases"].(map[string]any)
	if !ok {
		return
	}
	for _, rv := range releases {
		rel, ok := rv.(map[string]any)
		if !ok {
			continue
		}
		if _, has := rel["values"]; has {
			rel["values"] = normalizeRefList(rel["values"])
		}
		if _, has := rel["store"]; has {
			rel["store"] = normalizeRefList(rel["store"])
		}
	}
}

// normalizeRefList converts each bare-string element of a values/store list
// into a {src: <string>} map. Non-string elements (already maps) are left as is.
func normalizeRefList(v any) any {
	list, ok := v.([]any)
	if !ok {
		return v
	}
	for i, e := range list {
		if s, ok := e.(string); ok {
			list[i] = map[string]any{"src": s}
		}
	}
	return list
}

// canonicalizeSources rewrites every Src so equivalent spellings collapse.
func (c *Config) canonicalizeSources() {
	for i := range c.Values {
		c.Values[i].Src = canonicalizeSrc(c.Values[i].Src)
	}
	for name, r := range c.Releases {
		for i := range r.Values {
			r.Values[i].Src = canonicalizeSrc(r.Values[i].Src)
		}
		for i := range r.Store {
			r.Store[i].Src = canonicalizeSrc(r.Store[i].Src)
		}
		c.Releases[name] = r
	}
}

// canonicalizeSrc normalizes a datasource reference so that a bare path and its
// file:// URL spelling become identical. Local references collapse to a plain
// path; any other datasource scheme (env:, vault://, s3://, http(s)://, git://,
// ...) is kept verbatim.
//
//	values/pg.yml.tpl        -> values/pg.yml.tpl
//	file://values/pg.yml.tpl -> values/pg.yml.tpl
//	file:///etc/pg.yml       -> /etc/pg.yml
//	env:PG_VALUES            -> env:PG_VALUES (unchanged)
func canonicalizeSrc(src string) string {
	u, err := url.Parse(src)
	if err != nil || u.Scheme == "" {
		return src
	}
	if u.Scheme == "file" {
		if p := u.Host + u.Path; p != "" {
			return p
		}
		return u.Opaque
	}
	return src
}

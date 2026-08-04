package config

import (
	"fmt"
	"net/url"

	"github.com/helmwave/confijer"
	"gopkg.in/yaml.v3"
)

// Parse unmarshals already-rendered nelmwave.yml bytes into a Config.
//
// It runs two normalizations that confijer cannot do itself:
//
//  1. values/store entries written as a bare scalar ("src") are rewritten to
//     the mapping form ({src: "..."}) before confijer decodes them, because
//     confijer silently drops scalar elements where a struct is expected.
//  2. every resolved Src is canonicalized so equivalent spellings collapse to
//     one form (see canonicalizeSrc).
//
// It does not validate; call Validate after.
func Parse(data []byte) (*Config, error) {
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse nelmwave config: %w", err)
	}
	normalizeRefLists(raw)

	normalized, err := yaml.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("normalize nelmwave config: %w", err)
	}

	var cfg Config
	if err := confijer.UnmarshalYAML(normalized, &cfg); err != nil {
		return nil, fmt.Errorf("decode nelmwave config: %w", err)
	}
	cfg.canonicalizeSources()
	return &cfg, nil
}

// normalizeRefLists rewrites bare-string entries in the top-level values list
// and in each release's values/store lists into {src: <string>} maps, so
// confijer decodes them into ValueRef/StoreRef instead of dropping them.
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

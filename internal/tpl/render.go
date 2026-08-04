// Package tpl renders nelmwave manifests and *.tpl datasources through
// gomplate v5, using [[ ]] action delimiters by default.
//
// The rendering context intentionally exposes only environment access (.Env,
// getenv) plus gomplate's standard function namespaces (strings, datasource,
// file, ...). Per-release data is not available while rendering the root
// manifest — releases are expanded from the parsed structure afterwards.
package tpl

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"text/template"

	gomplate "github.com/hairyhenderson/gomplate/v5"
)

// Default action delimiters. [FIXED] by the project spec.
const (
	DefaultLeftDelim  = "[["
	DefaultRightDelim = "]]"
)

// Options tune a single render.
type Options struct {
	// LeftDelim / RightDelim override the action delimiters. Empty values fall
	// back to the [[ ]] defaults.
	LeftDelim  string
	RightDelim string
	// Funcs are extra template functions merged on top of gomplate's built-ins.
	Funcs template.FuncMap
	// Datasources registers gomplate datasources by name (name -> URL) available
	// to the template via ds/datasource/include.
	Datasources map[string]string
}

// Render renders src as a gomplate template and returns the result. name is
// used only in error messages.
func Render(ctx context.Context, name string, src []byte, opts Options) ([]byte, error) {
	ldelim := opts.LeftDelim
	if ldelim == "" {
		ldelim = DefaultLeftDelim
	}
	rdelim := opts.RightDelim
	if rdelim == "" {
		rdelim = DefaultRightDelim
	}

	datasources, err := parseDatasources(opts.Datasources)
	if err != nil {
		return nil, err
	}
	renderer := gomplate.NewRenderer(gomplate.RenderOptions{
		LDelim:      ldelim,
		RDelim:      rdelim,
		Funcs:       opts.Funcs,
		Datasources: datasources,
	})

	var out bytes.Buffer
	if err := renderer.Render(ctx, name, string(src), &out); err != nil {
		return nil, fmt.Errorf("render template %q: %w", name, err)
	}
	return out.Bytes(), nil
}

// parseDatasources converts a name->URL map into gomplate's DataSource map.
func parseDatasources(sources map[string]string) (map[string]gomplate.DataSource, error) {
	if len(sources) == 0 {
		return nil, nil
	}
	ds := make(map[string]gomplate.DataSource, len(sources))
	for name, raw := range sources {
		u, err := url.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("datasource %q: parse url %q: %w", name, raw, err)
		}
		ds[name] = gomplate.DataSource{URL: u}
	}
	return ds, nil
}

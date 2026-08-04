// Package datasource resolves values/store file references (FileRef.Src) into
// bytes, and deep-merges values documents.
//
// Every reference is read through gomplate v5's `include`, which returns the raw
// datasource content: local paths (schemeless or file://) become absolute
// file:// URLs, other schemes (env:, http(s)://, s3://, vault://, git://, ...)
// are passed through as-is. The content is then post-processed by extension:
// *.tpl/*.tmpl are rendered as gomplate templates, *.sops is rejected
// (deferred), everything else is copied verbatim.
package datasource

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	gomplate "github.com/hairyhenderson/gomplate/v5"

	"github.com/helmwave/nelmwave/internal/tpl"
)

// ErrSopsNotSupported is returned for *.sops sources (deferred feature).
var ErrSopsNotSupported = errors.New("sops sources are not supported yet")

// Resolver reads datasource references relative to a base directory.
type Resolver struct {
	// BaseDir is the directory schemeless (local) paths resolve against.
	BaseDir string
}

// NewResolver returns a Resolver rooted at baseDir.
func NewResolver(baseDir string) *Resolver { return &Resolver{BaseDir: baseDir} }

type kind int

const (
	kindCopy kind = iota
	kindRender
	kindSops
)

// Resolve fetches src and post-processes it by extension.
func (r *Resolver) Resolve(ctx context.Context, src string) ([]byte, error) {
	switch classify(src) {
	case kindSops:
		return nil, fmt.Errorf("%q: %w", src, ErrSopsNotSupported)
	case kindRender:
		raw, err := r.fetch(ctx, src)
		if err != nil {
			return nil, err
		}
		return tpl.Render(ctx, src, raw, tpl.Options{})
	default:
		return r.fetch(ctx, src)
	}
}

// classify picks the post-processing behavior from src's suffix (ignoring any
// query/fragment).
func classify(src string) kind {
	p := src
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	switch {
	case strings.HasSuffix(p, ".sops"):
		return kindSops
	case strings.HasSuffix(p, ".tpl"), strings.HasSuffix(p, ".tmpl"):
		return kindRender
	default:
		return kindCopy
	}
}

// fetch returns the raw bytes of src via gomplate's include. Local references
// (schemeless or file://) are resolved to an absolute file:// URL against
// BaseDir; other schemes are passed through unchanged.
func (r *Resolver) fetch(ctx context.Context, src string) ([]byte, error) {
	u, err := url.Parse(src)
	if err != nil {
		return nil, fmt.Errorf("parse source %q: %w", src, err)
	}
	if u.Scheme == "" || u.Scheme == "file" {
		path := src
		if u.Scheme == "file" {
			path = u.Host + u.Path
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(r.BaseDir, path)
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("resolve path %q: %w", src, err)
		}
		u = &url.URL{Scheme: "file", Path: abs}
	}
	return r.fetchDatasource(ctx, src, u)
}

// fetchDatasource reads a datasource URL through gomplate's include, which
// returns the raw content without parsing it.
func (r *Resolver) fetchDatasource(ctx context.Context, src string, u *url.URL) ([]byte, error) {
	renderer := gomplate.NewRenderer(gomplate.RenderOptions{
		LDelim:      tpl.DefaultLeftDelim,
		RDelim:      tpl.DefaultRightDelim,
		Datasources: map[string]gomplate.DataSource{"src": {URL: u}},
	})
	var out bytes.Buffer
	if err := renderer.Render(ctx, src, `[[ include "src" ]]`, &out); err != nil {
		return nil, fmt.Errorf("resolve datasource %q: %w", src, err)
	}
	return out.Bytes(), nil
}

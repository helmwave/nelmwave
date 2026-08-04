// Package datasource resolves a values/store file reference (FileRef.Src) into
// bytes. Nothing more: merging of multiple values files is left to nelm
// (Helm-native ordered merge), so this package only turns a reference into
// content.
//
// Every reference is read through gomplate v5's `include`, which returns the raw
// datasource content: local paths (schemeless or file://) become absolute
// file:// URLs, other schemes (env:, http(s)://, s3://, vault://, git://, ...)
// are passed through as-is. The content is then post-processed by extension:
// *.sops is decrypted, *.tpl/*.tmpl are rendered as gomplate templates, and
// everything else is copied verbatim. The two compose: *.tpl.sops is decrypted
// first, then rendered.
package datasource

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/getsops/sops/v3/decrypt"
	gomplate "github.com/hairyhenderson/gomplate/v5"

	"github.com/helmwave/nelmwave/internal/tpl"
)

// Resolver reads datasource references relative to a base directory.
type Resolver struct {
	// BaseDir is the directory schemeless (local) paths resolve against.
	BaseDir string
}

// NewResolver returns a Resolver rooted at baseDir.
func NewResolver(baseDir string) *Resolver { return &Resolver{BaseDir: baseDir} }

// IsEncrypted reports whether src is a sops source, i.e. whether resolving it
// writes decrypted content to disk. Callers use it to warn about what ends up in
// the build directory.
func IsEncrypted(src string) bool { return classify(src).decrypt }

// steps describes the post-processing a source needs, in the order it applies.
type steps struct {
	// decrypt runs first: a template inside an encrypted file is only readable
	// once the file has been decrypted.
	decrypt bool
	// sopsFormat is the sops input format ("yaml", "json", "dotenv", "binary"),
	// taken from the extension left of .sops.
	sopsFormat string
	// render runs last, over cleartext.
	render bool
}

// Resolve fetches src and post-processes it by extension. For *.tpl sources the
// given datasources (name -> URL) are exposed to the template via ds/include.
func (r *Resolver) Resolve(ctx context.Context, src string, datasources map[string]string) ([]byte, error) {
	s := classify(src)

	out, err := r.fetch(ctx, src)
	if err != nil {
		return nil, err
	}

	if s.decrypt {
		if out, err = decryptSops(src, out, s.sopsFormat); err != nil {
			return nil, err
		}
	}
	if s.render {
		return tpl.Render(ctx, src, out, tpl.Options{Datasources: datasources})
	}
	return out, nil
}

// decryptSops turns a sops-encrypted document into cleartext. Key material comes
// from the ambient environment exactly as it does for the sops CLI (age, PGP,
// KMS, Vault), so nelmwave neither holds nor configures keys.
func decryptSops(src string, data []byte, format string) ([]byte, error) {
	clear, err := decrypt.Data(data, format)
	if err != nil {
		return nil, fmt.Errorf("decrypt %q: %w "+
			"(sops reads keys from the environment: SOPS_AGE_KEY_FILE, "+
			"GnuPG, or cloud KMS credentials)", src, err)
	}
	return clear, nil
}

// classify picks the post-processing steps from src's suffixes (ignoring any
// query/fragment). Suffixes are peeled right to left, so secrets.yml.tpl.sops
// means "decrypt, then render", and the format handed to sops is the one under
// the encryption layer.
func classify(src string) steps {
	p := src
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}

	var s steps
	if rest, ok := strings.CutSuffix(p, ".sops"); ok {
		s.decrypt = true
		p = rest
		s.sopsFormat = sopsFormat(p)
	}
	if strings.HasSuffix(p, ".tpl") || strings.HasSuffix(p, ".tmpl") {
		s.render = true
	}
	return s
}

// sopsFormat maps the extension under .sops onto a sops input format.
//
// A template layer forces "binary": gomplate's [[ ... ]] is a valid YAML flow
// sequence, so encrypting a template as YAML would let sops reshape it into a
// nested list and destroy the actions. Templates are text until they are
// rendered, and sops must treat them as such — encrypt them with
// `sops --input-type binary`.
func sopsFormat(path string) string {
	if strings.HasSuffix(path, ".tpl") || strings.HasSuffix(path, ".tmpl") {
		return "binary"
	}
	switch {
	case strings.HasSuffix(path, ".yml"), strings.HasSuffix(path, ".yaml"):
		return "yaml"
	case strings.HasSuffix(path, ".json"):
		return "json"
	case strings.HasSuffix(path, ".env"):
		return "dotenv"
	default:
		// Anything else is decrypted as an opaque blob, which is what sops does
		// for files it cannot structure.
		return "binary"
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

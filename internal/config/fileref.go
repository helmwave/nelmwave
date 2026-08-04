package config

// FileRef is a single file source resolved through the datasource layer. The
// same type backs both a release's values and its store files. Name optionally
// names the resolved artifact under .nelmwave/; when empty an index-prefixed
// basename is used. The internal artifact directory layout is otherwise owned
// by nelmwave, not the user.
//
// In the manifest an entry may be written in any of these equivalent forms:
//
//	values:
//	  - src: file://values/pg.yml.tpl   # mapping, with scheme
//	  - file://values/pg.yml.tpl        # bare string, with scheme
//	  - src: values/pg.yml.tpl          # mapping, no scheme (local file)
//	  - values/pg.yml.tpl               # bare string, no scheme (local file)
//
// The bare-string forms are expanded to the mapping form during parsing, and a
// missing/file:// scheme collapses to a plain local path (see canonicalizeSrc).
type FileRef struct {
	// Src is a datasource reference: a local path, or a URL with any gomplate
	// datasource scheme (env:, vault://, s3://, http(s)://, git://, ...).
	// Behaviour is chosen by extension: *.yml/*.yaml are copied, *.yml.tpl are
	// rendered through gomplate. (*.yml.sops is reserved; not supported yet.)
	Src string `json:"src" yaml:"src"`
	// Name names the resolved artifact file under .nelmwave/ (values or store).
	// Empty means nelmwave derives an index-prefixed basename automatically.
	Name string `json:"name" yaml:"name,omitempty"`
	// Optional skips a source that does not exist instead of failing the build.
	// It covers "the file is not there" (os.ErrNotExist) — a datasource that
	// exists but errors (HTTP 500, a broken template) still fails.
	Optional bool `json:"optional" yaml:"optional,omitempty"`
}

package config

// FileRef is a single file source resolved through the datasource layer. The
// same type backs both a release's values and its store files: for values only
// Src (and the Optional/Strict flags) is meaningful, while store files also use
// Dst to place the resolved file under the release's store directory.
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
	// Dst is the relative destination path under the release's store dir. It is
	// only used for store files; values ignore it.
	Dst string `json:"dst" yaml:"dst,omitempty"`
	// Optional: a missing source is skipped instead of failing.
	Optional bool `json:"optional" yaml:"optional,omitempty"`
	// Strict: fail loudly on any resolution warning.
	Strict bool `json:"strict" yaml:"strict,omitempty"`
}

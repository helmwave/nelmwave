package config

// ValueRef is a single values source resolved through the datasource layer.
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
type ValueRef struct {
	// Src is a datasource reference: a local path, or a URL with any gomplate
	// datasource scheme (env:, vault://, s3://, http(s)://, git://, ...).
	// Behaviour is chosen by extension: *.yml/*.yaml are copied, *.yml.tpl are
	// rendered through gomplate. (*.yml.sops is reserved; not supported yet.)
	Src string `json:"src" yaml:"src"`
	// Optional: a missing source is skipped instead of failing.
	Optional bool `json:"optional" yaml:"optional"`
	// Strict: fail loudly on any resolution warning.
	Strict bool `json:"strict" yaml:"strict"`
}

// StoreRef is a companion file resolved and written under .nelmwave/store/.
type StoreRef struct {
	// Src is a datasource URL, same rules as ValueRef.Src.
	Src string `json:"src" yaml:"src"`
	// Dst is the relative destination path under the release's store dir.
	Dst string `json:"dst" yaml:"dst"`
	// Optional: a missing source is skipped instead of failing.
	Optional bool `json:"optional" yaml:"optional"`
	// Strict: fail loudly on any resolution warning.
	Strict bool `json:"strict" yaml:"strict"`
}

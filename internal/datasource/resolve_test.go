package datasource

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolve_LocalCopyAndRender(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "plain.yml", "foo: bar\n")
	writeFile(t, dir, "tmpl.yml.tpl", `n: [[ getenv "NW_N" "3" ]]`)
	t.Setenv("NW_N", "7")

	r := NewResolver(dir)
	ctx := context.Background()

	got, err := r.Resolve(ctx, "plain.yml", nil)
	if err != nil || string(got) != "foo: bar\n" {
		t.Errorf("copy: got %q err %v", got, err)
	}

	got, err = r.Resolve(ctx, "tmpl.yml.tpl", nil)
	if err != nil || string(got) != "n: 7" {
		t.Errorf("render: got %q err %v", got, err)
	}
}

func TestResolve_EnvScheme(t *testing.T) {
	t.Setenv("NW_SECRET", "hunter2")
	got, err := NewResolver("").Resolve(context.Background(), "env:NW_SECRET", nil)
	if err != nil || string(got) != "hunter2" {
		t.Errorf("env scheme: got %q err %v", got, err)
	}
}

// useTestAgeKey points sops at the committed test identity (testdata/age-test.key).
func useTestAgeKey(t *testing.T) {
	t.Helper()
	key, err := filepath.Abs(filepath.Join("testdata", "age-test.key"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SOPS_AGE_KEY_FILE", key)
}

func TestResolve_SopsDecrypts(t *testing.T) {
	useTestAgeKey(t)

	got, err := NewResolver("testdata").Resolve(context.Background(), "secret.yml.sops", nil)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	// Cleartext, and no sops metadata left behind.
	if !strings.Contains(string(got), "password: s3cr3t") {
		t.Errorf("decrypted content = %q", got)
	}
	if strings.Contains(string(got), "ENC[") || strings.Contains(string(got), "sops:") {
		t.Errorf("output still carries sops artifacts: %q", got)
	}
}

// A *.tpl.sops source composes both steps: decrypt, then render. The template
// is unreadable before decryption, so the order is not interchangeable.
func TestResolve_SopsThenTemplate(t *testing.T) {
	useTestAgeKey(t)
	t.Setenv("SOPS_TEST_ENV", "prod")

	got, err := NewResolver("testdata").Resolve(context.Background(), "secret.yml.tpl.sops", nil)
	if err != nil {
		t.Fatalf("decrypt+render: %v", err)
	}
	if !strings.Contains(string(got), "env: prod") {
		t.Errorf("template was not rendered after decryption: %q", got)
	}
	if !strings.Contains(string(got), "password: s3cr3t") {
		t.Errorf("decrypted content missing: %q", got)
	}
}

func TestResolve_SopsWithoutKeyFails(t *testing.T) {
	// No SOPS_AGE_KEY_FILE, and an explicitly empty one so an ambient key in the
	// developer's environment cannot make this pass by accident.
	t.Setenv("SOPS_AGE_KEY_FILE", filepath.Join(t.TempDir(), "absent.key"))
	t.Setenv("SOPS_AGE_KEY", "")

	_, err := NewResolver("testdata").Resolve(context.Background(), "secret.yml.sops", nil)
	if err == nil {
		t.Fatal("decrypting without a key should fail")
	}
	// The message must point at where keys come from.
	if !strings.Contains(err.Error(), "SOPS_AGE_KEY_FILE") {
		t.Errorf("error should mention how to supply keys, got: %v", err)
	}
}

func TestClassify_PeelsSuffixes(t *testing.T) {
	cases := map[string]steps{
		"a.yml":       {},
		"a.yml.tpl":   {render: true},
		"a.yml.sops":  {decrypt: true, sopsFormat: "yaml"},
		"a.yaml.sops": {decrypt: true, sopsFormat: "yaml"},
		"a.json.sops": {decrypt: true, sopsFormat: "json"},
		"a.env.sops":  {decrypt: true, sopsFormat: "dotenv"},
		"a.txt.sops":  {decrypt: true, sopsFormat: "binary"},
		// A template layer forces binary: [[ ... ]] is valid YAML, and sops
		// would otherwise reshape the actions into nested lists.
		"a.yml.tpl.sops":  {decrypt: true, sopsFormat: "binary", render: true},
		"a.yml.tmpl.sops": {decrypt: true, sopsFormat: "binary", render: true},
		// Query strings belong to the URL, not to the file name.
		"http://h/a.yml.sops?v=1": {decrypt: true, sopsFormat: "yaml"},
	}
	for src, want := range cases {
		if got := classify(src); got != want {
			t.Errorf("classify(%q) = %+v, want %+v", src, got, want)
		}
	}
}

func TestResolve_MissingIsNotExist(t *testing.T) {
	_, err := NewResolver(t.TempDir()).Resolve(context.Background(), "nope.yml", nil)
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing file should be os.ErrNotExist, got %v", err)
	}
}

func TestResolve_RenderErrorNamesSource(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "bad.yml.tpl", "x: [[ ]]")
	_, err := NewResolver(dir).Resolve(context.Background(), "bad.yml.tpl", nil)
	if err == nil || !strings.Contains(err.Error(), "bad.yml.tpl") {
		t.Errorf("want error naming source, got %v", err)
	}
}

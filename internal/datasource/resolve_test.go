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

	got, err := r.Resolve(ctx, "plain.yml")
	if err != nil || string(got) != "foo: bar\n" {
		t.Errorf("copy: got %q err %v", got, err)
	}

	got, err = r.Resolve(ctx, "tmpl.yml.tpl")
	if err != nil || string(got) != "n: 7" {
		t.Errorf("render: got %q err %v", got, err)
	}
}

func TestResolve_EnvScheme(t *testing.T) {
	t.Setenv("NW_SECRET", "hunter2")
	got, err := NewResolver("").Resolve(context.Background(), "env:NW_SECRET")
	if err != nil || string(got) != "hunter2" {
		t.Errorf("env scheme: got %q err %v", got, err)
	}
}

func TestResolve_SopsDeferred(t *testing.T) {
	_, err := NewResolver("").Resolve(context.Background(), "secret.yml.sops")
	if !errors.Is(err, ErrSopsNotSupported) {
		t.Errorf("want ErrSopsNotSupported, got %v", err)
	}
}

func TestResolve_MissingIsNotExist(t *testing.T) {
	_, err := NewResolver(t.TempDir()).Resolve(context.Background(), "nope.yml")
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing file should be os.ErrNotExist, got %v", err)
	}
}

func TestResolve_RenderErrorNamesSource(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "bad.yml.tpl", "x: [[ ]]")
	_, err := NewResolver(dir).Resolve(context.Background(), "bad.yml.tpl")
	if err == nil || !strings.Contains(err.Error(), "bad.yml.tpl") {
		t.Errorf("want error naming source, got %v", err)
	}
}

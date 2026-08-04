package datasource

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkResolve_LocalCopy(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "v.yml")
	if err := os.WriteFile(path, []byte("a: 1\nb: 2\n"), 0o644); err != nil {
		b.Fatal(err)
	}
	r := NewResolver(dir)
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := r.Resolve(ctx, "v.yml", nil); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkOSReadFile_Baseline(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "v.yml")
	if err := os.WriteFile(path, []byte("a: 1\nb: 2\n"), 0o644); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := os.ReadFile(path); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkResolve_Template(b *testing.B) {
	dir := b.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "v.yml.tpl"), []byte(`a: [[ getenv "X" "1" ]]`), 0o644); err != nil {
		b.Fatal(err)
	}
	r := NewResolver(dir)
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := r.Resolve(ctx, "v.yml.tpl", nil); err != nil {
			b.Fatal(err)
		}
	}
}

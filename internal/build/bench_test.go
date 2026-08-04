package build

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"

	"github.com/helmwave/nelmwave/internal/config"
	"github.com/helmwave/nelmwave/internal/plan"
)

// benchConfig builds a manifest with n releases, each carrying one plain and one
// templated values file — the shape of a mid-sized real project.
func benchConfig(b *testing.B, base string, n int) *config.Config {
	b.Helper()
	if err := os.WriteFile(filepath.Join(base, "plain.yml"), []byte("a: 1\n"), 0o644); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "tmpl.yml.tpl"), []byte(`a: [[ getenv "X" "1" ]]`), 0o644); err != nil {
		b.Fatal(err)
	}
	releases := make(map[string]config.Release, n)
	for i := range n {
		releases[fmt.Sprintf("r%d@ns", i)] = config.Release{
			Chart:  config.Chart{Name: "repo/c"},
			Values: []config.FileRef{{Src: "plain.yml"}, {Src: "tmpl.yml.tpl"}},
		}
	}
	return &config.Config{Releases: releases}
}

func BenchmarkArtifacts_50Releases(b *testing.B) {
	base := b.TempDir()
	cfg := benchConfig(b, base, 50)
	ctx := context.Background()
	log := zap.NewNop()
	b.ReportAllocs()
	for b.Loop() {
		out := filepath.Join(b.TempDir(), "out")
		if err := Artifacts(ctx, cfg, plan.FromConfig(cfg), base, out, log); err != nil {
			b.Fatal(err)
		}
	}
}

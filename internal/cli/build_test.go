package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/helmwave/nelmwave/internal/plan"
)

func TestBuildCommand_EndToEnd(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "nelmwave.yml.tpl")
	const src = `
project: demo
releases:
  cache@app:
    labels: { app: redis }
    universal:
      image: redis:[[ getenv "REDIS_TAG" "7" ]]
      service: { port: 6379 }
`
	if err := os.WriteFile(manifest, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out")

	root := NewRootCommand()
	root.SetArgs([]string{"build", "--file", manifest, "--output", out})
	if err := root.Execute(); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	p, err := plan.Read(out)
	if err != nil {
		t.Fatalf("read plan: %v", err)
	}
	if p.Project != "demo" || len(p.Releases) != 1 {
		t.Fatalf("unexpected plan: %+v", p)
	}
	if got := p.Releases["cache@app"].Universal.Image; got != "redis:7" {
		t.Errorf("template not rendered into values, image=%q", got)
	}
}

func TestBuildCommand_ReportsMissingManifest(t *testing.T) {
	root := NewRootCommand()
	root.SetArgs([]string{"build", "--file", filepath.Join(t.TempDir(), "nope.tpl")})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error for missing manifest")
	}
}

package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"

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
    chart:
      name: bitnami/redis
      version: [[ getenv "REDIS_VER" "20.x" ]]
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
	if got := p.Releases["cache@app"].Chart.Version; got != "20.x" {
		t.Errorf("template not rendered into manifest, chart.version=%q", got)
	}
}

func TestBuildCommand_ReportsMissingManifest(t *testing.T) {
	root := NewRootCommand()
	root.SetArgs([]string{"build", "--file", filepath.Join(t.TempDir(), "nope.tpl")})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for missing manifest")
	}
	// A plain "not found" — the underlying stat error would repeat the path.
	if got := err.Error(); strings.Contains(got, "no such file") {
		t.Errorf("missing-manifest error should not carry the raw stat error: %q", got)
	}
}

func TestBuildCommand_ReportsEveryValidationProblem(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "nelmwave.yml")
	// Three distinct problems: a missing chart name, a dangling need, and a
	// cycle. Validate collects them all rather than stopping at the first.
	const src = `
project: broken
releases:
  a@ns:
    chart: {}
    needs:
      releases:
        ghost@ns: {}
  b@ns:
    chart: { name: repo/b }
    needs:
      releases:
        c@ns: {}
  c@ns:
    chart: { name: repo/c }
    needs:
      releases:
        b@ns: {}
`
	if err := os.WriteFile(manifest, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	root := NewRootCommand()
	root.SetArgs([]string{"build", "--file", manifest, "--output", filepath.Join(dir, "out")})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected validation to fail")
	}
	if got := err.Error(); !strings.Contains(got, "3 problem(s)") {
		t.Errorf("want a count of all problems, got %q", got)
	}
}

func TestLogValidationProblems_CountsJoinedErrors(t *testing.T) {
	logger := zap.NewNop()
	if got := logValidationProblems(logger, errors.New("single")); got != 1 {
		t.Errorf("plain error: got %d, want 1", got)
	}
	joined := errors.Join(errors.New("one"), errors.New("two"), errors.New("three"))
	if got := logValidationProblems(logger, joined); got != 3 {
		t.Errorf("joined error: got %d, want 3", got)
	}
}

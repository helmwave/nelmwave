package cli

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/helmwave/nelmwave/internal/config"
	"github.com/helmwave/nelmwave/internal/plan"
)

// complete drives the same hidden __complete command a shell would call.
func complete(t *testing.T, args ...string) []string {
	t.Helper()
	root := NewRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{cobra.ShellCompRequestCmd}, args...))
	if err := root.Execute(); err != nil {
		t.Fatalf("__complete %v: %v", args, err)
	}
	var candidates []string
	for _, line := range strings.Split(out.String(), "\n") {
		// The trailing ":<directive>" line and the human-readable note are not
		// candidates.
		if line == "" || strings.HasPrefix(line, ":") || strings.HasPrefix(line, "Completion ended") {
			continue
		}
		candidates = append(candidates, line)
	}
	return candidates
}

func TestComplete_FixedFlagValues(t *testing.T) {
	if got := complete(t, "--log-format", ""); !slices.Equal(got, []string{"auto", "console", "json"}) {
		t.Errorf("log-format candidates = %v", got)
	}
	if got := complete(t, "--log-level", "w"); !slices.Equal(got, []string{"warn"}) {
		t.Errorf("log-level candidates for \"w\" = %v", got)
	}
}

// -l matches labels from the built plan, so that is what gets suggested.
func TestComplete_SelectorFromPlan(t *testing.T) {
	dir := t.TempDir()
	p := &plan.Plan{Releases: map[string]plan.Release{
		"api@app": {Labels: map[string]string{"app": "api", "tier": "backend"}, Chart: config.Chart{Name: "r/api"}},
		"db@data": {Labels: map[string]string{"app": "postgres", "tier": "db"}, Chart: config.Chart{Name: "r/db"}},
	}}
	if err := p.Write(dir); err != nil {
		t.Fatal(err)
	}

	keys := complete(t, "up", "--output", dir, "-l", "")
	if !slices.Equal(keys, []string{"app=", "tier="}) {
		t.Errorf("label keys = %v", keys)
	}

	values := complete(t, "up", "--output", dir, "-l", "app=")
	if !slices.Equal(values, []string{"app=api", "app=postgres"}) {
		t.Errorf("values of app = %v", values)
	}

	// A selector is comma-separated: the finished part is carried through, since
	// the shell replaces the whole word.
	second := complete(t, "up", "--output", dir, "-l", "app=api,tier=d")
	if !slices.Equal(second, []string{"app=api,tier=db"}) {
		t.Errorf("second term = %v", second)
	}
}

// Without a plan there is nothing to suggest, and completion must stay quiet
// rather than fail or fall back to listing files.
func TestComplete_SelectorWithoutPlan(t *testing.T) {
	if got := complete(t, "up", "--output", t.TempDir(), "-l", ""); len(got) != 0 {
		t.Errorf("expected no candidates, got %v", got)
	}
}

// The commands take no positional arguments, so the shell must not offer files.
func TestComplete_NoPositionalArguments(t *testing.T) {
	for _, cmd := range []string{"up", "down", "diff", "build"} {
		if got := complete(t, cmd, ""); len(got) != 0 {
			t.Errorf("%s offered positional candidates: %v", cmd, got)
		}
	}
}

func TestSplitSelector(t *testing.T) {
	cases := map[string][2]string{
		"":                {"", ""},
		"app":             {"", "app"},
		"app=api":         {"", "app=api"},
		"app=api,":        {"app=api,", ""},
		"app=api,tier=db": {"app=api,", "tier=db"},
	}
	for in, want := range cases {
		prefix, last := splitSelector(in)
		if prefix != want[0] || last != want[1] {
			t.Errorf("splitSelector(%q) = (%q, %q), want (%q, %q)", in, prefix, last, want[0], want[1])
		}
	}
}

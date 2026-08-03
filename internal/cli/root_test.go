package cli

import (
	"testing"
)

func TestNewRootCommand_HasMVPSubcommands(t *testing.T) {
	root := NewRootCommand()

	want := []string{"build", "up", "down", "diff"}
	for _, name := range want {
		if _, _, err := root.Find([]string{name}); err != nil {
			t.Errorf("subcommand %q not wired up: %v", name, err)
		}
	}
}

func TestDiffCommand_HasPlanAlias(t *testing.T) {
	root := NewRootCommand()

	cmd, _, err := root.Find([]string{"plan"})
	if err != nil {
		t.Fatalf("plan alias not resolvable: %v", err)
	}
	if cmd.Name() != "diff" {
		t.Errorf("plan should alias diff, resolved to %q", cmd.Name())
	}
}

package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/helmwave/nelmwave/internal/plan"
	"github.com/helmwave/nelmwave/internal/release"
)

type diffOptions struct {
	output           string
	selector         string
	concurrency      int
	includeNeeds     bool
	detailedExitCode bool
}

func newDiffCommand(g *globalOptions) *cobra.Command {
	o := &diffOptions{}
	cmd := &cobra.Command{
		Use:     "diff",
		Aliases: []string{"plan"},
		Short:   "Show the changes the selected releases would apply",
		Long: `Compute what up would change, without touching the cluster.

diff reads the plan written by build and asks nelm to plan each selected
release. Releases are planned in parallel, bounded by --concurrency; unlike up,
the dependency graph only orders the output, since nothing is applied.

--include-needs widens the selection to what it depends on, exactly as it does
for up, so the preview covers the same set the apply would. Planning changes
nothing, so an unsatisfied dependency is never an error here — it is one for up.

Exit codes:

  0   no error (and, with --detailed-exitcode, no pending changes)
  1   something failed
  2   changes are planned (--detailed-exitcode only)

The exit code 2 convention matches terraform plan and git diff --exit-code, so a
CI job can gate on drift without parsing output.`,
		Example: `  # What would nelmwave up do?
  nelmwave diff

  # Same thing, spelled differently
  nelmwave plan -l 'env=prod'

  # Gate a CI job on drift
  nelmwave diff --detailed-exitcode || echo "drift detected"`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDiff(cmd, g, o)
		},
	}
	f := cmd.Flags()
	f.StringVarP(&o.selector, "selector", "l", "", "k8s-style label selector to filter releases")
	f.IntVar(&o.concurrency, "concurrency", 0, "max releases to plan in parallel (0 = unlimited)")
	f.StringVar(&o.output, "output", plan.DefaultDir, "directory of the built plan")
	f.BoolVar(&o.includeNeeds, "include-needs", false, "also plan the releases the selection depends on")
	f.BoolVar(&o.detailedExitCode, "detailed-exitcode", false, "exit with code 2 when changes are planned")
	return cmd
}

func runDiff(cmd *cobra.Command, g *globalOptions, o *diffOptions) error {
	ctx := cmd.Context()
	logger := loggerFrom(ctx).With(zap.String("phase", "diff"))

	p, err := plan.Read(o.output)
	if err != nil {
		return fmt.Errorf("%w (run `nelmwave build` first)", err)
	}

	return diffReleases(ctx, logger, p, deployOptions{
		output:       o.output,
		selector:     o.selector,
		concurrency:  o.concurrency,
		includeNeeds: o.includeNeeds,
		kubeContext:  g.kubeContext,
		kubeConfig:   g.kubeConfig,
	}, o.detailedExitCode, release.NelmApplier{})
}

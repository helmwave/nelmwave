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
	detailedExitCode bool
}

func newDiffCommand(g *globalOptions) *cobra.Command {
	o := &diffOptions{}
	cmd := &cobra.Command{
		Use:     "diff",
		Aliases: []string{"plan"},
		Short:   "Show the changes the selected releases would apply",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDiff(cmd, g, o)
		},
	}
	f := cmd.Flags()
	f.StringVarP(&o.selector, "selector", "l", "", "k8s-style label selector to filter releases")
	f.IntVar(&o.concurrency, "concurrency", 0, "max releases to plan in parallel (0 = unlimited)")
	f.StringVar(&o.output, "output", plan.DefaultDir, "directory of the built plan")
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
		output:      o.output,
		selector:    o.selector,
		concurrency: o.concurrency,
		kubeContext: g.kubeContext,
		kubeConfig:  g.kubeConfig,
	}, o.detailedExitCode, release.NelmApplier{})
}

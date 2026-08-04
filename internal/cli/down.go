package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/helmwave/nelmwave/internal/plan"
	"github.com/helmwave/nelmwave/internal/release"
)

type downOptions struct {
	output      string
	selector    string
	concurrency int
}

func newDownCommand(g *globalOptions) *cobra.Command {
	o := &downOptions{}
	cmd := &cobra.Command{
		Use:   "down",
		Short: "Uninstall the selected releases in reverse dependency order",
		Long: `Uninstall the selected releases, reversing the dependency graph.

Dependents go first: if api needs postgres, api is removed before postgres.
Independent releases are uninstalled in parallel, bounded by --concurrency.

Unlike up, down does not consider needs policy — it only reverses the edges
within the selection. A dependency outside the selection is left alone, so
narrowing with -l never removes something you did not select.`,
		Example: `  # Tear down everything in the plan
  nelmwave down

  # Remove just the staging releases
  nelmwave down -l 'env=stg'`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDown(cmd, g, o)
		},
	}
	f := cmd.Flags()
	f.StringVarP(&o.selector, "selector", "l", "", "k8s-style label selector to filter releases")
	f.IntVar(&o.concurrency, "concurrency", 0, "max releases to uninstall in parallel (0 = unlimited)")
	f.StringVar(&o.output, "output", plan.DefaultDir, "directory of the built plan")
	return cmd
}

func runDown(cmd *cobra.Command, g *globalOptions, o *downOptions) error {
	ctx := cmd.Context()
	logger := loggerFrom(ctx).With(zap.String("phase", "down"))

	p, err := plan.Read(o.output)
	if err != nil {
		return fmt.Errorf("%w (run `nelmwave build` first)", err)
	}

	return deploy(ctx, logger, p, deployOptions{
		output:      o.output,
		selector:    o.selector,
		concurrency: o.concurrency,
		kubeContext: g.kubeContext,
		kubeConfig:  g.kubeConfig,
	}, release.NelmApplier{}, opUninstall)
}

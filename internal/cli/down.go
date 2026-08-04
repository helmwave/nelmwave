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

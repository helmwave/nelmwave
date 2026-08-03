package cli

import (
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

type upOptions struct {
	selector     string
	concurrency  int
	build        bool
	includeNeeds bool
	dryRun       bool
}

func newUpCommand(_ *globalOptions) *cobra.Command {
	o := &upOptions{}
	cmd := &cobra.Command{
		Use:   "up",
		Short: "Deploy the selected releases in dependency order",
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := loggerFrom(cmd.Context()).With(zap.String("phase", "up"))
			logger.Info("up invoked",
				zap.String("selector", o.selector),
				zap.Int("concurrency", o.concurrency),
				zap.Bool("dry-run", o.dryRun),
			)
			return errNotImplemented
		},
	}
	f := cmd.Flags()
	f.StringVarP(&o.selector, "selector", "l", "", "k8s-style label selector to filter releases")
	f.IntVar(&o.concurrency, "concurrency", 0, "max releases to deploy in parallel (0 = auto)")
	f.BoolVar(&o.build, "build", false, "run build before up")
	f.BoolVar(&o.includeNeeds, "include-needs", false, "pull in needed releases even if filtered out")
	f.BoolVar(&o.dryRun, "dry-run", false, "plan instead of applying")
	return cmd
}

package cli

import (
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

type diffOptions struct {
	selector         string
	detailedExitCode bool
}

func newDiffCommand(_ *globalOptions) *cobra.Command {
	o := &diffOptions{}
	cmd := &cobra.Command{
		Use:     "diff",
		Aliases: []string{"plan"},
		Short:   "Show the changes the selected releases would apply",
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := loggerFrom(cmd.Context()).With(zap.String("phase", "diff"))
			logger.Info("diff invoked",
				zap.String("selector", o.selector),
				zap.Bool("detailed-exitcode", o.detailedExitCode),
			)
			return errNotImplemented
		},
	}
	f := cmd.Flags()
	f.StringVarP(&o.selector, "selector", "l", "", "k8s-style label selector to filter releases")
	f.BoolVar(&o.detailedExitCode, "detailed-exitcode", false, "exit non-zero when changes are planned")
	return cmd
}

package cli

import (
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

type downOptions struct {
	selector    string
	concurrency int
}

func newDownCommand(_ *globalOptions) *cobra.Command {
	o := &downOptions{}
	cmd := &cobra.Command{
		Use:   "down",
		Short: "Uninstall the selected releases in reverse dependency order",
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := loggerFrom(cmd.Context()).With(zap.String("phase", "down"))
			logger.Info("down invoked",
				zap.String("selector", o.selector),
				zap.Int("concurrency", o.concurrency),
			)
			return errNotImplemented
		},
	}
	f := cmd.Flags()
	f.StringVarP(&o.selector, "selector", "l", "", "k8s-style label selector to filter releases")
	f.IntVar(&o.concurrency, "concurrency", 0, "max releases to uninstall in parallel (0 = auto)")
	return cmd
}

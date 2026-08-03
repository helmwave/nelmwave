package cli

import (
	"errors"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

// errNotImplemented marks skeleton commands whose milestone is not done yet.
var errNotImplemented = errors.New("not implemented yet")

type buildOptions struct {
	file   string
	output string
}

func newBuildCommand(_ *globalOptions) *cobra.Command {
	o := &buildOptions{}
	cmd := &cobra.Command{
		Use:   "build",
		Short: "Render nelmwave.yml.tpl and write the plan to .nelmwave/",
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := loggerFrom(cmd.Context()).With(zap.String("phase", "build"))
			logger.Info("build invoked", zap.String("file", o.file), zap.String("output", o.output))
			return errNotImplemented
		},
	}
	cmd.Flags().StringVar(&o.file, "file", "nelmwave.yml.tpl", "path to the nelmwave manifest (.tpl or plain yml)")
	cmd.Flags().StringVar(&o.output, "output", ".nelmwave", "directory for the built plan and artifacts")
	return cmd
}

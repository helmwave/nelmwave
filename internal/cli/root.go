// Package cli wires up the cobra command tree for the nelmwave binary.
package cli

import (
	"context"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/helmwave/nelmwave/internal/log"
	"github.com/helmwave/nelmwave/internal/version"
)

// globalOptions holds flags shared by every subcommand.
type globalOptions struct {
	logLevel    string
	logFormat   string
	kubeContext string
	kubeConfig  string

	logger *zap.Logger
}

// NewRootCommand builds the root `nelmwave` command with all subcommands
// attached. It is the single entry point used by cmd/nelmwave.
func NewRootCommand() *cobra.Command {
	opts := &globalOptions{}

	cmd := &cobra.Command{
		Use:           "nelmwave",
		Short:         "Declarative release orchestrator on top of nelm",
		Long:          "nelmwave manages many releases from a single declarative nelmwave.yml manifest, rendered through gomplate and applied via nelm.",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version.String(),
		// Build the logger once, before any subcommand RunE, and stash it in
		// the command context so subcommands can pull it out.
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			logger, err := log.New(log.Options{
				Level:  opts.logLevel,
				Format: log.Format(opts.logFormat),
			})
			if err != nil {
				return err
			}
			opts.logger = logger
			cmd.SetContext(withLogger(cmd.Context(), logger))
			return nil
		},
		PersistentPostRun: func(_ *cobra.Command, _ []string) {
			if opts.logger != nil {
				// Sync errors on stderr are expected on some platforms; ignore.
				_ = opts.logger.Sync()
			}
		},
	}

	flags := cmd.PersistentFlags()
	flags.StringVar(&opts.logLevel, "log-level", "info", "log level: debug|info|warn|error")
	flags.StringVar(&opts.logFormat, "log-format", "auto", "log format: auto|console|json")
	flags.StringVar(&opts.kubeContext, "kube-context", "", "name of the kubeconfig context to use")
	flags.StringVar(&opts.kubeConfig, "kube-config", "", "path to the kubeconfig file")

	cmd.AddCommand(
		newBuildCommand(opts),
		newUpCommand(opts),
		newDownCommand(opts),
		newDiffCommand(opts),
	)

	return cmd
}

// Execute runs the root command with a context cancelled on SIGINT/SIGTERM,
// giving in-flight work a chance to stop gracefully. It returns the process
// exit code.
func Execute() int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	root := NewRootCommand()
	if err := root.ExecuteContext(ctx); err != nil {
		// Logger may not be built yet (e.g. flag parse error); fall back to stderr.
		if logger := loggerFrom(root.Context()); logger != nil {
			logger.Error("command failed", zap.Error(err))
		}
		return 1
	}
	return 0
}

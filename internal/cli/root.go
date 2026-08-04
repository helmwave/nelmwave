// Package cli wires up the cobra command tree for the nelmwave binary.
package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
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
// attached, using a fresh set of global options. Useful for tests.
func NewRootCommand() *cobra.Command {
	return newRootCommand(&globalOptions{})
}

// newRootCommand builds the root command around a caller-provided options
// struct, so Execute can read the constructed logger back out (opts.logger)
// after the run to report top-level errors.
func newRootCommand(opts *globalOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "nelmwave",
		Short: "Declarative release orchestrator on top of nelm",
		Long: `nelmwave manages many releases from a single declarative nelmwave.yml manifest.

The manifest is rendered with gomplate ([[ ]] delimiters), values and companion
files are resolved from arbitrary datasources into .nelmwave/, and releases are
applied through nelm in dependency order — independent branches in parallel.

The usual loop:

  nelmwave build     # render, validate, resolve datasources into .nelmwave/
  nelmwave diff      # preview what would change
  nelmwave up        # apply it

Runtime commands (up/down/diff) never re-render the manifest: they read the
plan that build wrote, so what you reviewed is what gets applied.`,
		Example: `  # Build the plan and inspect it
  nelmwave build && cat .nelmwave/planfile.yml

  # Deploy only the production API tier
  nelmwave up -l 'app=api,env=prod'

  # Fail a CI job when live state drifted from the manifest
  nelmwave diff --detailed-exitcode`,
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

	opts := &globalOptions{}
	root := newRootCommand(opts)
	if err := root.ExecuteContext(ctx); err != nil {
		// An exitError carries a specific exit code and is not a failure (e.g.
		// diff --detailed-exitcode reporting planned changes as code 2).
		var ee *exitError
		if errors.As(err, &ee) {
			if opts.logger != nil {
				opts.logger.Info(ee.message, zap.Int("exit-code", ee.code))
			}
			return ee.code
		}
		// opts.logger is nil only if we failed before PersistentPreRunE (e.g. a
		// flag parse error), in which case cobra already printed the message.
		if opts.logger != nil {
			opts.logger.Error("command failed", zap.Error(err))
		} else {
			fmt.Fprintln(os.Stderr, "Error:", err)
		}
		return 1
	}
	return 0
}

// exitError requests a specific process exit code without being treated as a
// command failure.
type exitError struct {
	code    int
	message string
}

func (e *exitError) Error() string { return e.message }

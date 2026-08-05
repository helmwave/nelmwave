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
	// kube holds the rest of the cluster-connection flags; kubeContext stays
	// separate because a release's uniqname can override it.
	kube kubeOptions

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
			// Environment first: every flag below, including --log-level, can be
			// set through NELMWAVE_*, and nothing has read a flag value yet.
			if err := applyEnv(cmd); err != nil {
				return err
			}

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
	opts.kube.register(flags)

	cmd.AddCommand(
		newBuildCommand(opts),
		newUpCommand(opts),
		newDownCommand(opts),
		newDiffCommand(opts),
	)

	// cobra contributes the `completion` command itself; this fills in the value
	// completions it cannot infer (kube contexts, plan labels).
	registerCompletions(cmd, opts)

	// Documents the NELMWAVE_* variable behind each flag in --help. Must come
	// after the tree is assembled, since it walks the subcommands.
	annotateEnvUsage(cmd)

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

// ExitCode maps an error returned by a command to the exit code Execute would
// report for it: 0 for nil, the carried code for a signalling error (2 from
// diff --detailed-exitcode), 1 for anything else. Callers embedding the command
// tree — the end-to-end suite, or another binary — need this to tell "changes
// are planned" apart from "the command failed".
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exitError
	if errors.As(err, &ee) {
		return ee.code
	}
	return 1
}

// exitError requests a specific process exit code without being treated as a
// command failure.
type exitError struct {
	code    int
	message string
}

func (e *exitError) Error() string { return e.message }

package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// envPrefix namespaces every variable nelmwave reads, so nothing it picks up can
// collide with an unrelated variable in a CI job.
const envPrefix = "NELMWAVE_"

// cobraOwnedFlags are the flags cobra contributes on its own. They exist to
// print something and exit, so an environment variable behind them would be a
// booby trap: NELMWAVE_HELP left over in a shell would turn every command into
// `--help`.
var cobraOwnedFlags = map[string]bool{
	"help":    true,
	"version": true,
}

// envVarName maps a flag to its variable: --kube-request-timeout is read from
// NELMWAVE_KUBE_REQUEST_TIMEOUT. Every flag has one by construction — there is
// no per-flag opt-in to forget — and env_test.go asserts that over the whole
// command tree.
//
// The mapping is flat: no command component, so --output means the same thing in
// build, up, down and diff and NELMWAVE_OUTPUT covers all four. One variable per
// flag, one flag per variable, nothing to disambiguate.
func envVarName(flag string) string {
	return envPrefix + strings.ToUpper(strings.ReplaceAll(flag, "-", "_"))
}

// applyEnv fills in every flag the caller did not pass on the command line from
// its environment variable. Precedence is the usual one: command line, then
// environment, then the flag's default.
//
// It runs from the root PersistentPreRunE, before anything reads a flag value.
func applyEnv(cmd *cobra.Command) error {
	flags := cmd.Flags()

	var errs []error
	flags.VisitAll(func(f *pflag.Flag) {
		if f.Changed || cobraOwnedFlags[f.Name] {
			return
		}

		name := envVarName(f.Name)
		value, ok := os.LookupEnv(name)
		// An empty variable counts as unset. CI templating produces empty
		// variables by accident all the time, and `NELMWAVE_CONCURRENCY=` should
		// not be the reason a deploy refuses to start.
		if !ok || value == "" {
			return
		}

		// FlagSet.Set rather than Flag.Value.Set: it also marks the flag as
		// changed, so code that asks "did the caller ask for this?" — the --file
		// lookup in build and up — treats an environment variable as an answer.
		if err := flags.Set(f.Name, value); err != nil {
			errs = append(errs, fmt.Errorf("%s=%q: %w", name, value, err))
		}
	})

	return errors.Join(errs...)
}

// annotateEnvUsage appends the variable name to each flag's help text, so
// `nelmwave up --help` documents the environment as well as the flags. Called
// once, after the command tree is assembled.
func annotateEnvUsage(cmd *cobra.Command) {
	annotate := func(f *pflag.Flag) {
		// A persistent flag shows up in both flag sets below, and a global one in
		// every command's help; the prefix check keeps the second visit from
		// appending a second copy.
		if cobraOwnedFlags[f.Name] || strings.Contains(f.Usage, envPrefix) {
			return
		}
		f.Usage = fmt.Sprintf("%s [env: %s]", f.Usage, envVarName(f.Name))
	}

	cmd.PersistentFlags().VisitAll(annotate)
	cmd.Flags().VisitAll(annotate)

	for _, sub := range cmd.Commands() {
		annotateEnvUsage(sub)
	}
}

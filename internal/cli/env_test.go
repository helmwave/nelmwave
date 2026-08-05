package cli

import (
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func TestEnvVarName(t *testing.T) {
	for flag, want := range map[string]string{
		"file":                  "NELMWAVE_FILE",
		"log-level":             "NELMWAVE_LOG_LEVEL",
		"kube-request-timeout":  "NELMWAVE_KUBE_REQUEST_TIMEOUT",
		"detailed-exitcode":     "NELMWAVE_DETAILED_EXITCODE",
		"show-sensitive-diffs":  "NELMWAVE_SHOW_SENSITIVE_DIFFS",
		"kube-impersonate-user": "NELMWAVE_KUBE_IMPERSONATE_USER",
	} {
		if got := envVarName(flag); got != want {
			t.Errorf("envVarName(%q) = %q, want %q", flag, got, want)
		}
	}
}

// One variable per flag, and the same one whichever command reads it: --output
// is NELMWAVE_OUTPUT under build, up, down and diff alike.
func TestEnv_OneVariableCoversEveryCommand(t *testing.T) {
	t.Setenv("NELMWAVE_OUTPUT", "shared")

	for _, name := range []string{"build", "up", "down", "diff"} {
		root := NewRootCommand()
		cmd := commandNamed(t, root, name)
		if err := cmd.ParseFlags(nil); err != nil {
			t.Fatalf("%s: parse flags: %v", name, err)
		}
		if err := applyEnv(cmd); err != nil {
			t.Fatalf("%s: applyEnv: %v", name, err)
		}
		if got := cmd.Flags().Lookup("output").Value.String(); got != "shared" {
			t.Errorf("%s output = %q, want shared", name, got)
		}
	}
}

// sampleValue returns something the flag will accept, so the audit below can set
// every flag regardless of its type.
func sampleValue(f *pflag.Flag) string {
	switch f.Value.Type() {
	case "bool":
		return "true"
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64", "count":
		return "7"
	case "float32", "float64":
		return "1.5"
	case "duration":
		return "42s"
	case "stringSlice", "stringArray":
		return "one,two"
	case "stringToString":
		return "k=v"
	default:
		// string and anything string-shaped nelmwave adds later.
		return "sample"
	}
}

// flagSets returns every flag a user can pass, labelled by the command that
// declares it. The root entry carries the persistent flags — --log-level and the
// whole kube-* family — which are NOT in root.Flags() until a command runs, so
// they have to be collected from PersistentFlags explicitly. Missing that is how
// an audit like this quietly passes while covering nothing that matters.
func flagSets(root *cobra.Command) map[string]*pflag.FlagSet {
	rootFlags := pflag.NewFlagSet("nelmwave", pflag.ContinueOnError)
	rootFlags.AddFlagSet(root.PersistentFlags())
	rootFlags.AddFlagSet(root.Flags())

	out := map[string]*pflag.FlagSet{"nelmwave": rootFlags}

	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		for _, sub := range cmd.Commands() {
			// LocalFlags, not Flags: each command is audited for what it declares
			// itself, and the inherited ones are covered by the root entry plus
			// TestEnv_InheritedFlagsReachSubcommands.
			out[sub.Name()] = sub.LocalFlags()
			walk(sub)
		}
	}
	walk(root)

	return out
}

// TestEnv_EveryFlagIsSettableFromTheEnvironment is the standing guarantee: for
// every flag of every command there is a NELMWAVE_* variable that sets it. A new
// flag is covered the moment it is registered — and if that ever stops being
// true, this test is what says so.
func TestEnv_EveryFlagIsSettableFromTheEnvironment(t *testing.T) {
	for cmdName, fs := range flagSets(NewRootCommand()) {
		fs.VisitAll(func(f *pflag.Flag) {
			if cobraOwnedFlags[f.Name] {
				return
			}

			t.Run(cmdName+"/"+f.Name, func(t *testing.T) {
				variable := envVarName(f.Name)
				if !strings.HasPrefix(variable, envPrefix) {
					t.Fatalf("flag --%s maps to %q, which is not %s-prefixed", f.Name, variable, envPrefix)
				}

				want := sampleValue(f)
				t.Setenv(variable, want)

				// A fresh tree per flag: applyEnv mutates flag state, and one
				// command's flags must not leak into the next assertion.
				root := NewRootCommand()
				cmd := commandNamed(t, root, cmdName)

				// What cobra does before PersistentPreRunE: merges the persistent
				// and inherited flags into cmd.Flags(), which is what applyEnv
				// walks. Without it the root's own flags are invisible here.
				if err := cmd.ParseFlags(nil); err != nil {
					t.Fatalf("parse flags: %v", err)
				}

				if err := applyEnv(cmd); err != nil {
					t.Fatalf("applyEnv: %v", err)
				}

				got := cmd.Flags().Lookup(f.Name)
				if got == nil {
					t.Fatalf("flag --%s vanished from %s", f.Name, cmdName)
				}
				if !got.Changed {
					t.Errorf("%s did not set --%s", variable, f.Name)
				}
				if !valueHolds(got, want) {
					t.Errorf("%s=%q left --%s at %q", variable, want, f.Name, got.Value.String())
				}
			})
		})
	}
}

// valueHolds reports whether f ended up carrying want. Slice flags stringify as
// "[one,two]", so they are compared by their elements rather than verbatim.
func valueHolds(f *pflag.Flag, want string) bool {
	got := f.Value.String()
	if got == want {
		return true
	}
	for _, part := range strings.Split(want, ",") {
		if !strings.Contains(got, part) {
			return false
		}
	}
	return true
}

func commandNamed(t *testing.T, root *cobra.Command, name string) *cobra.Command {
	t.Helper()
	if name == "nelmwave" {
		return root
	}
	for _, sub := range root.Commands() {
		if sub.Name() == name {
			return sub
		}
	}
	t.Fatalf("command %q not found", name)
	return nil
}

// A persistent flag declared on the root has to be settable from the environment
// while running a subcommand — that is where every kube-* flag is actually used.
func TestEnv_InheritedFlagsReachSubcommands(t *testing.T) {
	t.Setenv("NELMWAVE_KUBE_REQUEST_TIMEOUT", "42s")
	t.Setenv("NELMWAVE_KUBE_CONTEXT", "from-env")

	root := NewRootCommand()
	up := commandNamed(t, root, "up")
	if err := up.ParseFlags(nil); err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	if err := applyEnv(up); err != nil {
		t.Fatalf("applyEnv: %v", err)
	}

	if got := up.Flags().Lookup("kube-request-timeout").Value.String(); got != "42s" {
		t.Errorf("kube-request-timeout = %q, want 42s", got)
	}
	if got := up.Flags().Lookup("kube-context").Value.String(); got != "from-env" {
		t.Errorf("kube-context = %q, want from-env", got)
	}
}

func TestEnv_CommandLineWinsOverTheEnvironment(t *testing.T) {
	t.Setenv("NELMWAVE_OUTPUT", "from-env")

	root := NewRootCommand()
	build := commandNamed(t, root, "build")
	if err := build.Flags().Parse([]string{"--output", "from-flag"}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := applyEnv(build); err != nil {
		t.Fatalf("applyEnv: %v", err)
	}

	if got := build.Flags().Lookup("output").Value.String(); got != "from-flag" {
		t.Errorf("output = %q, want the command-line value", got)
	}
}

func TestEnv_EmptyValueIsIgnored(t *testing.T) {
	t.Setenv("NELMWAVE_CONCURRENCY", "")

	root := NewRootCommand()
	up := commandNamed(t, root, "up")
	if err := applyEnv(up); err != nil {
		t.Fatalf("applyEnv: %v", err)
	}

	f := up.Flags().Lookup("concurrency")
	if f.Changed {
		t.Error("an empty NELMWAVE_CONCURRENCY marked --concurrency as set")
	}
	if got := f.Value.String(); got != "0" {
		t.Errorf("concurrency = %q, want the default 0", got)
	}
}

func TestEnv_InvalidValueNamesTheVariable(t *testing.T) {
	t.Setenv("NELMWAVE_CONCURRENCY", "not-a-number")

	root := NewRootCommand()
	err := applyEnv(commandNamed(t, root, "up"))
	if err == nil {
		t.Fatal("applyEnv accepted a non-numeric concurrency")
	}
	if !strings.Contains(err.Error(), "NELMWAVE_CONCURRENCY") {
		t.Errorf("error %q does not name the offending variable", err)
	}
}

// The environment has to be read before anything consumes a flag. --log-level is
// the earliest consumer: the root PersistentPreRunE builds the logger from it, so
// a rejected level proves applyEnv ran first.
func TestEnv_AppliesBeforeTheLoggerIsBuilt(t *testing.T) {
	t.Setenv("NELMWAVE_LOG_LEVEL", "bogus")

	root := NewRootCommand()
	root.SetArgs([]string{"build"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)

	err := root.Execute()
	if err == nil {
		t.Fatal("execute accepted a bogus log level from the environment")
	}
	if !strings.Contains(err.Error(), "log level") {
		t.Errorf("error %q does not come from the log level parse", err)
	}
}

func TestEnv_UsageDocumentsTheVariable(t *testing.T) {
	root := NewRootCommand()

	if got := root.PersistentFlags().Lookup("log-level").Usage; !strings.Contains(got, "NELMWAVE_LOG_LEVEL") {
		t.Errorf("--log-level usage %q does not mention its variable", got)
	}

	build := commandNamed(t, root, "build")
	if got := build.Flags().Lookup("file").Usage; !strings.Contains(got, "NELMWAVE_FILE") {
		t.Errorf("--file usage %q does not mention its variable", got)
	}

	// The flags cobra owns must stay clean: they are not env-backed.
	root.InitDefaultHelpFlag()
	if got := root.Flags().Lookup("help").Usage; strings.Contains(got, envPrefix) {
		t.Errorf("--help usage %q advertises an environment variable", got)
	}
}

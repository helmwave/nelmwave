package cli

import (
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/helmwave/nelmwave/internal/plan"
)

// registerCompletions teaches the shell what the flags accept. cobra already
// completes command and flag names on its own; what it cannot guess is the
// values — the contexts in your kubeconfig, the labels in your plan.
//
// Every function here has to be fast and silent: it runs on a keypress, and a
// diagnostic printed to stdout would be parsed as a completion candidate.
func registerCompletions(root *cobra.Command, g *globalOptions) {
	// Only "help" and "completion" take a positional argument; for the rest,
	// stop the shell from falling back to listing files.
	for _, cmd := range root.Commands() {
		if cmd.Name() == "help" || cmd.Name() == "completion" {
			continue
		}
		cmd.ValidArgsFunction = noComplete
	}

	_ = root.RegisterFlagCompletionFunc("log-level", fixed("debug", "info", "warn", "error"))
	_ = root.RegisterFlagCompletionFunc("log-format", fixed("auto", "console", "json"))
	_ = root.RegisterFlagCompletionFunc("kube-context",
		func(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return withPrefix(g.kube.connection().ContextNames(), toComplete), cobra.ShellCompDirectiveNoFileComp
		})

	// Paths: let the shell complete them, but narrow the suffixes where we can.
	for _, name := range []string{"kube-config", "kube-ca", "kube-cert", "kube-key", "kube-token-path"} {
		_ = root.MarkPersistentFlagFilename(name)
	}

	for _, cmd := range root.Commands() {
		f := cmd.Flags()
		if f.Lookup("selector") != nil {
			_ = cmd.RegisterFlagCompletionFunc("selector", completeSelector(cmd))
		}
		if f.Lookup("file") != nil {
			_ = cmd.MarkFlagFilename("file", "yml", "yaml", "tpl")
		}
		if f.Lookup("output") != nil {
			_ = cmd.MarkFlagDirname("output")
		}
	}
}

// fixed completes a closed set of values.
func fixed(values ...string) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return withPrefix(values, toComplete), cobra.ShellCompDirectiveNoFileComp
	}
}

// withPrefix keeps the candidates the user has already started typing. Shells
// filter too, but not all of them do it the same way, so filter here as well.
func withPrefix(values []string, toComplete string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if strings.HasPrefix(v, toComplete) {
			out = append(out, v)
		}
	}
	return out
}

func noComplete(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return nil, cobra.ShellCompDirectiveNoFileComp
}

// completeSelector suggests label keys and key=value pairs taken from the built
// plan, which is what -l actually matches against. Without a plan there is
// nothing to suggest — the shell then just stays quiet.
//
// A selector is a comma-separated list, so only the part after the last comma is
// completed and the rest is carried through: the shell replaces the whole word.
func completeSelector(cmd *cobra.Command) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		dir := plan.DefaultDir
		if f := cmd.Flags().Lookup("output"); f != nil && f.Value.String() != "" {
			dir = f.Value.String()
		}
		p, err := plan.Read(dir)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		prefix, last := splitSelector(toComplete)

		// Key set and the values seen for each key, across every release.
		values := map[string]map[string]struct{}{}
		for _, rel := range p.Releases {
			for k, v := range rel.Labels {
				if values[k] == nil {
					values[k] = map[string]struct{}{}
				}
				values[k][v] = struct{}{}
			}
		}

		var out []string
		// Before the "=", offer keys; after it, the values of that key.
		if key, val, ok := strings.Cut(last, "="); ok {
			for v := range values[key] {
				if strings.HasPrefix(v, val) {
					out = append(out, prefix+key+"="+v)
				}
			}
		} else {
			for k := range values {
				if strings.HasPrefix(k, last) {
					out = append(out, prefix+k+"=")
				}
			}
		}
		sort.Strings(out)

		// NoSpace: a bare "key=" is not a finished selector, and neither is a
		// pair the user may want to extend with a comma.
		return out, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace
	}
}

// splitSelector splits a partial selector into the finished part (kept verbatim,
// trailing comma included) and the term being typed.
func splitSelector(s string) (prefix, last string) {
	if i := strings.LastIndex(s, ","); i >= 0 {
		return s[:i+1], s[i+1:]
	}
	return "", s
}

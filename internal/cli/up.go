package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/helmwave/nelmwave/internal/plan"
	"github.com/helmwave/nelmwave/internal/release"
)

type upOptions struct {
	file         string
	output       string
	selector     string
	concurrency  int
	build        bool
	includeNeeds bool
	dryRun       bool
}

func newUpCommand(g *globalOptions) *cobra.Command {
	o := &upOptions{}
	cmd := &cobra.Command{
		Use:   "up",
		Short: "Deploy the selected releases in dependency order",
		Long: `Install or upgrade the selected releases, respecting the dependency graph.

up reads the plan written by build (pass --build to refresh it first) and never
re-renders the manifest itself. Releases with no dependency between them are
applied in parallel; --concurrency bounds how many run at once. A failure stops
that branch of the graph — releases that depend on the failed one are skipped,
unrelated branches keep going.

Dependencies outside the selection: a strict need is an error, a non-strict one
is dropped with a warning, and --include-needs pulls both back into the run.`,
		Example: `  # Deploy everything
  nelmwave up

  # Deploy one tier, dragging in whatever it depends on
  nelmwave up -l 'tier=frontend' --include-needs

  # Rebuild the plan and apply it, two releases at a time
  nelmwave up --build --concurrency 2

  # Preview instead of applying (same as nelmwave diff)
  nelmwave up --dry-run`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runUp(cmd, g, o)
		},
	}
	f := cmd.Flags()
	f.StringVarP(&o.selector, "selector", "l", "", "k8s-style label selector to filter releases")
	f.IntVar(&o.concurrency, "concurrency", 0, "max releases to deploy in parallel (0 = unlimited)")
	f.BoolVar(&o.build, "build", false, "run build before up")
	f.BoolVar(&o.includeNeeds, "include-needs", false, "pull in needed releases even if filtered out")
	f.BoolVar(&o.dryRun, "dry-run", false, "plan instead of applying")
	f.StringVar(&o.output, "output", plan.DefaultDir, "directory of the built plan")
	f.StringVar(&o.file, "file", "nelmwave.yml.tpl", "manifest to build when --build is set")
	return cmd
}

func runUp(cmd *cobra.Command, g *globalOptions, o *upOptions) error {
	ctx := cmd.Context()
	logger := loggerFrom(ctx).With(zap.String("phase", "up"))

	if o.build {
		manifest, err := resolveManifest(o.file, cmd.Flags().Changed("file"))
		if err != nil {
			return err
		}
		if err := buildPlan(ctx, manifest, o.output, logger); err != nil {
			return err
		}
	}

	p, err := plan.Read(o.output)
	if err != nil {
		return fmt.Errorf("%w (run `nelmwave build` first, or pass --build)", err)
	}

	opts := deployOptions{
		output:       o.output,
		selector:     o.selector,
		concurrency:  o.concurrency,
		includeNeeds: o.includeNeeds,
		kubeContext:  g.kubeContext,
		kubeConfig:   g.kubeConfig,
	}

	// --dry-run plans instead of applying.
	if o.dryRun {
		return diffReleases(ctx, logger, p, opts, false, release.NelmApplier{})
	}
	return deploy(ctx, logger, p, opts, release.NelmApplier{}, opInstall)
}

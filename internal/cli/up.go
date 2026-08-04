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

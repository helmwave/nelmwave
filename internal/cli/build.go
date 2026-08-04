package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/helmwave/nelmwave/internal/build"
	"github.com/helmwave/nelmwave/internal/config"
	"github.com/helmwave/nelmwave/internal/plan"
	"github.com/helmwave/nelmwave/internal/tpl"
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
			return runBuild(cmd, o)
		},
	}
	cmd.Flags().StringVar(&o.file, "file", "nelmwave.yml.tpl", "path to the nelmwave manifest (.tpl or plain yml)")
	cmd.Flags().StringVar(&o.output, "output", plan.DefaultDir, "directory for the built plan and artifacts")
	return cmd
}

func runBuild(cmd *cobra.Command, o *buildOptions) error {
	ctx := cmd.Context()
	logger := loggerFrom(ctx).With(zap.String("phase", "build"))

	manifest, err := resolveManifest(o.file, cmd.Flags().Changed("file"))
	if err != nil {
		return err
	}
	return buildPlan(ctx, manifest, o.output, logger)
}

// buildPlan renders and validates manifest, resolves its datasources, and
// writes the plan and artifacts to output. Shared by `build` and `up --build`.
func buildPlan(ctx context.Context, manifest, output string, logger *zap.Logger) error {
	logger.Info("building", zap.String("file", manifest), zap.String("output", output))

	src, err := os.ReadFile(manifest)
	if err != nil {
		return fmt.Errorf("read manifest %q: %w", manifest, err)
	}

	// Render only templated manifests; a plain .yml is loaded verbatim.
	rendered := src
	if isTemplate(manifest) {
		rendered, err = tpl.Render(ctx, filepath.Base(manifest), src, tpl.Options{})
		if err != nil {
			return err
		}
		logger.Debug("manifest rendered", zap.Int("bytes", len(rendered)))
	}

	cfg, err := config.Parse(rendered)
	if err != nil {
		return err
	}
	if err := config.Validate(cfg); err != nil {
		return fmt.Errorf("invalid manifest:\n%w", err)
	}

	p := plan.FromConfig(cfg)

	// Resolve values/store datasources relative to the manifest directory.
	if err := build.Artifacts(ctx, cfg, p, filepath.Dir(manifest), output, logger); err != nil {
		return err
	}

	if err := p.Write(output); err != nil {
		return err
	}

	logger.Info("plan written",
		zap.String("path", filepath.Join(output, plan.PlanfileName)),
		zap.Int("releases", len(p.Releases)),
	)
	return nil
}

// isTemplate reports whether path should be rendered through gomplate.
func isTemplate(path string) bool {
	return strings.HasSuffix(path, ".tpl")
}

// resolveManifest returns the manifest path to build. When the user did not set
// --file and the default .tpl is absent, it falls back to a plain nelmwave.yml.
func resolveManifest(file string, changed bool) (string, error) {
	if changed {
		if _, err := os.Stat(file); err != nil {
			return "", fmt.Errorf("manifest %q not found: %w", file, err)
		}
		return file, nil
	}
	if _, err := os.Stat(file); err == nil {
		return file, nil
	}
	fallback := "nelmwave.yml"
	if _, err := os.Stat(fallback); err == nil {
		return fallback, nil
	}
	return "", fmt.Errorf("no manifest found (looked for %q and %q)", file, fallback)
}

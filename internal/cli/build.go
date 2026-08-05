package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/helmwave/nelmwave/internal/build"
	"github.com/helmwave/nelmwave/internal/config"
	"github.com/helmwave/nelmwave/internal/plan"
	"github.com/helmwave/nelmwave/internal/tpl"
)

type buildOptions struct {
	file   string
	output string
}

func newBuildCommand(_ *globalOptions) *cobra.Command {
	o := &buildOptions{}
	cmd := &cobra.Command{
		Use:   "build",
		Short: "Render nelmwave.yml.tpl and write the plan to .nelmwave/",
		Long: `Render the manifest, validate it, resolve every datasource, and write the plan.

build is the only command that renders templates and reaches out to datasources.
It produces, under --output (default .nelmwave/):

  planfile.yml            the resolved plan: releases, dependency edges, artifacts
  values/<release>/...    values files, in merge order
  stores/<release>/...    companion files declared in stores:

Values and store artifacts are rebuilt from scratch on every run, so sources
removed from the manifest leave nothing behind. Within a release, stores resolve
first, then values; each resolved artifact is registered as a gomplate
datasource ("stores/<name>", "values/<name>") that later *.tpl artifacts of the
same release can pull in via ds/include.

With no --file, build looks for nelmwave.yml.tpl and falls back to nelmwave.yml.`,
		Example: `  # Build from nelmwave.yml.tpl in the current directory
  nelmwave build

  # Environment variables reach the manifest through gomplate
  ENV=stg nelmwave build

  # Build a specific manifest into a specific directory
  nelmwave build --file manifests/prod.yml.tpl --output .nelmwave-prod`,
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
		n := logValidationProblems(logger, err)
		return fmt.Errorf("invalid manifest %q: %d problem(s), see the log above", manifest, n)
	}

	warnEmbeddedDriverPasswords(logger, cfg)

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

// warnEmbeddedDriverPasswords points out a driverURL carrying its password
// inline. It is not an error — it works — but build writes the manifest into
// the planfile, so the password ends up in cleartext on disk and in whatever CI
// keeps as an artifact. libpq reads PGPASSWORD, which keeps it out of both.
func warnEmbeddedDriverPasswords(logger *zap.Logger, cfg *config.Config) {
	for _, key := range sortedReleaseKeys(cfg) {
		d, err := config.ParseDriverURL(cfg.Releases[key].DriverURL)
		if err == nil && d.HasPassword {
			logger.Warn("driverURL embeds a password; it will be written to the planfile in cleartext "+
				"(pass it via PGPASSWORD instead)",
				zap.String("release", key))
		}
	}
}

// sortedReleaseKeys returns the release keys in deterministic order.
func sortedReleaseKeys(cfg *config.Config) []string {
	keys := make([]string, 0, len(cfg.Releases))
	for k := range cfg.Releases {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// logValidationProblems logs every problem carried by a joined validation error
// as its own record and returns how many there were. Validate collects all
// problems at once; emitting them separately keeps each one readable in console
// format and greppable in json, instead of one escaped multi-line string.
func logValidationProblems(logger *zap.Logger, err error) int {
	var joined interface{ Unwrap() []error }
	if !errors.As(err, &joined) {
		logger.Error("manifest problem", zap.Error(err))
		return 1
	}
	problems := joined.Unwrap()
	for _, p := range problems {
		logger.Error("manifest problem", zap.Error(p))
	}
	return len(problems)
}

// isTemplate reports whether path should be rendered through gomplate.
func isTemplate(path string) bool {
	return strings.HasSuffix(path, ".tpl")
}

// resolveManifest returns the manifest path to build. When the user did not set
// --file and the default .tpl is absent, it falls back to a plain nelmwave.yml.
func resolveManifest(file string, changed bool) (string, error) {
	if changed {
		_, err := os.Stat(file)
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("manifest %q not found", file)
		}
		if err != nil {
			return "", fmt.Errorf("manifest %q: %w", file, err)
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

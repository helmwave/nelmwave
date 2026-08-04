// Package plan builds, writes and reads the self-contained build artifact under
// .nelmwave/. Runtime commands (up/down/diff) read only the plan and never
// re-render templates, keeping CI deterministic.
package plan

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/helmwave/nelmwave/internal/config"
)

// Default locations of the build output.
const (
	// DefaultDir is the build output directory.
	DefaultDir = ".nelmwave"
	// PlanfileName is the plan file inside the build directory.
	PlanfileName = "planfile.yml"
	// ValuesDir holds merged per-release values (populated in a later milestone).
	ValuesDir = "values"
	// StoreDir holds resolved store files (populated in a later milestone).
	StoreDir = "store"
)

// Plan is the flat, fully-resolved deployment plan persisted to disk.
// Repositories and Releases are keyed by identity, mirroring the manifest.
// yaml.v3 marshals map keys in sorted order, so output is deterministic.
type Plan struct {
	Project      string                       `yaml:"project"`
	Repositories map[string]config.Repository `yaml:"repositories,omitempty"`
	Releases     map[string]Release           `yaml:"releases"`
}

// Release is a plan entry for a single release, keyed by its uniqname
// ("name[@namespace[@kubecontext]]") in Plan.Releases. It mirrors config.Release
// but adds resolved artifact paths filled by later build stages.
type Release struct {
	Labels  map[string]string     `yaml:"labels,omitempty"`
	Needs   config.Needs          `yaml:"needs,omitempty"`
	Chart   config.Chart          `yaml:"chart,omitempty"`
	Values  []config.FileRef      `yaml:"values,omitempty"`
	Store   []config.FileRef      `yaml:"store,omitempty"`
	Options config.ReleaseOptions `yaml:"options"`

	// ValuesFile is the plan-relative path to the merged values file. Empty
	// until the datasource milestone resolves and merges values.
	ValuesFile string `yaml:"valuesFile,omitempty"`
}

// FromConfig projects a validated Config into a Plan.
func FromConfig(cfg *config.Config) *Plan {
	p := &Plan{
		Project:      cfg.Project,
		Repositories: cfg.Repositories,
		Releases:     make(map[string]Release, len(cfg.Releases)),
	}
	for name, r := range cfg.Releases {
		p.Releases[name] = Release{
			Labels:  r.Labels,
			Needs:   r.Needs,
			Chart:   r.Chart,
			Values:  r.Values,
			Store:   r.Store,
			Options: r.Options,
		}
	}
	return p
}

// ReleaseNames returns the release names in deterministic (sorted) order.
func (p *Plan) ReleaseNames() []string {
	names := make([]string, 0, len(p.Releases))
	for name := range p.Releases {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Write serializes the plan to dir/planfile.yml, creating dir if needed.
func (p *Plan) Write(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create plan dir %q: %w", dir, err)
	}
	data, err := yaml.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal plan: %w", err)
	}
	path := filepath.Join(dir, PlanfileName)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write plan file %q: %w", path, err)
	}
	return nil
}

// Read loads a plan from dir/planfile.yml.
func Read(dir string) (*Plan, error) {
	path := filepath.Join(dir, PlanfileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read plan file %q: %w", path, err)
	}
	var p Plan
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse plan file %q: %w", path, err)
	}
	return &p, nil
}

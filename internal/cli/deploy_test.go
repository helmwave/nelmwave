package cli

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"go.uber.org/zap"

	"github.com/helmwave/nelmwave/internal/config"
	"github.com/helmwave/nelmwave/internal/plan"
	"github.com/helmwave/nelmwave/internal/release"
)

type fakeApplier struct {
	mu          sync.Mutex
	installed   []string
	uninstalled []string
	fail        map[string]error
}

func (f *fakeApplier) Install(_ context.Context, s release.Spec) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.installed = append(f.installed, s.Name)
	return f.fail[s.Name]
}

func (f *fakeApplier) Uninstall(_ context.Context, s release.Spec) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.uninstalled = append(f.uninstalled, s.Name)
	return f.fail[s.Name]
}

// a@ns (no needs) and b@ns (needs a@ns, optionally strict).
func testPlan(strict bool) *plan.Plan {
	return &plan.Plan{
		Releases: map[string]plan.Release{
			"a@ns": {
				Labels: map[string]string{"app": "a"},
				Chart:  config.Chart{Name: "r/a"},
			},
			"b@ns": {
				Labels: map[string]string{"app": "b"},
				Needs:  []plan.Need{{Uniqname: "a@ns", Strict: strict}},
				Chart:  config.Chart{Name: "r/b"},
			},
		},
	}
}

func opts(selector string, includeNeeds bool) deployOptions {
	return deployOptions{output: ".", selector: selector, concurrency: 1, includeNeeds: includeNeeds}
}

func TestDeploy_InstallsInDependencyOrder(t *testing.T) {
	f := &fakeApplier{}
	if err := deploy(context.Background(), zap.NewNop(), testPlan(false), opts("", false), f, opInstall); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	ai := slices.Index(f.installed, "a")
	bi := slices.Index(f.installed, "b")
	if ai < 0 || bi < 0 || ai > bi {
		t.Errorf("install order = %v, want a before b", f.installed)
	}
}

func TestDeploy_UninstallsInReverseOrder(t *testing.T) {
	f := &fakeApplier{}
	if err := deploy(context.Background(), zap.NewNop(), testPlan(false), opts("", false), f, opUninstall); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	ai := slices.Index(f.uninstalled, "a")
	bi := slices.Index(f.uninstalled, "b")
	if ai < 0 || bi < 0 || bi > ai {
		t.Errorf("uninstall order = %v, want b before a", f.uninstalled)
	}
}

func TestDeploy_SelectorFiltersReleases(t *testing.T) {
	f := &fakeApplier{}
	// Select only a; b (which needs a) is excluded.
	if err := deploy(context.Background(), zap.NewNop(), testPlan(false), opts("app=a", false), f, opInstall); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if len(f.installed) != 1 || f.installed[0] != "a" {
		t.Errorf("installed = %v, want [a]", f.installed)
	}
}

func TestDeploy_StrictNeedOutsideSelectionErrors(t *testing.T) {
	f := &fakeApplier{}
	// Select only b, which strictly needs a (filtered out).
	err := deploy(context.Background(), zap.NewNop(), testPlan(true), opts("app=b", false), f, opInstall)
	if err == nil {
		t.Fatal("expected strict-need error")
	}
	if len(f.installed) != 0 {
		t.Errorf("nothing should have been installed, got %v", f.installed)
	}
}

func TestDeploy_IncludeNeedsPullsInDependency(t *testing.T) {
	f := &fakeApplier{}
	// Select only b, but --include-needs pulls a back in.
	if err := deploy(context.Background(), zap.NewNop(), testPlan(true), opts("app=b", true), f, opInstall); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if len(f.installed) != 2 || slices.Index(f.installed, "a") > slices.Index(f.installed, "b") {
		t.Errorf("installed = %v, want a then b", f.installed)
	}
}

func TestDeploy_FailureAggregatesAndSkipsDependents(t *testing.T) {
	f := &fakeApplier{fail: map[string]error{"a": errors.New("boom")}}
	err := deploy(context.Background(), zap.NewNop(), testPlan(false), opts("", false), f, opInstall)
	if err == nil {
		t.Fatal("expected aggregated error when a fails")
	}
	// b depends on a, so b must be skipped (never installed).
	if slices.Contains(f.installed, "b") {
		t.Errorf("b should have been skipped, installed = %v", f.installed)
	}
}

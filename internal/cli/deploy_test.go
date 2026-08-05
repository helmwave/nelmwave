package cli

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/helmwave/nelmwave/internal/config"
	"github.com/helmwave/nelmwave/internal/plan"
	"github.com/helmwave/nelmwave/internal/release"
)

type fakeApplier struct {
	mu          sync.Mutex
	installed   []string
	uninstalled []string
	planned     []string
	sets        map[string][]string
	deleteNS    map[string]bool
	specs       map[string]release.Spec
	planOpts    release.PlanOptions
	fail        map[string]error
	changes     map[string]bool // release name -> has planned changes
}

func (f *fakeApplier) Install(_ context.Context, s release.Spec) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.installed = append(f.installed, s.Name)
	if f.sets == nil {
		f.sets = map[string][]string{}
	}
	f.sets[s.Name] = s.SetJSON
	if f.specs == nil {
		f.specs = map[string]release.Spec{}
	}
	f.specs[s.Name] = s
	return f.fail[s.Name]
}

func (f *fakeApplier) Uninstall(_ context.Context, s release.Spec) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.uninstalled = append(f.uninstalled, s.Name)
	if f.deleteNS == nil {
		f.deleteNS = map[string]bool{}
	}
	f.deleteNS[s.Name] = s.DeleteNamespace
	return f.fail[s.Name]
}

func (f *fakeApplier) Plan(_ context.Context, s release.Spec, o release.PlanOptions) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.planned = append(f.planned, s.Name)
	f.planOpts = o
	if err := f.fail[s.Name]; err != nil {
		return false, err
	}
	return o.ErrorIfChanges && f.changes[s.Name], nil
}

// a@ns (no needs) and b@ns (needs a@ns, required or optional).
func testPlan(optional bool) *plan.Plan {
	return &plan.Plan{
		Releases: map[string]plan.Release{
			"a@ns": {
				Labels: map[string]string{"app": "a"},
				Chart:  config.Chart{Name: "r/a"},
			},
			"b@ns": {
				Labels: map[string]string{"app": "b"},
				Needs:  []plan.Need{{Uniqname: "a@ns", Optional: optional}},
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

func TestDeploy_RequiredNeedOutsideSelectionErrors(t *testing.T) {
	f := &fakeApplier{}
	// Select only b, which requires a (filtered out). Required is the default,
	// so this is what an unannotated dependency does.
	err := deploy(context.Background(), zap.NewNop(), testPlan(false), opts("app=b", false), f, opInstall)
	if err == nil {
		t.Fatal("expected an unsatisfied-need error")
	}
	if len(f.installed) != 0 {
		t.Errorf("nothing should have been installed, got %v", f.installed)
	}
}

func TestDeploy_IncludeNeedsPullsInDependency(t *testing.T) {
	f := &fakeApplier{}
	// Select only b, but --include-needs pulls a back in.
	if err := deploy(context.Background(), zap.NewNop(), testPlan(false), opts("app=b", true), f, opInstall); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if len(f.installed) != 2 || slices.Index(f.installed, "a") > slices.Index(f.installed, "b") {
		t.Errorf("installed = %v, want a then b", f.installed)
	}
}

func TestDeploy_IncludeNeedsPullsInDependentsOnUninstall(t *testing.T) {
	f := &fakeApplier{}
	// Select only a, the dependency. On uninstall the flag travels the other
	// way: b depends on a, so b must go too — and go first.
	if err := deploy(context.Background(), zap.NewNop(), testPlan(false), opts("app=a", true), f, opUninstall); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if len(f.uninstalled) != 2 {
		t.Fatalf("uninstalled = %v, want both releases", f.uninstalled)
	}
	if slices.Index(f.uninstalled, "b") > slices.Index(f.uninstalled, "a") {
		t.Errorf("uninstalled = %v, want b (the dependent) before a", f.uninstalled)
	}
}

func TestDeploy_UninstallWithoutFlagLeavesDependentsAlone(t *testing.T) {
	f := &fakeApplier{}
	// The same selection without the flag removes exactly what was asked for.
	if err := deploy(context.Background(), zap.NewNop(), testPlan(false), opts("app=a", false), f, opUninstall); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if len(f.uninstalled) != 1 || f.uninstalled[0] != "a" {
		t.Errorf("uninstalled = %v, want [a]", f.uninstalled)
	}
}

func TestDiff_IncludeNeedsWidensTheSelection(t *testing.T) {
	f := &fakeApplier{}
	// Planning only b would cover one release; with the flag its dependency a
	// is planned too.
	if err := diffReleases(context.Background(), zap.NewNop(), testPlan(false), opts("app=b", true), false, f); err != nil {
		t.Fatalf("diff: %v", err)
	}
	if len(f.planned) != 2 {
		t.Errorf("planned = %v, want both b and its dependency a", f.planned)
	}
}

func TestDeploy_OptionalNeedOutsideSelectionIsDropped(t *testing.T) {
	f := &fakeApplier{}
	// Select only b, whose need on a is optional: the edge is dropped and the
	// run proceeds, where a required need would have failed.
	if err := deploy(context.Background(), zap.NewNop(), testPlan(true), opts("app=b", false), f, opInstall); err != nil {
		t.Fatalf("optional need outside the selection should not fail: %v", err)
	}
	if len(f.installed) != 1 || f.installed[0] != "b" {
		t.Errorf("installed = %v, want [b]", f.installed)
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

func TestDiff_PlansSelectedReleases(t *testing.T) {
	f := &fakeApplier{}
	err := diffReleases(context.Background(), zap.NewNop(), testPlan(false), opts("", false), false, f)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if len(f.planned) != 2 {
		t.Errorf("planned = %v, want both releases", f.planned)
	}
	if len(f.installed) != 0 || len(f.uninstalled) != 0 {
		t.Error("diff must not install or uninstall anything")
	}
}

func TestDiff_DetailedExitCodeOnChanges(t *testing.T) {
	f := &fakeApplier{changes: map[string]bool{"a": true}}
	err := diffReleases(context.Background(), zap.NewNop(), testPlan(false), opts("", false), true, f)
	var ee *exitError
	if !errors.As(err, &ee) || ee.code != 2 {
		t.Fatalf("want exitError code 2, got %v", err)
	}
}

func TestDiff_DetailedExitCodeNoChanges(t *testing.T) {
	f := &fakeApplier{} // no changes
	if err := diffReleases(context.Background(), zap.NewNop(), testPlan(false), opts("", false), true, f); err != nil {
		t.Fatalf("no changes should exit cleanly, got %v", err)
	}
}

func TestDeploy_PassesSetsToApplierAsTypedJSON(t *testing.T) {
	p := &plan.Plan{
		Releases: map[string]plan.Release{
			"a@ns": {
				Labels: map[string]string{"app": "a"},
				Chart:  config.Chart{Name: "r/a"},
				Sets:   map[string]any{"image.tag": "1.2.3", "replicaCount": 3},
			},
		},
	}
	f := &fakeApplier{}
	if err := deploy(context.Background(), zap.NewNop(), p, opts("", false), f, opInstall); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	// Sorted keys, JSON-encoded values: string stays quoted, int stays a number.
	got := f.sets["a"]
	if len(got) != 2 || got[0] != `image.tag="1.2.3"` || got[1] != "replicaCount=3" {
		t.Errorf("set JSON args = %v", got)
	}
}

func TestSetJSONArgs_NestedMapsAndTypes(t *testing.T) {
	args, err := setJSONArgs(map[string]any{
		"image":        map[string]any{"tag": 111},
		"foo":          map[string]any{"bar": map[string]any{"greet": "Hello"}},
		"replicaCount": 3,
		"image.tag":    "1.4.2",
	})
	if err != nil {
		t.Fatalf("setJSONArgs: %v", err)
	}
	// Sorted keys; nested maps become JSON objects, types preserved.
	want := []string{
		`foo={"bar":{"greet":"Hello"}}`,
		`image={"tag":111}`,
		`image.tag="1.4.2"`,
		`replicaCount=3`,
	}
	if len(args) != len(want) {
		t.Fatalf("args = %v", args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Errorf("arg[%d] = %q, want %q", i, args[i], want[i])
		}
	}
}

func TestDeploy_ResourcePoliciesReachTheApplier(t *testing.T) {
	p := &plan.Plan{
		Releases: map[string]plan.Release{
			"a@ns": {
				Chart:               config.Chart{Name: "r/a"},
				Namespace:           config.Namespace{Create: true},
				ForceAdoption:       true,
				RemoveManualChanges: false,
				InstallCRDs:         true,
				DeletePropagation:   "Background",
				HistoryLimit:        5,
			},
		},
	}
	f := &fakeApplier{}
	if err := deploy(context.Background(), zap.NewNop(), p, opts("", false), f, opInstall); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	got := f.specs["a"]
	if !got.ForceAdoption || got.RemoveManualChanges || !got.InstallCRDs ||
		got.DeletePropagation != "Background" || got.HistoryLimit != 5 {
		t.Errorf("policies did not reach the spec: %+v", got)
	}
}

func TestDiff_RenderingOptionsReachTheApplier(t *testing.T) {
	o := opts("", false)
	o.diff = release.DiffOptions{
		ShowVerbose:       true,
		ShowVerboseCRD:    true,
		ShowInsignificant: true,
		ShowSensitive:     true,
		ContextLines:      7,
	}
	f := &fakeApplier{}
	if err := diffReleases(context.Background(), zap.NewNop(), testPlan(false), o, false, f); err != nil {
		t.Fatalf("diff: %v", err)
	}
	if f.planOpts.Diff != o.diff {
		t.Errorf("diff options = %+v, want %+v", f.planOpts.Diff, o.diff)
	}
}

// up --dry-run has no flags of its own, so it must not silently plan with a
// narrower view than nelm's CLI would show.
func TestUp_DryRunUsesNelmDefaultDiffView(t *testing.T) {
	f := &fakeApplier{}
	o := opts("", false)
	o.diff = release.DefaultDiffOptions()
	if err := diffReleases(context.Background(), zap.NewNop(), testPlan(false), o, false, f); err != nil {
		t.Fatalf("diff: %v", err)
	}
	if !f.planOpts.Diff.ShowVerbose {
		t.Error("verbose diffs must be on by default, as in nelm's CLI")
	}
}

func TestDeploy_NamespaceDeleteReachesTheApplierAndWarns(t *testing.T) {
	p := &plan.Plan{
		Releases: map[string]plan.Release{
			"a@ns": {
				Labels:    map[string]string{"app": "a"},
				Chart:     config.Chart{Name: "r/a"},
				Namespace: config.Namespace{Create: true, Delete: true},
			},
			"b@ns": {
				Labels:    map[string]string{"app": "b"},
				Chart:     config.Chart{Name: "r/b"},
				Namespace: config.Namespace{Create: true},
			},
		},
	}

	core, logs := observer.New(zap.InfoLevel)
	f := &fakeApplier{}
	if err := deploy(context.Background(), zap.New(core), p, opts("", false), f, opUninstall); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if !f.deleteNS["a"] {
		t.Error("namespace.delete did not reach the applier")
	}
	if f.deleteNS["b"] {
		t.Error("release without namespace.delete must not delete its namespace")
	}

	// Deleting a namespace takes unrelated tenants with it, so it must be
	// announced at warn level, once, and only for the release that asked.
	warns := logs.FilterLevelExact(zap.WarnLevel).All()
	if len(warns) != 1 {
		t.Fatalf("want exactly one warning, got %d: %v", len(warns), warns)
	}
	if got := warns[0].ContextMap()["release"]; got != "a@ns" {
		t.Errorf("warning is about %v, want a@ns", got)
	}
}

func TestDeploy_NamespaceDeleteIsIgnoredOnInstall(t *testing.T) {
	p := &plan.Plan{
		Releases: map[string]plan.Release{
			"a@ns": {
				Chart:     config.Chart{Name: "r/a"},
				Namespace: config.Namespace{Create: true, Delete: true},
			},
		},
	}

	core, logs := observer.New(zap.InfoLevel)
	f := &fakeApplier{}
	if err := deploy(context.Background(), zap.New(core), p, opts("", false), f, opInstall); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if n := logs.FilterLevelExact(zap.WarnLevel).Len(); n != 0 {
		t.Errorf("up must not warn about namespace deletion, got %d warnings", n)
	}
}

func TestLogSelection_FlagsWhatTheSelectorDidNotName(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	selected := []string{"a@ns"}
	active := map[string]struct{}{"a@ns": {}, "b@ns": {}}

	logSelection(zap.New(core), "uninstall", selected, active)

	entries := logs.All()
	if len(entries) != 2 {
		t.Fatalf("want a selection line plus a pulled-in warning, got %d entries", len(entries))
	}
	if entries[0].Message != "uninstall selection" {
		t.Errorf("first entry = %q", entries[0].Message)
	}
	// The widening must be a warning, not buried at info level.
	if entries[1].Level != zap.WarnLevel {
		t.Errorf("pulled-in entry level = %v, want warn", entries[1].Level)
	}
	// zap.Strings lands in ContextMap as []interface{}, so compare rendered.
	if got := fmt.Sprint(entries[1].ContextMap()["releases"]); got != "[b@ns]" {
		t.Errorf("pulled-in releases = %s, want [b@ns]", got)
	}
}

func TestLogSelection_QuietWhenNothingWasAdded(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	active := map[string]struct{}{"a@ns": {}}

	logSelection(zap.New(core), "install", []string{"a@ns"}, active)

	if n := logs.Len(); n != 1 {
		t.Errorf("want only the selection line when nothing was pulled in, got %d entries", n)
	}
}

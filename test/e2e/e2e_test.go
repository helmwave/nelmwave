//go:build e2e

// Package e2e drives nelmwave against a real Kubernetes API server.
//
// The cluster is a k3s container owned by docker-compose (see
// docker-compose.yml); the chart is a local one from testdata, so the suite
// downloads nothing and every assertion is about nelmwave's own behaviour.
//
//	docker-compose -f test/e2e/docker-compose.yml up -d --wait
//	KUBECONFIG=test/e2e/.kube/kubeconfig.yaml go test -tags e2e ./test/e2e/...
//	docker-compose -f test/e2e/docker-compose.yml down -v
//
// Or, equivalently: make e2e.
package e2e

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/helmwave/nelmwave/internal/cli"
)

const (
	namespace   = "nelmwave-e2e"
	waitTimeout = 90 * time.Second
)

// TestLifecycle walks one manifest through the whole product: build, up, a
// no-change diff, a drifting diff, the upgrade that resolves it, a selective
// down, and a full down. The steps share cluster state, so they run in order
// as subtests of a single test rather than as independent ones.
func TestLifecycle(t *testing.T) {
	kubeconfig := requireKubeconfig(t)
	clients := connect(t, kubeconfig)
	ctx := context.Background()

	// A leftover namespace from an interrupted run would make the first
	// assertions pass for the wrong reason.
	cleanNamespace(ctx, t, clients)
	t.Cleanup(func() { cleanNamespace(context.Background(), t, clients) })

	manifest := abs(t, "testdata/nelmwave.yml.tpl")
	chart := abs(t, "testdata/chart")
	out := filepath.Join(t.TempDir(), ".nelmwave")

	env := map[string]string{
		"E2E_NS":      namespace,
		"E2E_CHART":   chart,
		"E2E_MESSAGE": "hello",
	}

	t.Run("build", func(t *testing.T) {
		setEnv(t, env)
		run(t, "build", "--file", manifest, "--output", out)

		// The plan is self-contained: artifacts land next to it, and up/down/diff
		// read only what is here.
		for _, rel := range []string{"base@" + namespace, "app@" + namespace} {
			mustExist(t, filepath.Join(out, "values", rel))
		}
		mustExist(t, filepath.Join(out, "planfile.yml"))
	})

	t.Run("up", func(t *testing.T) {
		setEnv(t, env)
		run(t, "up", "--output", out, "--kube-config", kubeconfig)

		// `sets` outrank the values file: base.yml says from-values-file.
		assertConfigMap(ctx, t, clients, "base", "hello")
		// A release without `sets` keeps what its values file declared.
		assertConfigMap(ctx, t, clients, "app", "app-values")

		for _, name := range []string{"base", "app"} {
			waitDeploymentReady(ctx, t, clients, name)
		}
	})

	t.Run("diff is clean right after up", func(t *testing.T) {
		setEnv(t, env)
		err := execute(t, "diff", "--output", out, "--kube-config", kubeconfig, "--detailed-exitcode")
		if code := cli.ExitCode(err); code != 0 {
			t.Fatalf("diff right after up: exit %d, err %v; want a clean 0", code, err)
		}
	})

	t.Run("diff reports drift with exit code 2", func(t *testing.T) {
		env["E2E_MESSAGE"] = "changed"
		setEnv(t, env)
		// Rebuild so the plan carries the new value; runtime commands never
		// re-render the manifest themselves.
		run(t, "build", "--file", manifest, "--output", out)

		err := execute(t, "diff", "--output", out, "--kube-config", kubeconfig, "--detailed-exitcode")
		if code := cli.ExitCode(err); code != 2 {
			t.Fatalf("diff with pending changes: exit %d, err %v; want 2", code, err)
		}
		// Planning must not touch the cluster.
		assertConfigMap(ctx, t, clients, "base", "hello")
	})

	t.Run("up applies the change", func(t *testing.T) {
		setEnv(t, env)
		run(t, "up", "--output", out, "--kube-config", kubeconfig)
		assertConfigMap(ctx, t, clients, "base", "changed")
		waitDeploymentReady(ctx, t, clients, "base")
	})

	t.Run("down honours the selector", func(t *testing.T) {
		setEnv(t, env)
		run(t, "down", "--output", out, "--kube-config", kubeconfig, "-l", "app=app")

		waitGone(ctx, t, clients, "app")
		// The unselected release is left alone.
		assertConfigMap(ctx, t, clients, "base", "changed")
	})

	t.Run("down removes the rest", func(t *testing.T) {
		setEnv(t, env)
		run(t, "down", "--output", out, "--kube-config", kubeconfig)
		waitGone(ctx, t, clients, "base")
	})
}

// TestRequiredNeedOutsideSelection covers the needs policy against a live
// cluster: selecting only the dependent release must fail before anything is
// applied, because its dependency is required and filtered out.
func TestRequiredNeedOutsideSelection(t *testing.T) {
	kubeconfig := requireKubeconfig(t)
	clients := connect(t, kubeconfig)
	ctx := context.Background()

	cleanNamespace(ctx, t, clients)
	t.Cleanup(func() { cleanNamespace(context.Background(), t, clients) })

	manifest := abs(t, "testdata/required-needs.yml.tpl")
	out := filepath.Join(t.TempDir(), ".nelmwave")
	setEnv(t, map[string]string{
		"E2E_NS":    namespace,
		"E2E_CHART": abs(t, "testdata/chart"),
	})

	run(t, "build", "--file", manifest, "--output", out)

	err := execute(t, "up", "--output", out, "--kube-config", kubeconfig, "-l", "app=dependent")
	if err == nil {
		t.Fatal("selecting only the dependent release should fail: its required need is filtered out")
	}
	// Nothing may reach the cluster once the policy rejects the selection.
	if _, getErr := clients.CoreV1().ConfigMaps(namespace).Get(ctx, "dependent", metav1.GetOptions{}); getErr == nil {
		t.Error("no release should have been applied after an unsatisfied-need failure")
	}

	// With --include-needs the dependency is pulled back in and both go up.
	run(t, "up", "--output", out, "--kube-config", kubeconfig, "-l", "app=dependent", "--include-needs")
	assertConfigMap(ctx, t, clients, "dependency", "dependency")
	assertConfigMap(ctx, t, clients, "dependent", "dependent")
}

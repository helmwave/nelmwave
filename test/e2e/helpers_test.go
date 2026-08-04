//go:build e2e

package e2e

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/helmwave/nelmwave/internal/cli"
)

// requireKubeconfig locates the cluster the suite should talk to: $KUBECONFIG,
// or the file docker-compose drops next to this package. Running with -tags e2e
// is an explicit request for a cluster, so a missing one is a failure with
// instructions rather than a silent skip.
func requireKubeconfig(t *testing.T) string {
	t.Helper()
	candidates := []string{os.Getenv("KUBECONFIG"), filepath.Join(".kube", "kubeconfig.yaml")}
	for _, path := range candidates {
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err == nil {
			return abs(t, path)
		}
	}
	t.Fatalf("no kubeconfig found (looked at $KUBECONFIG and .kube/kubeconfig.yaml).\n" +
		"Start the fixture first:\n" +
		"  docker-compose -f test/e2e/docker-compose.yml up -d --wait")
	return ""
}

// connect builds a clientset and proves the API server answers, so a broken
// fixture fails here instead of midway through a lifecycle assertion.
func connect(t *testing.T, kubeconfig string) *kubernetes.Clientset {
	t.Helper()
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		t.Fatalf("load kubeconfig %q: %v", kubeconfig, err)
	}
	cfg.Timeout = 30 * time.Second
	clients, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("build client: %v", err)
	}
	if _, err := clients.Discovery().ServerVersion(); err != nil {
		t.Fatalf("cluster is not reachable via %q: %v", kubeconfig, err)
	}
	return clients
}

// run executes a nelmwave command and fails the test if it errors.
func run(t *testing.T, args ...string) {
	t.Helper()
	if err := execute(t, args...); err != nil {
		t.Fatalf("nelmwave %v: %v", args, err)
	}
}

// execute runs a nelmwave command through the real command tree and returns its
// error, so callers can inspect the exit code (see cli.ExitCode).
func execute(t *testing.T, args ...string) error {
	t.Helper()
	root := cli.NewRootCommand()
	root.SetArgs(args)
	return root.Execute()
}

// setEnv exports the manifest's template inputs for the duration of the test.
func setEnv(t *testing.T, env map[string]string) {
	t.Helper()
	for k, v := range env {
		t.Setenv(k, v)
	}
}

func abs(t *testing.T, path string) string {
	t.Helper()
	p, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("resolve %q: %v", path, err)
	}
	return p
}

func mustExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected build artifact %q: %v", path, err)
	}
}

// assertConfigMap checks the message the chart rendered for a release, which is
// how the suite observes which values actually won.
func assertConfigMap(ctx context.Context, t *testing.T, clients *kubernetes.Clientset, name, want string) {
	t.Helper()
	cm, err := clients.CoreV1().ConfigMaps(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get configmap %q: %v", name, err)
	}
	if got := cm.Data["message"]; got != want {
		t.Errorf("configmap %q message = %q, want %q", name, got, want)
	}
}

// waitDeploymentReady polls until the Deployment reports its replicas ready.
// nelm tracks readiness itself, so this normally passes on the first look; the
// poll only covers the gap when tracking is disabled or the node is slow.
func waitDeploymentReady(ctx context.Context, t *testing.T, clients *kubernetes.Clientset, name string) {
	t.Helper()
	var lastErr error
	deadline := time.Now().Add(waitTimeout)
	for time.Now().Before(deadline) {
		d, err := clients.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		switch {
		case err != nil:
			lastErr = err
		case d.Status.ReadyReplicas >= *d.Spec.Replicas:
			return
		default:
			lastErr = errors.New("not ready yet")
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("deployment %q never became ready within %s: %v", name, waitTimeout, lastErr)
}

// waitGone polls until the release's ConfigMap is gone, i.e. the uninstall
// actually removed its resources rather than just its release record.
func waitGone(ctx context.Context, t *testing.T, clients *kubernetes.Clientset, name string) {
	t.Helper()
	deadline := time.Now().Add(waitTimeout)
	for time.Now().Before(deadline) {
		_, err := clients.CoreV1().ConfigMaps(namespace).Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("configmap %q still exists %s after uninstall", name, waitTimeout)
}

// cleanNamespace deletes the test namespace and waits for it to disappear, so
// each test starts from nothing regardless of how the previous run ended.
func cleanNamespace(ctx context.Context, t *testing.T, clients *kubernetes.Clientset) {
	t.Helper()
	err := clients.CoreV1().Namespaces().Delete(ctx, namespace, metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return
	}
	if err != nil {
		t.Fatalf("delete namespace %q: %v", namespace, err)
	}
	deadline := time.Now().Add(waitTimeout)
	for time.Now().Before(deadline) {
		if _, err := clients.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{}); apierrors.IsNotFound(err) {
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("namespace %q was still terminating after %s", namespace, waitTimeout)
}

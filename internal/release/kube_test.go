package release

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/werf/nelm/pkg/common"
)

// The CI case: no kubeconfig anywhere, credentials arrive as a token and a CA.
// nelmwave patches namespaces with its own client, so this path has to work
// without a file — otherwise namespace labels break exactly where nelm succeeds.
func TestKubeConnection_RESTConfigWithoutKubeconfig(t *testing.T) {
	// Point HOME and KUBECONFIG at nothing, so a stray ~/.kube/config on the
	// machine running the tests cannot make this pass by accident.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "absent"))

	k := KubeConnection{
		APIServer:      "https://k8s.example.com:6443",
		TokenData:      "s3cr3t",
		CAData:         "",
		SkipTLSVerify:  true,
		RequestTimeout: 15 * time.Second,
		QPSLimit:       7,
		BurstLimit:     9,
	}

	cfg, err := k.RESTConfig("")
	if err != nil {
		t.Fatalf("RESTConfig: %v", err)
	}
	if cfg.Host != "https://k8s.example.com:6443" {
		t.Errorf("host = %q", cfg.Host)
	}
	if cfg.BearerToken != "s3cr3t" {
		t.Errorf("token = %q", cfg.BearerToken)
	}
	if !cfg.Insecure {
		t.Error("skipTLSVerify did not reach the rest config")
	}
	if cfg.Timeout != 15*time.Second {
		t.Errorf("timeout = %v", cfg.Timeout)
	}
	if cfg.QPS != 7 || cfg.Burst != 9 {
		t.Errorf("qps/burst = %v/%v", cfg.QPS, cfg.Burst)
	}
}

// Zero limits must land on nelm's defaults, not client-go's much lower ones, so
// nelmwave's own calls are throttled like nelm's.
func TestKubeConnection_DefaultLimits(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "absent"))

	cfg, err := KubeConnection{APIServer: "https://x:6443", SkipTLSVerify: true}.RESTConfig("")
	if err != nil {
		t.Fatalf("RESTConfig: %v", err)
	}
	if cfg.QPS != float32(common.DefaultQPSLimit) || cfg.Burst != common.DefaultBurstLimit {
		t.Errorf("qps/burst = %v/%v, want nelm's %v/%v",
			cfg.QPS, cfg.Burst, common.DefaultQPSLimit, common.DefaultBurstLimit)
	}
}

func TestKubeConnection_ConfigBase64AndContext(t *testing.T) {
	const kubeconfig = `
apiVersion: v1
kind: Config
clusters:
  - name: stg
    cluster: { server: https://stg.example.com:6443, insecure-skip-tls-verify: true }
  - name: prod
    cluster: { server: https://prod.example.com:6443, insecure-skip-tls-verify: true }
contexts:
  - name: staging
    context: { cluster: stg, user: dev }
  - name: production
    context: { cluster: prod, user: dev }
current-context: staging
users:
  - name: dev
    user: { token: t }
`
	k := KubeConnection{ConfigBase64: base64.StdEncoding.EncodeToString([]byte(kubeconfig))}

	cfg, err := k.RESTConfig("")
	if err != nil {
		t.Fatalf("RESTConfig: %v", err)
	}
	if cfg.Host != "https://stg.example.com:6443" {
		t.Errorf("default context host = %q, want the staging cluster", cfg.Host)
	}

	// The release's own context wins over the file's current-context.
	cfg, err = k.RESTConfig("production")
	if err != nil {
		t.Fatalf("RESTConfig(production): %v", err)
	}
	if cfg.Host != "https://prod.example.com:6443" {
		t.Errorf("host = %q, want the production cluster", cfg.Host)
	}
}

// Several kubeconfig files merge, the earlier file winning any conflict — the
// KUBECONFIG=a:b behaviour, spelled as a repeatable flag.
//
// The "first wins" part is why go.mod pins imdario/mergo back to v0.3.6: newer
// mergo reverses the merge and clientcmd would take the *last* file's value,
// unlike kubectl. Both cases below are regression guards for that pin.
func TestKubeConnection_ConfigPathsPrecedence(t *testing.T) {
	dir := t.TempDir()
	write := func(name, server, ctx, cluster string) string {
		p := filepath.Join(dir, name)
		body := "apiVersion: v1\nkind: Config\nclusters:\n  - name: " + cluster +
			"\n    cluster: { server: " + server + ", insecure-skip-tls-verify: true }\ncontexts:\n  - name: " + ctx +
			"\n    context: { cluster: " + cluster + ", user: u }\ncurrent-context: " + ctx +
			"\nusers:\n  - name: u\n    user: { token: t }\n"
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	// Distinct cluster names: this checks which current-context wins.
	first := write("first.yml", "https://first:6443", "one", "c-one")
	second := write("second.yml", "https://second:6443", "two", "c-two")
	paths := []string{first, second}

	cfg, err := KubeConnection{ConfigPaths: paths}.RESTConfig("")
	if err != nil {
		t.Fatalf("RESTConfig: %v", err)
	}
	if cfg.Host != "https://first:6443" {
		t.Errorf("host = %q, want the first file's current-context to win", cfg.Host)
	}

	// Both files' contexts are in the merged view, so the later one is reachable
	// by name — that is what a release's uniqname does.
	cfg, err = KubeConnection{ConfigPaths: paths}.RESTConfig("two")
	if err != nil {
		t.Fatalf("RESTConfig(two): %v", err)
	}
	if cfg.Host != "https://second:6443" {
		t.Errorf("host = %q, want the second file's context", cfg.Host)
	}

	// Same cluster name in both files: the earlier file's definition wins, as
	// kubectl does.
	dup := []string{
		write("dup-first.yml", "https://dup-first:6443", "dup-one", "shared"),
		write("dup-second.yml", "https://dup-second:6443", "dup-two", "shared"),
	}
	cfg, err = KubeConnection{ConfigPaths: dup}.RESTConfig("dup-two")
	if err != nil {
		t.Fatalf("RESTConfig(dup-two): %v", err)
	}
	if cfg.Host != "https://dup-first:6443" {
		t.Errorf("host = %q, want the first definition of the shared cluster", cfg.Host)
	}
}

// With no --kube-config, $KUBECONFIG decides — as it does for kubectl. Falling
// back to ~/.kube/config while the environment points elsewhere would deploy to
// the wrong cluster, silently and successfully.
func TestKubeConnection_KubeconfigFromEnv(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(filepath.Join(home, ".kube"), 0o700); err != nil {
		t.Fatal(err)
	}
	kubeconfig := func(server, ctx string) string {
		return "apiVersion: v1\nkind: Config\nclusters:\n  - name: c\n    cluster: { server: " + server +
			", insecure-skip-tls-verify: true }\ncontexts:\n  - name: " + ctx +
			"\n    context: { cluster: c, user: u }\ncurrent-context: " + ctx +
			"\nusers:\n  - name: u\n    user: { token: t }\n"
	}
	// The home kubeconfig must lose: it is the cluster a mistake would reach.
	if err := os.WriteFile(filepath.Join(home, ".kube", "config"),
		[]byte(kubeconfig("https://home:6443", "home")), 0o600); err != nil {
		t.Fatal(err)
	}
	env := filepath.Join(dir, "env.yml")
	if err := os.WriteFile(env, []byte(kubeconfig("https://env:6443", "env")), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", home)
	t.Setenv("KUBECONFIG", env)

	cfg, err := KubeConnection{}.RESTConfig("")
	if err != nil {
		t.Fatalf("RESTConfig: %v", err)
	}
	if cfg.Host != "https://env:6443" {
		t.Errorf("host = %q, want the cluster $KUBECONFIG points at", cfg.Host)
	}

	// nelm has to read the very same files, so it is handed the resolved list
	// rather than the empty one.
	var o common.KubeConnectionOptions
	applyKube(&o, Spec{Kube: KubeConnection{}})
	if len(o.KubeConfigPaths) != 1 || o.KubeConfigPaths[0] != env {
		t.Errorf("nelm config paths = %v, want [%s]", o.KubeConfigPaths, env)
	}

	// An explicit flag still wins over the environment, and ":"-separated entries
	// split like kubectl splits them.
	paths := KubeConnection{ConfigPaths: []string{"/a" + string(os.PathListSeparator) + "/b", "/c"}}.
		ConfigPathPrecedence()
	if len(paths) != 3 || paths[0] != "/a" || paths[1] != "/b" || paths[2] != "/c" {
		t.Errorf("precedence = %v, want [/a /b /c]", paths)
	}
}

func TestApplyKube(t *testing.T) {
	s := Spec{
		KubeContext: "staging",
		Kube: KubeConnection{
			ConfigPaths:       []string{"/a", "/b"},
			ContextCluster:    "cl",
			ContextUser:       "us",
			APIServer:         "https://x:6443",
			TokenPath:         "/var/run/token",
			CAPath:            "/ca.pem",
			ImpersonateUser:   "deployer",
			ImpersonateGroups: []string{"ops"},
			RequestTimeout:    time.Minute,
		},
	}
	var o common.KubeConnectionOptions
	applyKube(&o, s)

	if len(o.KubeConfigPaths) != 2 || o.KubeConfigPaths[0] != "/a" {
		t.Errorf("config paths = %v", o.KubeConfigPaths)
	}
	if o.KubeContextCurrent != "staging" {
		t.Errorf("current context = %q, want the release's own", o.KubeContextCurrent)
	}
	if o.KubeContextCluster != "cl" || o.KubeContextUser != "us" {
		t.Errorf("context overrides lost: %+v", o)
	}
	if o.KubeAPIServerAddress != "https://x:6443" || o.KubeBearerTokenPath != "/var/run/token" ||
		o.KubeTLSCAPath != "/ca.pem" {
		t.Errorf("direct connection lost: %+v", o)
	}
	if o.KubeImpersonateUser != "deployer" || len(o.KubeImpersonateGroups) != 1 {
		t.Errorf("impersonation lost: %+v", o)
	}
	if o.KubeRequestTimeout != time.Minute {
		t.Errorf("request timeout = %v", o.KubeRequestTimeout)
	}
}

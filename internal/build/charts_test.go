package build

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"go.uber.org/zap"

	"github.com/helmwave/nelmwave/internal/config"
	"github.com/helmwave/nelmwave/internal/plan"
)

// chartRepo serves a one-chart helm repository over HTTP: an index.yaml and the
// archive it points at. It counts archive fetches, so a test can tell a shared
// download from two of them.
type chartRepo struct {
	*httptest.Server
	archiveHits atomic.Int32
}

func newChartRepo(t *testing.T, name, version string) *chartRepo {
	t.Helper()
	archive := chartArchive(t, name, version)
	digest := sha256.Sum256(archive)

	repo := &chartRepo{}
	mux := http.NewServeMux()
	file := fmt.Sprintf("/%s-%s.tgz", name, version)
	mux.HandleFunc(file, func(w http.ResponseWriter, _ *http.Request) {
		repo.archiveHits.Add(1)
		_, _ = w.Write(archive)
	})
	repo.Server = httptest.NewServer(mux)
	t.Cleanup(repo.Close)

	index := fmt.Sprintf(`apiVersion: v1
generated: "2020-01-01T00:00:00Z"
entries:
  %s:
    - apiVersion: v2
      name: %s
      version: %s
      digest: %s
      urls:
        - %s%s
`, name, name, version, hex.EncodeToString(digest[:]), repo.URL, file)
	mux.HandleFunc("/index.yaml", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(index))
	})
	return repo
}

// chartArchive packages the smallest thing helm still calls a chart.
func chartArchive(t *testing.T, name, version string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := fmt.Sprintf("apiVersion: v2\nname: %s\nversion: %s\n", name, version)
	hdr := &tar.Header{
		Name: name + "/Chart.yaml",
		Mode: 0o644,
		Size: int64(len(body)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// isolateHelmHome keeps the download away from the developer's own helm
// directories, which the downloader otherwise reads and writes.
func isolateHelmHome(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HELM_REPOSITORY_CACHE", filepath.Join(dir, "cache"))
	t.Setenv("HELM_REPOSITORY_CONFIG", filepath.Join(dir, "repositories.yaml"))
}

func TestCharts_DownloadsOncePerChartAndRewritesThePlan(t *testing.T) {
	isolateHelmHome(t)
	srv := newChartRepo(t, "demo", "1.2.3")
	out := t.TempDir()

	// Two releases of the same chart, so one download has to serve both.
	p := &plan.Plan{
		Repositories: map[string]config.Repository{"acme": {URL: srv.URL}},
		Releases: map[string]plan.Release{
			"a@ns": {Chart: config.Chart{Name: "acme/demo", Version: "1.2.3"}},
			"b@ns": {Chart: config.Chart{Name: "acme/demo", Version: "1.2.3"}},
		},
	}

	if err := Charts(p, t.TempDir(), out, zap.NewNop()); err != nil {
		t.Fatalf("Charts: %v", err)
	}

	want := filepath.ToSlash(filepath.Join(plan.ChartsDir, "acme_demo", "demo-1.2.3.tgz"))
	for _, key := range []string{"a@ns", "b@ns"} {
		if got := p.Releases[key].ChartFile; got != want {
			t.Errorf("%s: chartFile = %q, want %q", key, got, want)
		}
	}
	if got := srv.archiveHits.Load(); got != 1 {
		t.Errorf("archive fetched %d times, want 1 — releases sharing a chart share the download", got)
	}

	// The recorded path is plan-relative, and there really is an archive there.
	if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(want))); err != nil {
		t.Errorf("chart archive not written: %v", err)
	}
}

func TestCharts_RebuildsTheDirectoryFromScratch(t *testing.T) {
	isolateHelmHome(t)
	srv := newChartRepo(t, "demo", "1.2.3")
	out := t.TempDir()

	// A leftover from an earlier build, for a chart no longer in the manifest.
	stale := filepath.Join(out, plan.ChartsDir, "gone", "gone-0.1.0.tgz")
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := &plan.Plan{
		Repositories: map[string]config.Repository{"acme": {URL: srv.URL}},
		Releases:     map[string]plan.Release{"a@ns": {Chart: config.Chart{Name: "acme/demo", Version: "1.2.3"}}},
	}
	if err := Charts(p, t.TempDir(), out, zap.NewNop()); err != nil {
		t.Fatalf("Charts: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale chart survived the rebuild (stat err = %v)", err)
	}
}

// A local chart is copied in rather than left behind, so the build directory is
// the whole story regardless of where a chart came from.
func TestCharts_CopiesLocalChartsIn(t *testing.T) {
	isolateHelmHome(t)
	base := t.TempDir()
	out := t.TempDir()

	// An unpacked chart, referenced relative to the manifest...
	unpacked := filepath.Join(base, "charts", "mine")
	writeChartDir(t, unpacked, "mine", "0.1.0")
	// ...and a packaged one, referenced by absolute path.
	packaged := filepath.Join(base, "vendor", "other-2.0.0.tgz")
	if err := os.MkdirAll(filepath.Dir(packaged), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(packaged, chartArchive(t, "other", "2.0.0"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := &plan.Plan{
		Releases: map[string]plan.Release{
			"a@ns": {Chart: config.Chart{Name: "./charts/mine"}},
			"b@ns": {Chart: config.Chart{Name: "./charts/mine"}},
			"c@ns": {Chart: config.Chart{Name: packaged}},
		},
	}
	if err := Charts(p, base, out, zap.NewNop()); err != nil {
		t.Fatalf("Charts: %v", err)
	}

	// The unpacked chart becomes the directory itself; the packaged one lands
	// inside one, next to where a downloaded archive would.
	wantDir := filepath.ToSlash(filepath.Join(plan.ChartsDir, "mine"))
	wantFile := filepath.ToSlash(filepath.Join(plan.ChartsDir, "other-2.0.0", "other-2.0.0.tgz"))
	for key, want := range map[string]string{"a@ns": wantDir, "b@ns": wantDir, "c@ns": wantFile} {
		if got := p.Releases[key].ChartFile; got != want {
			t.Errorf("%s: chartFile = %q, want %q", key, got, want)
		}
	}

	// The whole tree travels, not just Chart.yaml.
	for _, rel := range []string{"Chart.yaml", "values.yaml", "templates/cm.yaml"} {
		if _, err := os.Stat(filepath.Join(out, plan.ChartsDir, "mine", filepath.FromSlash(rel))); err != nil {
			t.Errorf("%s not copied: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(wantFile))); err != nil {
		t.Errorf("packaged chart not copied: %v", err)
	}
}

func TestCharts_ReportsALocalChartThatIsNotThere(t *testing.T) {
	isolateHelmHome(t)
	base := t.TempDir()

	// A directory that exists but holds no chart is the confusing case: without
	// this check it would fail much later, with a cluster already involved.
	empty := filepath.Join(base, "charts", "empty")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}

	for name, ref := range map[string]string{"missing": "./charts/nope", "no Chart.yaml": "./charts/empty"} {
		p := &plan.Plan{Releases: map[string]plan.Release{"a@ns": {Chart: config.Chart{Name: ref}}}}
		err := Charts(p, base, t.TempDir(), zap.NewNop())
		if err == nil {
			t.Errorf("%s: expected an error for %q", name, ref)
			continue
		}
		if !strings.Contains(err.Error(), `release "a@ns"`) {
			t.Errorf("%s: the error should name the release, got %q", name, err)
		}
	}
}

// writeChartDir lays out the smallest unpacked chart, plus a template, so a
// copy can be checked for more than its Chart.yaml.
func writeChartDir(t *testing.T, dir, name, version string) {
	t.Helper()
	files := map[string]string{
		"Chart.yaml":        fmt.Sprintf("apiVersion: v2\nname: %s\nversion: %s\n", name, version),
		"values.yaml":       "message: hello\n",
		"templates/cm.yaml": "kind: ConfigMap\n",
	}
	for rel, body := range files {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCharts_ReportsAnUnreachableRepository(t *testing.T) {
	isolateHelmHome(t)
	p := &plan.Plan{
		Repositories: map[string]config.Repository{"acme": {URL: "http://127.0.0.1:1/charts"}},
		Releases:     map[string]plan.Release{"a@ns": {Chart: config.Chart{Name: "acme/demo", Version: "1.2.3"}}},
	}
	err := Charts(p, t.TempDir(), t.TempDir(), zap.NewNop())
	if err == nil {
		t.Fatal("expected a download failure")
	}
	if got := err.Error(); !strings.Contains(got, `release "a@ns"`) {
		t.Errorf("the error should name the release, got %q", got)
	}
}

func TestChartDir_SeparatesReferencesThatSanitizeAlike(t *testing.T) {
	dirs := map[string]string{}
	first := chartDir(dirs, "oci://ghcr.io/acme/demo", "|oci://ghcr.io/acme/demo")
	if first != "ghcr.io_acme_demo" {
		t.Errorf("dir = %q, want the scheme dropped and separators flattened", first)
	}
	// Same directory for another version of the same chart...
	if again := chartDir(dirs, "oci://ghcr.io/acme/demo", "|oci://ghcr.io/acme/demo"); again != first {
		t.Errorf("second version of the same chart got %q, want %q", again, first)
	}
	// ...but not for a different chart that happens to sanitize the same way.
	if other := chartDir(dirs, "ghcr.io/acme/demo", "https://repo|demo"); other == first {
		t.Errorf("distinct charts share the directory %q", other)
	}
}

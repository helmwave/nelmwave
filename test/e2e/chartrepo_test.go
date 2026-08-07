//go:build e2e

package e2e

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// serveChartRepo packages the local testdata chart and serves it as a helm
// repository, returning the repository URL and a func that stops the server.
//
// The suite still downloads nothing from the internet: this is the same chart
// TestLifecycle installs by path, just published. Stopping the server is the
// point — it is how a test proves that an apply reached for no repository.
func serveChartRepo(t *testing.T, chartDir, name, version string) (url string, stop func()) {
	t.Helper()
	archive := packageChart(t, chartDir, name, version)
	digest := sha256.Sum256(archive)

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	file := fmt.Sprintf("/%s-%s.tgz", name, version)

	mux.HandleFunc(file, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	})
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
`, name, name, version, hex.EncodeToString(digest[:]), srv.URL, file)
	mux.HandleFunc("/index.yaml", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(index))
	})

	var stopped bool
	return srv.URL, func() {
		if !stopped {
			stopped = true
			srv.Close()
		}
	}
}

// packageChart tars and gzips a chart directory the way `helm package` does:
// every file under a single top-level directory named after the chart.
func packageChart(t *testing.T, dir, name, version string) []byte {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "chart-*.tgz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		hdr := &tar.Header{
			Name: filepath.ToSlash(filepath.Join(name, rel)),
			Mode: 0o644,
			Size: int64(len(data)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		_, err = tw.Write(data)
		return err
	})
	if err != nil {
		t.Fatalf("package chart %q: %v", dir, err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

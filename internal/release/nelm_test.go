package release

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Every nelm action has to be handed the connection, uninstall included.
// Forgetting it there is invisible in the happy path — nelm silently falls back
// to its own default kubeconfig, finds no release in that other cluster, and
// reports a successful "nothing to do" while the release stays where it is.
//
// The probe is an unroutable API server: the error has to name it. If the
// connection were dropped, the attempt would go to the default kubeconfig (or
// client-go's localhost:8080) and the address would be missing from the error.
func TestNelmApplier_UninstallUsesTheConnection(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "absent"))

	const server = "https://127.0.0.1:1"
	s := Spec{
		Name:      "probe",
		Namespace: "probe",
		Timeout:   5 * time.Second,
		Kube: KubeConnection{
			APIServer:      server,
			TokenData:      "t",
			SkipTLSVerify:  true,
			RequestTimeout: 2 * time.Second,
		},
	}

	err := NelmApplier{LogLevel: "error"}.Uninstall(context.Background(), s)
	if err == nil {
		t.Fatal("Uninstall against an unroutable server succeeded")
	}
	if !strings.Contains(err.Error(), "127.0.0.1:1") {
		t.Errorf("error = %v, want it to name %s — the connection did not reach nelm", err, server)
	}
}

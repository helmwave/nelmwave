package repo

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/helmwave/nelmwave/internal/config"
)

// DockerConfig writes a temporary Docker config.json holding credentials for
// every OCI repository that has a username or password, and returns its path
// plus a cleanup func. When no OCI repository needs auth it returns
// ("", func(){}, nil) so callers can pass the empty path through unchanged
// (nelm then falls back to ~/.docker/config.json).
func DockerConfig(repos map[string]config.Repository) (string, func(), error) {
	auths := map[string]map[string]string{}
	for _, r := range repos {
		if !r.IsOCI() || (r.Username == "" && r.Password == "") {
			continue
		}
		host := ociHost(r.URL)
		token := base64.StdEncoding.EncodeToString([]byte(r.Username + ":" + r.Password))
		auths[host] = map[string]string{
			"username": r.Username,
			"password": r.Password,
			"auth":     token,
		}
	}
	noop := func() {}
	if len(auths) == 0 {
		return "", noop, nil
	}

	data, err := json.Marshal(map[string]any{"auths": auths})
	if err != nil {
		return "", noop, fmt.Errorf("marshal docker config: %w", err)
	}
	f, err := os.CreateTemp("", "nelmwave-docker-*.json")
	if err != nil {
		return "", noop, fmt.Errorf("create docker config: %w", err)
	}
	cleanup := func() { _ = os.Remove(f.Name()) }
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		cleanup()
		return "", noop, fmt.Errorf("write docker config: %w", err)
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", noop, fmt.Errorf("close docker config: %w", err)
	}
	return f.Name(), cleanup, nil
}

// ociHost extracts the registry host from an oci:// or oci+http:// URL (the part
// Docker config auths are keyed by), dropping the scheme and any path.
func ociHost(url string) string {
	host := strings.TrimPrefix(strings.TrimPrefix(url, config.OCIPlainHTTPScheme), config.OCIScheme)
	if i := strings.IndexByte(host, '/'); i >= 0 {
		host = host[:i]
	}
	return host
}

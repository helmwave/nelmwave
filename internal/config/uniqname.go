package config

import (
	"fmt"
	"strings"
)

// Uniqname is the unique identity of a release — the nelmwave equivalent of
// helmwave's uniqname, extended with a kube-context. It is the release's name
// plus an optional target namespace and kube-context, encoded in the manifest
// as the Config.Releases map key in the form:
//
//	name[@namespace[@kubecontext]]
//
// Omitted namespace/kube-context mean "use the current kube-context and its
// default namespace"; that resolution happens at apply time, not at build.
type Uniqname struct {
	Name        string
	Namespace   string
	KubeContext string
}

// ParseUniqname parses a "name[@namespace[@kubecontext]]" key.
//
// The kube-context segment may itself contain '@' (e.g. "user@cluster"), so the
// key is split into at most three fields — name and namespace cannot contain
// '@', the trailing kube-context can. Name must be non-empty.
func ParseUniqname(key string) (Uniqname, error) {
	parts := strings.SplitN(key, "@", 3)
	u := Uniqname{Name: parts[0]}
	if len(parts) > 1 {
		u.Namespace = parts[1]
	}
	if len(parts) > 2 {
		u.KubeContext = parts[2]
	}
	if u.Name == "" {
		return Uniqname{}, fmt.Errorf("release key %q: empty release name", key)
	}
	return u, nil
}

// String renders the identity back to its canonical map-key form, dropping
// trailing empty segments: "name", "name@ns", "name@ns@ctx", or "name@@ctx"
// (namespace empty but kube-context set).
func (u Uniqname) String() string {
	parts := []string{u.Name, u.Namespace, u.KubeContext}
	n := len(parts)
	for n > 1 && parts[n-1] == "" {
		n--
	}
	return strings.Join(parts[:n], "@")
}

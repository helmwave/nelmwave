package release

import (
	"encoding/base64"
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/werf/nelm/pkg/common"
)

// KubeConnection is how to reach the cluster: either a kubeconfig to read, or
// the connection details spelled out directly — which is what CI has, where the
// credentials arrive as a token and a CA in the environment rather than as a
// file.
//
// The current context is not here; it comes from the release's uniqname (see
// Spec.KubeContext), so it varies per release while everything else does not.
type KubeConnection struct {
	// ConfigPaths are kubeconfig files in precedence order; ConfigBase64 is a
	// whole kubeconfig passed by value instead. Empty means the usual client-go
	// default: $KUBECONFIG if set, ~/.kube/config otherwise (see
	// ConfigPathPrecedence).
	ConfigPaths  []string
	ConfigBase64 string
	// ContextCluster / ContextUser override the cluster and user of the selected
	// context.
	ContextCluster string
	ContextUser    string

	// APIServer and friends describe the cluster without a kubeconfig.
	APIServer     string
	TLSServerName string
	SkipTLSVerify bool
	CAPath        string
	CAData        string
	CertPath      string
	CertData      string
	KeyPath       string
	KeyData       string
	TokenData     string
	TokenPath     string
	BasicUsername string
	BasicPassword string
	ProxyURL      string

	// Impersonation: act as another user (kubectl --as).
	ImpersonateUser   string
	ImpersonateGroups []string
	ImpersonateUID    string

	// Client-side limits. Zero leaves nelm's defaults.
	QPSLimit       int
	BurstLimit     int
	RequestTimeout time.Duration
}

// overrides renders the connection as clientcmd overrides. currentContext is the
// release's context, empty for "whatever the kubeconfig says".
func (k KubeConnection) overrides(currentContext string) *clientcmd.ConfigOverrides {
	return &clientcmd.ConfigOverrides{
		AuthInfo: clientcmdapi.AuthInfo{
			ClientCertificate:     k.CertPath,
			ClientCertificateData: []byte(k.CertData),
			ClientKey:             k.KeyPath,
			ClientKeyData:         []byte(k.KeyData),
			Impersonate:           k.ImpersonateUser,
			ImpersonateGroups:     k.ImpersonateGroups,
			ImpersonateUID:        k.ImpersonateUID,
			Password:              k.BasicPassword,
			Token:                 k.TokenData,
			TokenFile:             k.TokenPath,
			Username:              k.BasicUsername,
		},
		ClusterDefaults: clientcmd.ClusterDefaults,
		ClusterInfo: clientcmdapi.Cluster{
			CertificateAuthority:     k.CAPath,
			CertificateAuthorityData: []byte(k.CAData),
			InsecureSkipTLSVerify:    k.SkipTLSVerify,
			ProxyURL:                 k.ProxyURL,
			Server:                   k.APIServer,
			TLSServerName:            k.TLSServerName,
		},
		Context: clientcmdapi.Context{
			AuthInfo: k.ContextUser,
			Cluster:  k.ContextCluster,
		},
		CurrentContext: currentContext,
		Timeout:        k.RequestTimeout.String(),
	}
}

// clientConfig resolves the connection into a clientcmd.ClientConfig. It mirrors
// nelm's own resolution — see nelm/pkg/kube/config.go — so that the namespace
// nelmwave patches and the release nelm installs always land in the same
// cluster. Diverging here would mean labels applied to one cluster and workloads
// to another.
func (k KubeConnection) clientConfig(currentContext string) (clientcmd.ClientConfig, error) {
	overrides := k.overrides(currentContext)

	if k.ConfigBase64 != "" {
		raw, err := base64.StdEncoding.DecodeString(k.ConfigBase64)
		if err != nil {
			return nil, fmt.Errorf("decode base64 kubeconfig: %w", err)
		}
		cfg, err := clientcmd.Load(raw)
		if err != nil {
			return nil, fmt.Errorf("load base64 kubeconfig: %w", err)
		}
		return clientcmd.NewDefaultClientConfig(*cfg, overrides), nil
	}

	rules := &clientcmd.ClientConfigLoadingRules{
		Precedence:          k.ConfigPathPrecedence(),
		MigrationRules:      clientcmd.NewDefaultClientConfigLoadingRules().MigrationRules,
		DefaultClientConfig: &clientcmd.DefaultClientConfig,
	}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides), nil
}

// ConfigPathPrecedence is the list of kubeconfig files to read, in precedence
// order. An explicit --kube-config wins, with ":"-separated entries split like
// kubectl splits them; with no flag, client-go's own rules apply — $KUBECONFIG
// if set, ~/.kube/config otherwise.
//
// Resolving the fallback here rather than leaving the list empty is the point:
// nelm defaults an empty list to ~/.kube/config and never looks at $KUBECONFIG,
// so an empty list would silently deploy to whatever the home kubeconfig's
// current-context happens to be — a live cluster, while the environment pointed
// at a throwaway one. The paths also have to be the same ones nelm reads, or
// namespace metadata and releases would land in different clusters.
func (k KubeConnection) ConfigPathPrecedence() []string {
	if len(k.ConfigPaths) == 0 {
		return clientcmd.NewDefaultClientConfigLoadingRules().Precedence
	}

	paths := make([]string, 0, len(k.ConfigPaths))
	for _, p := range k.ConfigPaths {
		for _, split := range filepath.SplitList(p) {
			if split != "" {
				paths = append(paths, split)
			}
		}
	}
	return paths
}

// RESTConfig builds the client configuration nelmwave uses for its own cluster
// calls (namespace metadata).
func (k KubeConnection) RESTConfig(currentContext string) (*rest.Config, error) {
	clientConfig, err := k.clientConfig(currentContext)
	if err != nil {
		return nil, err
	}

	cfg, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("build kube client config: %w", err)
	}

	cfg.QPS = float32(qpsLimit(k.QPSLimit))
	cfg.Burst = burstLimit(k.BurstLimit)
	return cfg, nil
}

// ContextNames lists the contexts of the resolved kubeconfig, sorted. It is for
// shell completion, so a broken or absent kubeconfig yields nothing rather than
// an error — nothing to complete is a fine answer there.
func (k KubeConnection) ContextNames() []string {
	clientConfig, err := k.clientConfig("")
	if err != nil {
		return nil
	}
	raw, err := clientConfig.RawConfig()
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(raw.Contexts))
	for name := range raw.Contexts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// qpsLimit / burstLimit apply nelm's defaults to a non-positive value, so
// nelmwave's own client is throttled exactly like nelm's.
func qpsLimit(v int) int {
	if v <= 0 {
		return common.DefaultQPSLimit
	}
	return v
}

func burstLimit(v int) int {
	if v <= 0 {
		return common.DefaultBurstLimit
	}
	return v
}

// applyKube copies the connection into nelm's options.
func applyKube(o *common.KubeConnectionOptions, s Spec) {
	k := s.Kube
	// Resolved, not raw: nelm's own fallback for an empty list ignores
	// $KUBECONFIG (see ConfigPathPrecedence).
	o.KubeConfigPaths = k.ConfigPathPrecedence()
	o.KubeConfigBase64 = k.ConfigBase64
	o.KubeContextCurrent = s.KubeContext
	o.KubeContextCluster = k.ContextCluster
	o.KubeContextUser = k.ContextUser

	o.KubeAPIServerAddress = k.APIServer
	o.KubeTLSServerName = k.TLSServerName
	o.KubeSkipTLSVerify = k.SkipTLSVerify
	o.KubeTLSCAPath = k.CAPath
	o.KubeTLSCAData = k.CAData
	o.KubeTLSClientCertPath = k.CertPath
	o.KubeTLSClientCertData = k.CertData
	o.KubeTLSClientKeyPath = k.KeyPath
	o.KubeTLSClientKeyData = k.KeyData
	o.KubeBearerTokenData = k.TokenData
	o.KubeBearerTokenPath = k.TokenPath
	o.KubeBasicAuthUsername = k.BasicUsername
	o.KubeBasicAuthPassword = k.BasicPassword
	o.KubeProxyURL = k.ProxyURL

	o.KubeImpersonateUser = k.ImpersonateUser
	o.KubeImpersonateGroups = k.ImpersonateGroups
	o.KubeImpersonateUID = k.ImpersonateUID

	o.KubeQPSLimit = k.QPSLimit
	o.KubeBurstLimit = k.BurstLimit
	o.KubeRequestTimeout = k.RequestTimeout
}

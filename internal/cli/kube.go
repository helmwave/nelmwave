package cli

import (
	"time"

	"github.com/spf13/pflag"

	"github.com/helmwave/nelmwave/internal/release"
)

// kubeOptions collects the cluster-connection flags. They are global rather than
// per-release: a run talks to one cluster, and the only part that varies per
// release is the context, which comes from its uniqname ("api@app@staging").
//
// Credentials passed by value (token, kubeconfig, key data) exist as flags only
// and deliberately have no manifest counterpart: build copies the manifest into
// .nelmwave/planfile.yml, so a secret written there would end up on disk and in
// CI artifacts.
type kubeOptions struct {
	configPaths    []string
	configBase64   string
	contextCluster string
	contextUser    string

	apiServer     string
	tlsServerName string
	skipTLSVerify bool
	caPath        string
	caData        string
	certPath      string
	certData      string
	keyPath       string
	keyData       string
	token         string
	tokenPath     string
	basicUsername string
	basicPassword string
	proxyURL      string

	impersonateUser   string
	impersonateGroups []string
	impersonateUID    string

	qpsLimit       int
	burstLimit     int
	requestTimeout time.Duration
}

// register adds every connection flag to f. --kube-context lives on
// globalOptions instead: a release's uniqname can override it.
func (o *kubeOptions) register(f *pflag.FlagSet) {
	f.StringSliceVar(&o.configPaths, "kube-config", nil,
		"kubeconfig files, in precedence order (repeatable)")
	f.StringVar(&o.configBase64, "kube-config-base64", "", "kubeconfig as a base64 string")
	f.StringVar(&o.contextCluster, "kube-context-cluster", "", "override the context's cluster")
	f.StringVar(&o.contextUser, "kube-context-user", "", "override the context's user")

	f.StringVar(&o.apiServer, "kube-api-server", "", "API server address, bypassing the kubeconfig")
	f.StringVar(&o.tlsServerName, "kube-api-server-tls-name", "", "server name to expect in the API server certificate")
	f.BoolVar(&o.skipTLSVerify, "no-verify-kube-tls", false, "don't verify the API server certificate")
	f.StringVar(&o.caPath, "kube-ca", "", "path to the API server CA bundle")
	f.StringVar(&o.caData, "kube-ca-data", "", "API server CA bundle, by value (PEM)")
	f.StringVar(&o.certPath, "kube-cert", "", "path to the client certificate")
	f.StringVar(&o.certData, "kube-cert-data", "", "client certificate, by value (PEM)")
	f.StringVar(&o.keyPath, "kube-key", "", "path to the client key")
	f.StringVar(&o.keyData, "kube-key-data", "", "client key, by value (PEM)")
	f.StringVar(&o.token, "kube-token", "", "bearer token, by value")
	f.StringVar(&o.tokenPath, "kube-token-path", "", "path to a file holding the bearer token")
	f.StringVar(&o.basicUsername, "kube-auth-username", "", "basic-auth username")
	f.StringVar(&o.basicPassword, "kube-auth-password", "", "basic-auth password")
	f.StringVar(&o.proxyURL, "kube-proxy-url", "", "proxy to reach the API server through")

	f.StringVar(&o.impersonateUser, "kube-impersonate-user", "", "act as this user (like kubectl --as)")
	f.StringSliceVar(&o.impersonateGroups, "kube-impersonate-group", nil, "act as these groups (repeatable)")
	f.StringVar(&o.impersonateUID, "kube-impersonate-uid", "", "act as this UID")

	f.IntVar(&o.qpsLimit, "kube-qps-limit", 0, "client-side QPS limit (0 = nelm's default of 30)")
	f.IntVar(&o.burstLimit, "kube-burst-limit", 0, "client-side burst limit (0 = nelm's default of 100)")
	f.DurationVar(&o.requestTimeout, "kube-request-timeout", 0, "timeout of a single API request (0 = none)")
}

// connection renders the flags as a release.KubeConnection.
func (o *kubeOptions) connection() release.KubeConnection {
	return release.KubeConnection{
		ConfigPaths:    o.configPaths,
		ConfigBase64:   o.configBase64,
		ContextCluster: o.contextCluster,
		ContextUser:    o.contextUser,

		APIServer:     o.apiServer,
		TLSServerName: o.tlsServerName,
		SkipTLSVerify: o.skipTLSVerify,
		CAPath:        o.caPath,
		CAData:        o.caData,
		CertPath:      o.certPath,
		CertData:      o.certData,
		KeyPath:       o.keyPath,
		KeyData:       o.keyData,
		TokenData:     o.token,
		TokenPath:     o.tokenPath,
		BasicUsername: o.basicUsername,
		BasicPassword: o.basicPassword,
		ProxyURL:      o.proxyURL,

		ImpersonateUser:   o.impersonateUser,
		ImpersonateGroups: o.impersonateGroups,
		ImpersonateUID:    o.impersonateUID,

		QPSLimit:       o.qpsLimit,
		BurstLimit:     o.burstLimit,
		RequestTimeout: o.requestTimeout,
	}
}

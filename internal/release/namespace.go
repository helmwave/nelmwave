package release

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// applyNamespaceMetadata makes sure the release namespace carries the declared
// annotations and labels before the release itself is applied. Policy metadata
// (pod-security, istio-injection) only takes effect on workloads created after
// it lands, so this cannot wait until nelm is done.
//
// nelm creates the namespace with nothing but a name — its API has no hook for
// metadata — so nelmwave talks to the cluster directly here. Existing keys that
// nelmwave does not declare are left untouched: this merges, never replaces.
//
// It is a no-op when nothing is declared, which is the common case.
func applyNamespaceMetadata(ctx context.Context, s Spec) error {
	if len(s.NamespaceAnnotations) == 0 && len(s.NamespaceLabels) == 0 {
		return nil
	}

	clients, err := kubeClient(s)
	if err != nil {
		return err
	}
	api := clients.CoreV1().Namespaces()

	ns, err := api.Get(ctx, s.Namespace, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		// Only create it if the release asked for creation; otherwise report the
		// missing namespace rather than conjuring one behind the user's back.
		if !s.CreateNamespace {
			return fmt.Errorf("namespace %q does not exist and namespace.create is false", s.Namespace)
		}
		created := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: s.Namespace}}
		mergeMetadata(created, s)
		if _, err := api.Create(ctx, created, metav1.CreateOptions{}); err != nil {
			// Someone else may have created it in the meantime; fall through to the
			// update path in that case.
			if !apierrors.IsAlreadyExists(err) {
				return fmt.Errorf("create namespace %q: %w", s.Namespace, err)
			}
			if ns, err = api.Get(ctx, s.Namespace, metav1.GetOptions{}); err != nil {
				return fmt.Errorf("get namespace %q: %w", s.Namespace, err)
			}
		} else {
			return nil
		}
	} else if err != nil {
		return fmt.Errorf("get namespace %q: %w", s.Namespace, err)
	}

	before := len(ns.Annotations) + len(ns.Labels)
	mergeMetadata(ns, s)
	if len(ns.Annotations)+len(ns.Labels) == before && metadataUnchanged(ns, s) {
		return nil // nothing to write
	}
	if _, err := api.Update(ctx, ns, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update namespace %q metadata: %w", s.Namespace, err)
	}
	return nil
}

// mergeMetadata copies the spec's namespace annotations and labels onto ns,
// overwriting only the keys it declares.
func mergeMetadata(ns *corev1.Namespace, s Spec) {
	if len(s.NamespaceAnnotations) > 0 && ns.Annotations == nil {
		ns.Annotations = make(map[string]string, len(s.NamespaceAnnotations))
	}
	for k, v := range s.NamespaceAnnotations {
		ns.Annotations[k] = v
	}
	if len(s.NamespaceLabels) > 0 && ns.Labels == nil {
		ns.Labels = make(map[string]string, len(s.NamespaceLabels))
	}
	for k, v := range s.NamespaceLabels {
		ns.Labels[k] = v
	}
}

// metadataUnchanged reports whether ns already carries every declared key with
// the declared value, so an Update would be a no-op API call.
func metadataUnchanged(ns *corev1.Namespace, s Spec) bool {
	for k, v := range s.NamespaceAnnotations {
		if ns.Annotations[k] != v {
			return false
		}
	}
	for k, v := range s.NamespaceLabels {
		if ns.Labels[k] != v {
			return false
		}
	}
	return true
}

// kubeClient builds a clientset from the spec's kubeconfig and context, matching
// what nelm resolves for the same release.
func kubeClient(s Spec) (*kubernetes.Clientset, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if s.KubeConfig != "" {
		rules.ExplicitPath = s.KubeConfig
	}
	overrides := &clientcmd.ConfigOverrides{}
	if s.KubeContext != "" {
		overrides.CurrentContext = s.KubeContext
	}
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("build kube client config: %w", err)
	}
	clients, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("build kube client: %w", err)
	}
	return clients, nil
}

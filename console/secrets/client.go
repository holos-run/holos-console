package secrets

import (
	"log/slog"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// NewClientset creates a Kubernetes clientset.
// It tries in-cluster config first, then falls back to KUBECONFIG.
// Returns nil clientset (not an error) if no config is available,
// allowing the server to run with only dummy-secret support.
func NewClientset() (kubernetes.Interface, error) {
	config, err := NewRestConfig()
	if err != nil {
		return nil, err
	}
	if config == nil {
		return nil, nil
	}
	return kubernetes.NewForConfig(config)
}

// NewRestConfig resolves the Kubernetes REST config using the same
// strategy as NewClientset: in-cluster first, then KUBECONFIG. Returns
// (nil, nil) when no config is available so callers (for example, the
// controller-runtime manager wiring in console.Serve) can skip the
// cluster-dependent subsystems without tripping the "no cluster" case
// into an error. HOL-620 carved this helper out of NewClientset so the
// controller-runtime manager can share the same loader without re-
// implementing the fallback logic.
func NewRestConfig() (*rest.Config, error) {
	if cfg, err := rest.InClusterConfig(); err == nil {
		slog.Debug("using in-cluster kubernetes config")
		return cfg, nil
	}

	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

	cfg, err := kubeConfig.ClientConfig()
	if err != nil {
		slog.Debug("no kubernetes config available", "error", err)
		return nil, nil
	}
	slog.Debug("using kubeconfig", "host", cfg.Host)
	return cfg, nil
}

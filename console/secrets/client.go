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
	// Try in-cluster config first
	config, err := rest.InClusterConfig()
	if err == nil {
		slog.Debug("using in-cluster kubernetes config")
		return kubernetes.NewForConfig(config)
	}

	// Fall back to KUBECONFIG (respects KUBECONFIG env var)
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

	config, err = kubeConfig.ClientConfig()
	if err != nil {
		// No config available - return nil to allow dummy-secret only mode
		slog.Debug("no kubernetes config available", "error", err)
		return nil, nil
	}

	slog.Debug("using kubeconfig", "host", config.Host)
	return kubernetes.NewForConfig(config)
}

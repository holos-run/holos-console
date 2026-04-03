package deployments

import (
	"log/slog"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// NewDynamicClient creates a Kubernetes dynamic client using in-cluster config
// or KUBECONFIG, mirroring the pattern used by secrets.NewClientset.
// Returns nil (not an error) if no config is available.
func NewDynamicClient() (dynamic.Interface, error) {
	config, err := rest.InClusterConfig()
	if err == nil {
		slog.Debug("using in-cluster kubernetes config for dynamic client")
		return dynamic.NewForConfig(config)
	}

	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

	config, err = kubeConfig.ClientConfig()
	if err != nil {
		slog.Debug("no kubernetes config available for dynamic client", "error", err)
		return nil, nil
	}

	slog.Debug("using kubeconfig for dynamic client", "host", config.Host)
	return dynamic.NewForConfig(config)
}

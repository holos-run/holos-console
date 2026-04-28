package deployments

import (
	"github.com/holos-run/holos-console/internal/deploymentrender"
	"k8s.io/client-go/dynamic"
)

// Applier creates/updates/deletes K8s resources produced by CUE templates.
// Deprecated: use internal/deploymentrender.Applier.
type Applier = deploymentrender.Applier

// NewApplier creates an Applier using the given dynamic client.
// Deprecated: use internal/deploymentrender.NewApplier.
func NewApplier(client dynamic.Interface) *Applier {
	return deploymentrender.NewApplier(client)
}

// ResourceNamespaces extracts the unique set of namespaces from the given
// resources.
// Deprecated: use internal/deploymentrender.ResourceNamespaces.
var ResourceNamespaces = deploymentrender.ResourceNamespaces

var allowedKinds = deploymentrender.AllowedKindGVRs()

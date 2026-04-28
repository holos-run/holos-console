package deployments

import (
	"context"

	"github.com/holos-run/holos-console/internal/deploymentrender"
)

// EvaluateGroupedCUE compiles and evaluates a pre-concatenated CUE source
// document and returns rendered Kubernetes resources grouped by origin.
// Deprecated: use internal/deploymentrender.EvaluateGroupedCUE.
func EvaluateGroupedCUE(ctx context.Context, combinedCUESource string, readPlatformResources bool) (*GroupedResources, error) {
	return deploymentrender.EvaluateGroupedCUE(ctx, combinedCUESource, readPlatformResources)
}

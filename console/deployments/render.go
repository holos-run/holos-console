package deployments

import "github.com/holos-run/holos-console/internal/deploymentrender"

// GroupedResources holds Kubernetes resources partitioned by origin.
// Deprecated: use internal/deploymentrender.GroupedResources.
type GroupedResources = deploymentrender.GroupedResources

// RenderInputs carries the structured inputs every render needs.
// Deprecated: use internal/deploymentrender.RenderInputs.
type RenderInputs = deploymentrender.RenderInputs

// CueRenderer evaluates CUE templates with deployment parameters.
// Deprecated: use internal/deploymentrender.CueRenderer.
type CueRenderer = deploymentrender.CueRenderer

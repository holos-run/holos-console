// Project namespace ReferenceGrant — org/folder-level platform template.
// Emits a Gateway API ReferenceGrant (gateway.networking.k8s.io/v1beta1)
// in the new project's namespace. The grant allows HTTPRoutes in the
// gateway namespace to reference Services in the project namespace —
// the standard pattern for exposing a project's workloads through the
// org-managed ingress gateway.
//
// To activate: create a TemplatePolicyBinding that references this template and
// sets targetRefs.kind = ProjectNamespace.
//
// Example TemplatePolicyBinding snippet:
//
//   apiVersion: console.holos.run/v1alpha2
//   kind: TemplatePolicyBinding
//   metadata:
//     name: namespace-reference-grant
//     namespace: holos-org-acme          # ancestor (org or folder) namespace
//   spec:
//     targetRefs:
//       - kind: ProjectNamespace
//     templateRef:
//       name: namespace-reference-grant-template
//
// The top-level fields (displayName, name, description, cueTemplate) are the
// registry metadata. The cueTemplate field contains the full CUE template body
// as a multi-line string so the outer file is valid CUE while the body can
// freely reference #PlatformInput, #ProjectInput, etc.

displayName: "Project Namespace ReferenceGrant (v1)"
name:        "project-namespace-reference-grant-v1"
description: "Emits a Gateway API ReferenceGrant in the new project namespace, allowing the gateway to forward traffic to project Services."

cueTemplate: """
	// platform is available because platform templates are unified with the
	// deployment template before evaluation (ADR 016 Decision 8).
	platform: #PlatformInput

	// platformResources holds resources the platform team manages. The renderer
	// reads platformResources from org/folder-level templates when a
	// TemplatePolicyBinding targets ProjectNamespace (ADR 034 Decision 2).
	platformResources: {
		// Emit the ReferenceGrant in the new project namespace (namespace-scoped).
		// The HOL-811 applier applies this after the namespace becomes Active.
		namespacedResources: (platform.namespace): {
			ReferenceGrant: "allow-from-gateway": {
				apiVersion: "gateway.networking.k8s.io/v1beta1"
				kind:       "ReferenceGrant"
				metadata: {
					name:      "allow-from-gateway"
					namespace: platform.namespace
					labels: {
						"app.kubernetes.io/managed-by": "console.holos.run"
					}
				}
				spec: {
					// Allow HTTPRoutes in the gateway namespace to reference
					// Services in this project namespace.
					from: [{
						group:     "gateway.networking.k8s.io"
						kind:      "HTTPRoute"
						namespace: platform.gatewayNamespace
					}]
					to: [{
						group: ""
						kind:  "Service"
					}]
				}
			}
		}
		clusterResources: {}
	}
	"""

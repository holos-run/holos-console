// package deployment is the required CUE package declaration for system templates.
// System templates use the same package as deployment templates so they can
// reference deployment template identifiers (input, system, _labels, etc.)
// when unified at deploy time.
package deployment

// output contributes system-managed Kubernetes resources.
// System templates define resources under systemNamespacedResources and
// systemClusterResources so they do not conflict with the deployment template's
// namespacedResources and clusterResources fields.
output: {
	// systemNamespacedResources organizes system-managed namespaced resources.
	systemNamespacedResources: (system.namespace): {
		// HTTPRoute exposes the deployment's Service via the gateway.
		// It routes all traffic from the gateway to the Service named input.name
		// on port 80 (the Service port, which forwards to containerPort input.port).
		// See: https://gateway-api.sigs.k8s.io/api-types/httproute/
		HTTPRoute: (input.name): {
			apiVersion: "gateway.networking.k8s.io/v1"
			kind:       "HTTPRoute"
			metadata: {
				name:      input.name
				namespace: system.namespace
				labels: {
					"app.kubernetes.io/managed-by": "console.holos.run"
					"app.kubernetes.io/name":       input.name
				}
			}
			spec: {
				parentRefs: [{
					group:     "gateway.networking.k8s.io"
					kind:      "Gateway"
					namespace: system.gatewayNamespace
					// Change "default" to the name of your Gateway resource.
					name:      "default"
				}]
				rules: [{
					backendRefs: [{
						name: input.name
						port: 80
					}]
				}]
			}
		}
	}

	// systemClusterResources organizes system-managed cluster-scoped resources (none for this template).
	systemClusterResources: {}
}

// platformResources contributes platform-managed Kubernetes resources.
// Any template at any level can define values for both platformResources and
// projectResources. The renderer reads platformResources from organization and
// folder templates (not project templates). See ADR 016 Decision 8.
platformResources: {
	// namespacedResources organizes platform-managed namespaced resources.
	namespacedResources: (platform.namespace): {
		// HTTPRoute exposes the deployment's Service via the gateway.
		// It routes all traffic from the gateway to the Service named input.name
		// on port 80 (the Service port, which forwards to containerPort input.port).
		// See: https://gateway-api.sigs.k8s.io/api-types/httproute/
		HTTPRoute: (input.name): {
			apiVersion: "gateway.networking.k8s.io/v1"
			kind:       "HTTPRoute"
			metadata: {
				name:      input.name
				namespace: platform.namespace
				labels: {
					"app.kubernetes.io/managed-by": "console.holos.run"
					"app.kubernetes.io/name":       input.name
				}
			}
			spec: {
				parentRefs: [{
					group:     "gateway.networking.k8s.io"
					kind:      "Gateway"
					namespace: platform.gatewayNamespace
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

	// clusterResources organizes platform-managed cluster-scoped resources (none for this template).
	clusterResources: {}
}

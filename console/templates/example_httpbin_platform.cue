// Org-level system template — evaluated at organization scope.
// Any changes here affect every project in the org.
//
// This template does two things:
//  1. Provides an HTTPRoute in platformResources so the gateway routes
//     traffic to the deployment's Service.
//  2. Closes projectResources.namespacedResources to Deployment, Service,
//     and ServiceAccount (ADR 016 Decision 9) so project templates cannot
//     produce any other resource kind.
//
// Pair with console/templates/example_httpbin.cue for the project-level template.

// input and platform are available because system templates are unified with
// the deployment template before evaluation (ADR 016 Decision 8).
input: #ProjectInput & {
	port: >0 & <=65535 | *8080
}
platform: #PlatformInput

// ── Platform resources (managed by the platform team) ───────────────────────

// platformResources holds resources the platform team manages. The renderer
// reads these only from organization/folder-level templates — project templates
// that define platformResources are silently ignored (ADR 016 Decision 8).
platformResources: {
	namespacedResources: (platform.namespace): {
		// HTTPRoute exposes the deployment's Service via the gateway.
		// It routes all traffic from the gateway to the Service named input.name
		// on port 80 (the Service port, which forwards to containerPort input.port).
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
	clusterResources: {}
}

// ── Project resource constraints (enforced by the platform team) ─────────────

// Close projectResources.namespacedResources so that every namespace bucket
// may only contain Deployment, Service, or ServiceAccount. Using close() with
// optional fields is the correct CUE pattern: the close() call marks the struct
// as closed (no additional fields allowed), and the ? marks each listed field
// as optional (a namespace bucket need not contain all three). Any unlisted
// Kind key — such as RoleBinding — is a CUE constraint violation at evaluation
// time, before any Kubernetes API call (ADR 016 Decision 9).
projectResources: namespacedResources: [_]: close({
	Deployment?:     _
	Service?:        _
	ServiceAccount?: _
})

// HTTPRoute platform example — folder-level platform template.
// Drop this into a folder's template set to expose every project in the folder
// via an HTTPRoute into the org-configured ingress gateway.
//
// The top-level fields (displayName, name, description, cueTemplate) are the
// registry metadata. The cueTemplate field contains the full CUE template body
// as a multi-line string so the outer file is valid CUE while the body can
// freely reference #PlatformInput, #ProjectInput, etc.

displayName: "HTTPRoute (v1)"
name:        "httproute-v1"
description: "Exposes project services via an HTTPRoute into the org-configured ingress gateway."

cueTemplate: """
	// input and platform are available because platform templates are unified with
	// the deployment template before evaluation (ADR 016 Decision 8).
	input: #ProjectInput & {
		port: >0 & <=65535 | *8080
	}
	platform: #PlatformInput

	// platformResources holds resources the platform team manages. The renderer
	// reads these only from organization/folder-level templates — project templates
	// that define platformResources are silently ignored (ADR 016 Decision 8).
	platformResources: {
		namespacedResources: (platform.gatewayNamespace): {
			// HTTPRoute routes traffic from the gateway to the project Service on port 80.
			HTTPRoute: (input.name): {
				apiVersion: "gateway.networking.k8s.io/v1"
				kind:       "HTTPRoute"
				metadata: {
					name:      input.name
					namespace: platform.gatewayNamespace
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
							name:      input.name
							namespace: platform.namespace
							port:      80
						}]
					}]
				}
			}
		}
		clusterResources: {}
	}
	"""

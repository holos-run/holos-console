// istio-ingress — Scope C (remote-project) demo template.
//
// Models the `istio-ingress` namespace as a Holos Project.  Each Deployment
// created from this template represents one HTTPRoute resource owned by the
// ingress gateway project.  App-project deployments declare a Scope C
// TemplateDependency on this template so that the Console reverse-dependency
// view shows which other-project deployments depend on each HTTPRoute.
//
// Pinned copy of holos-console-docs/demo/istio-ingress/istio-ingress.cue.
// Both must compile against the v1alpha2 generated schema (docs_sync_test.go).

// platform and input are available because project templates are unified with
// the deployment template before evaluation (ADR 016 Decision 8).
platform: #PlatformInput
input: #ProjectInput & {
	name: =~"^[a-z][a-z0-9-]*$" // DNS label for the HTTPRoute name
	port: >0 & <=65535 | *80
}

// Default values pre-fill the deployment form in the Console UI.
defaults: #ProjectInput & {
	name: "my-app"
	port: 80
}

// _gatewayNamespace is the Kubernetes namespace that hosts the Istio ingress
// gateway.  Sourced from the org-level platform configuration.
let _gatewayNamespace = platform.gatewayNamespace

// _labels are the standard labels required on every resource.
_labels: {
	"app.kubernetes.io/managed-by": "console.holos.run"
	"app.kubernetes.io/name":       input.name
}

// projectResources collects all rendered Kubernetes resources.
// The HTTPRoute lives in _gatewayNamespace (the gateway project's namespace).
projectResources: {
	namespacedResources: (_gatewayNamespace): {
		// HTTPRoute routes external traffic to the backend Service in the
		// app-project namespace.
		HTTPRoute: (input.name): {
			apiVersion: "gateway.networking.k8s.io/v1"
			kind:       "HTTPRoute"
			metadata: {
				name:      input.name
				namespace: _gatewayNamespace
				labels:    _labels
				annotations: {
					// Records the ADR 032 scope for observability.
					"console.holos.run/dependency-scope": "remote-project"
				}
			}
			spec: {
				parentRefs: [{
					group:     "gateway.networking.k8s.io"
					kind:      "Gateway"
					namespace: _gatewayNamespace
					name:      "default"
				}]
				rules: [{
					backendRefs: [{
						// The backend Service lives in the app-project namespace.
						// A ReferenceGrant in that namespace must allow this cross-
						// namespace backend reference (Gateway API requirement).
						name:      input.name
						namespace: platform.namespace
						port:      input.port
					}]
				}]
			}
		}
	}
	clusterResources: {}
}

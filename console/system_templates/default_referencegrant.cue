// package system_template is the required CUE package declaration.
package system_template

// #Claims carries the OIDC ID token claims of the authenticated user.
// These values are set by the console backend from the verified JWT and are
// never supplied directly by the user.
#Claims: {
	iss:            string
	sub:            string
	exp:            int
	iat:            int
	email:          string
	email_verified: bool
	name?:          string
	groups?: [...string]
	... // allow provider-specific claims
}

// #System defines the trusted system-provided fields set by the console backend.
// These values are derived from authenticated context (project namespace resolution
// and OIDC token claims) and are never supplied by the user.
#System: {
	project:   string
	namespace: string
	claims:    #Claims
}

// #Input defines template-specific user or system-configured inputs.
#Input: {
	// gatewayNamespace is the Kubernetes namespace that contains the Gateway
	// resource. HTTPRoute resources in this namespace will be granted
	// permission to reference Services in the project namespace.
	// Defaults to "istio-ingress" per Istio's recommended Helm install
	// convention (https://istio.io/latest/docs/setup/additional-setup/gateway/).
	gatewayNamespace: string | *"istio-ingress"
}

system: #System
input:  #Input

// #Namespaced constrains namespaced resource struct keys to match resource metadata.
// Structure: namespaced.<namespace>.<Kind>.<name>
#Namespaced: [Namespace=string]: [Kind=string]: [Name=string]: {
	kind: Kind
	metadata: {
		name:      Name
		namespace: Namespace
		...
	}
	...
}

// #Cluster constrains cluster-scoped resource struct keys to match resource metadata.
#Cluster: [Kind=string]: [Name=string]: {
	kind: Kind
	metadata: {
		name: Name
		...
	}
	...
}

// namespaced organizes resources that live within a Kubernetes namespace.
namespaced: #Namespaced & {
	(system.namespace): {
		// ReferenceGrant allows HTTPRoute resources in the gateway namespace to
		// reference Service resources in the project namespace.
		// See: https://gateway-api.sigs.k8s.io/api-types/referencegrant/
		ReferenceGrant: "allow-gateway-httproute": {
			apiVersion: "gateway.networking.k8s.io/v1beta1"
			kind:       "ReferenceGrant"
			metadata: {
				name:      "allow-gateway-httproute"
				namespace: system.namespace
				labels: {
					"app.kubernetes.io/managed-by": "console.holos.run"
				}
			}
			spec: {
				from: [{
					group:     "gateway.networking.k8s.io"
					kind:      "HTTPRoute"
					namespace: input.gatewayNamespace
				}]
				to: [{
					group: ""
					kind:  "Service"
				}]
			}
		}
	}
}

// cluster organizes cluster-scoped resources (none for this template).
cluster: #Cluster & {}

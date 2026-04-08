// #KeyRef identifies a key within a Kubernetes Secret or ConfigMap.
#KeyRef: {
	name: string
	key:  string
}

// #EnvVar represents a container environment variable.
// Exactly one of value, secretKeyRef, or configMapKeyRef should be set.
#EnvVar: {
	name: string
	// Exactly one of value, secretKeyRef, or configMapKeyRef.
	value?:           string
	secretKeyRef?:    #KeyRef
	configMapKeyRef?: #KeyRef
}

// #Input defines the user-provided fields the console fills in at render time.
// Constraints here are enforced by CUE before any Kubernetes call is made.
#Input: {
	name:    string & =~"^[a-z][a-z0-9-]*$" // DNS label
	image:   string
	tag:     string
	command?: [...string]
	args?: [...string]
	env:  [...#EnvVar] | *[]
	port: int & >0 & <=65535 | *8080
}

// #Claims carries the OIDC ID token claims of the authenticated user.
// These values are set by the console backend from the verified JWT and are
// never supplied directly by the user.  Standard claims are required; additional
// provider-specific claims are allowed via the open struct (...).
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

// #Platform defines the trusted platform-provided fields set by the console backend.
// These values are derived from authenticated context (project namespace resolution
// and OIDC token claims) and are never supplied by the user.
#Platform: {
	project:          string
	namespace:        string
	// gatewayNamespace is the namespace containing the Gateway resource. It is
	// used in ReferenceGrant specs to allow HTTPRoute resources from that namespace
	// to reference Services in the project namespace.
	gatewayNamespace: string
	organization:     string
	claims:           #Claims
}

input:    #Input
platform: #Platform

// _labels are the standard labels required on every resource.
// app.kubernetes.io/managed-by MUST equal "console.holos.run" or the
// render will be rejected.
_labels: {
	"app.kubernetes.io/name":       input.name
	"app.kubernetes.io/managed-by": "console.holos.run"
}

// _annotations are standard annotations applied to every resource.
// console.holos.run/deployer-email records the identity of the user
// who last rendered and applied this resource.
_annotations: {
	"console.holos.run/deployer-email": platform.claims.email
}

// _envSpec transforms the env input into Kubernetes container env format.
_envSpec: [for e in input.env {
	name: e.name
	if e.value != _|_ {
		value: e.value
	}
	if e.secretKeyRef != _|_ {
		valueFrom: secretKeyRef: {
			name: e.secretKeyRef.name
			key:  e.secretKeyRef.key
		}
	}
	if e.configMapKeyRef != _|_ {
		valueFrom: configMapKeyRef: {
			name: e.configMapKeyRef.name
			key:  e.configMapKeyRef.key
		}
	}
}]

// #Namespaced constrains namespaced resource struct keys to match resource metadata.
// Structure: namespaced.<namespace>.<Kind>.<name>
// The struct path keys must match the corresponding resource metadata fields.
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
// Structure: cluster.<Kind>.<name>
// The struct path keys must match the corresponding resource metadata fields.
#Cluster: [Kind=string]: [Name=string]: {
	kind: Kind
	metadata: {
		name: Name
		...
	}
	...
}

// projectResources collects all rendered Kubernetes resources.
// namespacedResources organizes resources that live within a Kubernetes namespace.
// The struct key path (namespace/Kind/name) must match the resource metadata.
// clusterResources organizes cluster-scoped resources.
projectResources: {
	namespacedResources: #Namespaced & {
		(platform.namespace): {
			// ServiceAccount provides a Kubernetes identity for the pods.
			ServiceAccount: (input.name): {
				apiVersion: "v1"
				kind:       "ServiceAccount"
				metadata: {
					name:        input.name
					namespace:   platform.namespace
					labels:      _labels
					annotations: _annotations
				}
			}

			// Deployment runs the container image.
			Deployment: (input.name): {
				apiVersion: "apps/v1"
				kind:       "Deployment"
				metadata: {
					name:        input.name
					namespace:   platform.namespace
					labels:      _labels
					annotations: _annotations
				}
				spec: {
					replicas: 1
					selector: matchLabels: "app.kubernetes.io/name": input.name
					template: {
						metadata: labels: _labels
						spec: {
							serviceAccountName: input.name
							containers: [{
								name:  input.name
								image: input.image + ":" + input.tag
								if len(_envSpec) > 0 {
									env: _envSpec
								}
								ports: [{containerPort: input.port, name: "http"}]
								if input.command != _|_ {
									command: input.command
								}
								if input.args != _|_ {
									args: input.args
								}
							}]
						}
					}
				}
			}

			// Service exposes port 80 → container port input.port (named "http").
			Service: (input.name): {
				apiVersion: "v1"
				kind:       "Service"
				metadata: {
					name:        input.name
					namespace:   platform.namespace
					labels:      _labels
					annotations: _annotations
				}
				spec: {
					selector: "app.kubernetes.io/name": input.name
					ports: [{port: 80, targetPort: "http", name: "http"}]
				}
			}

			// ReferenceGrant allows HTTPRoute resources in the gateway namespace to
			// reference Service resources in the project namespace.
			// This enables system templates (such as the example HTTPRoute template)
			// to expose deployments via the gateway.
			// See: https://gateway-api.sigs.k8s.io/api-types/referencegrant/
			ReferenceGrant: "allow-gateway-httproute": {
				apiVersion: "gateway.networking.k8s.io/v1beta1"
				kind:       "ReferenceGrant"
				metadata: {
					name:        "allow-gateway-httproute"
					namespace:   platform.namespace
					labels:      _labels
					annotations: _annotations
				}
				spec: {
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
	}

	// clusterResources organizes cluster-scoped resources. Initially empty;
	// extended as cluster resource support is added.
	clusterResources: #Cluster & {}
}

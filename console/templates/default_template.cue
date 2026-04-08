// Use generated type definitions from api/v1alpha1 (prepended by renderer).
// Additional CUE constraints narrow the generated types for this template.
input: #ProjectInput & {
	name: =~"^[a-z][a-z0-9-]*$" // DNS label
	env:  [...#EnvVar] | *[]
	port: >0 & <=65535 | *8080
}
platform: #PlatformInput

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

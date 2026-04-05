// package deployment is the required CUE package declaration.
package deployment

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

// #Input defines the fields the console fills in at render time.
// Constraints here are enforced by CUE before any Kubernetes call is made.
#Input: {
	name:      string & =~"^[a-z][a-z0-9-]*$" // DNS label
	image:     string
	tag:       string
	project:   string
	namespace: string
	command?: [...string]
	args?: [...string]
	env:  [...#EnvVar] | *[]
	port: int & >0 & <=65535 | *8080
}

input: #Input

// _labels are the standard labels required on every resource.
// app.kubernetes.io/managed-by MUST equal "console.holos.run" or the
// render will be rejected.
_labels: {
	"app.kubernetes.io/name":       input.name
	"app.kubernetes.io/managed-by": "console.holos.run"
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

// namespaced organizes resources that live within a Kubernetes namespace.
// The struct key path (namespace/Kind/name) must match the resource metadata.
namespaced: #Namespaced & {
	(input.namespace): {
		// ServiceAccount provides a Kubernetes identity for the pods.
		ServiceAccount: (input.name): {
			apiVersion: "v1"
			kind:       "ServiceAccount"
			metadata: {
				name:      input.name
				namespace: input.namespace
				labels:    _labels
			}
		}

		// Deployment runs the container image.
		Deployment: (input.name): {
			apiVersion: "apps/v1"
			kind:       "Deployment"
			metadata: {
				name:      input.name
				namespace: input.namespace
				labels:    _labels
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
				name:      input.name
				namespace: input.namespace
				labels:    _labels
			}
			spec: {
				selector: "app.kubernetes.io/name": input.name
				ports: [{port: 80, targetPort: "http", name: "http"}]
			}
		}
	}
}

// cluster organizes cluster-scoped resources. Initially empty; extended as
// cluster resource support is added.
cluster: #Cluster & {}

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
	env: [...#EnvVar] | *[]
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

resources: [
	// ServiceAccount provides a Kubernetes identity for the pods.
	{
		apiVersion: "v1"
		kind:       "ServiceAccount"
		metadata: {
			name:      input.name
			namespace: input.namespace
			labels:    _labels
		}
	},

	// Deployment runs holos-console on port 8443 (HTTPS).
	{
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
						ports: [{containerPort: 8443, name: "https"}]
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
	},

	// Service exposes port 443 → container port 8443.
	{
		apiVersion: "v1"
		kind:       "Service"
		metadata: {
			name:      input.name
			namespace: input.namespace
			labels:    _labels
		}
		spec: {
			selector: "app.kubernetes.io/name": input.name
			ports: [{port: 443, targetPort: "https", name: "https"}]
		}
	},
]

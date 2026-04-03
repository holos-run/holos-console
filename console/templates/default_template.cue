package deployment

#Input: {
	name:      string & =~"^[a-z][a-z0-9-]*$"
	image:     string
	tag:       string
	project:   string
	namespace: string
}

input: #Input

resources: [
	// ServiceAccount
	{apiVersion: "v1", kind: "ServiceAccount", metadata: {name: input.name, namespace: input.namespace}},
	// Deployment
	{apiVersion: "apps/v1", kind: "Deployment", metadata: {name: input.name, namespace: input.namespace}, spec: {
		replicas: 1,
		selector: matchLabels: {"app.kubernetes.io/name": input.name},
		template: {
			metadata: labels: {"app.kubernetes.io/name": input.name},
			spec: {
				serviceAccountName: input.name,
				containers: [{
					name:  input.name,
					image: "\(input.image):\(input.tag)",
					ports: [{containerPort: 8080, name: "http"}],
				}],
			},
		},
	}},
	// Service
	{apiVersion: "v1", kind: "Service", metadata: {name: input.name, namespace: input.namespace}, spec: {
		selector: {"app.kubernetes.io/name": input.name},
		ports: [{port: 80, targetPort: "http", name: "http"}],
	}},
	// HTTPRoute
	{apiVersion: "gateway.networking.k8s.io/v1", kind: "HTTPRoute", metadata: {name: input.name, namespace: input.namespace}, spec: {
		parentRefs: [{name: "default", namespace: "istio-ingress"}],
		hostnames: ["\(input.name).\(input.project).holos.localhost"],
		rules: [{
			matches: [{path: {type: "PathPrefix", value: "/"}}],
			backendRefs: [{name: input.name, port: 80}],
		}],
	}},
]

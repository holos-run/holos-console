// podinfo deployment example — project-level deployment template for podinfo.
// Produces: ServiceAccount, Deployment, Service. No ReferenceGrant — pair with
// the httproute-v1 platform template to expose the Service via the gateway.
//
// The top-level fields (displayName, name, description, cueTemplate) are the
// registry metadata. The cueTemplate field contains the full CUE template body
// as a multi-line string so the outer file is valid CUE while the body can
// freely reference #PlatformInput, #ProjectInput, etc.

displayName: "podinfo (v1)"
name:        "podinfo-v1"
description: "Podinfo is a tiny web application made with Go that showcases best practices of running microservices in Kubernetes."

cueTemplate: """
	// defaults declares the template's default values as concrete CUE data.
	// The backend reads this block (via ExtractDefaults) to pre-fill the Create
	// Deployment form. See ADR 027 for the authoritative pre-fill behavior.
	defaults: #ProjectInput & {
		name:        "podinfo"
		image:       "stefanprodan/podinfo"
		tag:         "6.11.2"
		port:        9898
		description: "Podinfo is a tiny web application made with Go that showcases best practices of running microservices in Kubernetes."
	}

	// Use generated type definitions from api/v1alpha2 (prepended by renderer).
	// Additional CUE constraints narrow the generated types for this template.
	input: #ProjectInput & {
		name:  *defaults.name | (string & =~"^[a-z][a-z0-9-]*$") // DNS label
		image: *defaults.image | _
		tag:   *defaults.tag | _
		port:  *defaults.port | (>0 & <=65535)
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

				// Deployment runs the podinfo container.
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
									ports: [{containerPort: input.port, name: "http"}]
								}]
							}
						}
					}
				}

				// Service exposes port 80 → container port input.port (named "http").
				// The HTTPRoute in the org platform template routes gateway traffic here.
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
			}
		}

		// clusterResources organizes cluster-scoped resources (none for this template).
		clusterResources: #Cluster & {}
	}
	"""

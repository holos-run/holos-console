// Valkey cache — project-level deployment template (Scope A: instance dependency).
// Produces a Valkey in-cluster cache: ServiceAccount, Deployment, and Service.
// Demonstrates Scope A (deployment-instance scope) from ADR 032: the cache is
// a per-deployment dependency declared by a TemplateDependency in the project
// namespace so every app that needs Valkey declares its own singleton copy.
//
// Dependency wiring (TemplateDependency — Scope A from ADR 032):
//
//   apiVersion: templates.holos.run/v1alpha1
//   kind: TemplateDependency
//   metadata:
//     name: my-app-requires-valkey
//     namespace: prj-my-project          # same as dependent
//   spec:
//     dependent:
//       namespace: prj-my-project
//       name: my-app
//     requires:
//       namespace: prj-my-project        # same namespace → no TemplateGrant needed
//       name: valkey
//     cascadeDelete: true
//
// No TemplateGrant is required because requires.namespace == dependent.namespace
// (same-namespace reference, ADR 032 Decision 1 Scope A).
//
// The top-level fields (displayName, name, description, cueTemplate) are the
// registry metadata. The cueTemplate field contains the full CUE template body
// as a multi-line string so the outer file is valid CUE while the body can
// freely reference #PlatformInput, #ProjectInput, etc.

displayName: "Valkey Cache (v1)"
name:        "valkey-v1"
description: "Deploys a Valkey in-cluster cache (ServiceAccount, Deployment, Service). Demonstrates Scope A (instance) from ADR 032: a per-deployment dependency declared via TemplateDependency in the project namespace."

cueTemplate: """
	// defaults declares the template's default values as concrete CUE data.
	// The backend reads this block (via ExtractDefaults) to pre-fill the Create
	// Deployment form. See ADR 027 for the authoritative pre-fill behavior.
	defaults: #ProjectInput & {
		name:        "valkey"
		image:       "valkey/valkey"
		tag:         "8.1-alpine"
		port:        6379
		description: "Valkey in-cluster cache (Redis-compatible)."
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

	// projectResources collects all rendered Kubernetes resources.
	// This is a Scope A (instance) dependency: the cache lives in the same
	// project namespace as the app that depends on it.
	//
	// ADR 032 Dependency Wiring (Scope A — instance):
	//   A separate TemplateDependency object (created by the service owner)
	//   declares that every Deployment of "my-app" requires a singleton of
	//   this "valkey" template in the same project namespace. Because
	//   requires.namespace == dependent.namespace, no TemplateGrant is needed.
	projectResources: {
		namespacedResources: #Namespaced & {
			(platform.namespace): {
				// ServiceAccount provides a Kubernetes identity for the cache pods.
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

				// Deployment runs the Valkey cache.
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
									ports: [{containerPort: input.port, name: "valkey"}]
								}]
							}
						}
					}
				}

				// Service exposes Valkey on port 6379 within the project namespace.
				// Apps in the same namespace connect via the Kubernetes DNS name:
				//   <input.name>.<platform.namespace>.svc.cluster.local:6379
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
						ports: [{port: input.port, targetPort: "valkey", name: "valkey"}]
					}
				}
			}
		}

		// clusterResources organizes cluster-scoped resources (none for this template).
		clusterResources: #Cluster & {}
	}
	"""

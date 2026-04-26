// All three dependency scopes — project-level deployment template (ADR 032).
// Composite example: a single template that exercises instance (Scope A),
// project (Scope B), and remote-project (Scope C) dependency scopes from
// ADR 032. The template deploys a minimal web application with an in-process
// cache (Scope A), relies on a platform-mandated ConfigMap (Scope B), and
// is exposed via a gateway-namespace HTTPRoute (Scope C).
//
// === Scope A — Deployment-Instance (TemplateDependency, same-namespace) ===
//
//   apiVersion: templates.holos.run/v1alpha1
//   kind: TemplateDependency
//   metadata:
//     name: myapp-requires-valkey
//     namespace: prj-my-project          # same as dependent
//   spec:
//     dependent:
//       namespace: prj-my-project
//       name: all-scopes                 # this template
//     requires:
//       namespace: prj-my-project        # same namespace → no TemplateGrant needed
//       name: valkey
//     cascadeDelete: true
//
// === Scope B — Project-Wide Mandate (TemplateRequirement, folder namespace) ===
//
//   apiVersion: templates.holos.run/v1alpha1
//   kind: TemplateRequirement
//   metadata:
//     name: require-platform-config
//     namespace: holos-folder-platform   # folder namespace (not a project namespace)
//   spec:
//     requires:
//       namespace: prj-shared-infra
//       name: shared-configmap
//     targetRefs:
//       - kind: Deployment
//         name: "*"
//         projectName: "*"
//     cascadeDelete: true
//
// === Scope C — Remote-Project (TemplateDependency, cross-namespace) ===
//
//   # TemplateGrant in the gateway namespace authorises cross-namespace references.
//   apiVersion: templates.holos.run/v1alpha1
//   kind: TemplateGrant
//   metadata:
//     name: allow-projects
//     namespace: prj-gateway-platform
//   spec:
//     from:
//       - namespace: "*"
//
//   # TemplateDependency in the project namespace references the gateway template.
//   apiVersion: templates.holos.run/v1alpha1
//   kind: TemplateDependency
//   metadata:
//     name: myapp-requires-httproute
//     namespace: prj-my-project
//   spec:
//     dependent:
//       namespace: prj-my-project
//       name: all-scopes
//     requires:
//       namespace: prj-gateway-platform   # different namespace → Scope C
//       name: httproute-with-grant
//     cascadeDelete: false
//
// The top-level fields (displayName, name, description, cueTemplate) are the
// registry metadata. The cueTemplate field contains the full CUE template body
// as a multi-line string so the outer file is valid CUE while the body can
// freely reference #PlatformInput, #ProjectInput, etc.

displayName: "All Three Dependency Scopes (v1)"
name:        "all-scopes-v1"
description: "Composite example exercising Scope A (instance TemplateDependency), Scope B (project TemplateRequirement), and Scope C (remote-project TemplateDependency + TemplateGrant) from ADR 032. Deploys a ServiceAccount, Deployment, and Service; see template comments for the companion CRD objects."

cueTemplate: """
	// defaults declares the template's default values as concrete CUE data.
	// The backend reads this block (via ExtractDefaults) to pre-fill the Create
	// Deployment form. See ADR 027 for the authoritative pre-fill behavior.
	defaults: #ProjectInput & {
		name:        "all-scopes"
		image:       "nginx"
		tag:         "1.27-alpine"
		port:        8080
		description: "Composite demo: instance + project + remote-project dependency scopes."
	}

	// Use generated type definitions from api/v1alpha2 (prepended by renderer).
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
	_annotations: {
		"console.holos.run/deployer-email": platform.claims.email
	}

	// #Namespaced constrains namespaced resource struct keys to match resource metadata.
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

	// projectResources contains the Kubernetes resources rendered by this template.
	//
	// Scope A (instance): a TemplateDependency declares that this template
	//   requires the valkey-v1 template as a same-namespace singleton.
	//   The Valkey Service DNS name consumed: valkey.<platform.namespace>:6379.
	//
	// Scope B (project): a TemplateRequirement in a folder namespace mandates
	//   that a shared-configmap-v1 singleton is present in every project
	//   namespace before this template renders. The ConfigMap is mounted or
	//   referenced by the application at runtime.
	//
	// Scope C (remote-project): a TemplateDependency in this project namespace
	//   references httproute-with-grant-v1 in the gateway platform namespace.
	//   A TemplateGrant in that namespace authorises the cross-namespace ref.
	projectResources: {
		namespacedResources: #Namespaced & {
			(platform.namespace): {
				// ServiceAccount provides a Kubernetes identity for the application pods.
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

				// Deployment runs the application. In production, this deployment would
				// have VALKEY_ADDR injected from the Scope A singleton Service and
				// platform config loaded from the Scope B ConfigMap singleton.
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
									env: [
										// Scope A: valkey singleton Service DNS name.
										// Use string concatenation (not interpolation) so the template
										// compiles cleanly in both the outer CUE file and the renderer.
										{
											name:  "VALKEY_ADDR"
											value: "valkey." + platform.namespace + ".svc.cluster.local:6379"
										},
										// Scope B: platform-config ConfigMap key reference.
										{
											name: "PLATFORM_ORGANIZATION"
											valueFrom: configMapKeyRef: {
												name:     "platform-config"
												key:      "organization"
												optional: true
											}
										},
									]
								}]
							}
						}
					}
				}

				// Service exposes the application on port 80 → container port.
				// The Scope C HTTPRoute in the gateway namespace routes external
				// traffic here via a cross-namespace backendRef.
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

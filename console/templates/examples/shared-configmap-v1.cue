// Shared ConfigMap — project-level deployment template (Scope B: project dependency).
// Produces a ConfigMap containing shared platform configuration consumed by
// all deployments in the project namespace.
//
// Demonstrates Scope B (project scope) from ADR 032: a TemplateRequirement
// stored in a folder or org namespace mandates that all matching deployments
// in every reachable project have this singleton deployed first.
//
// Dependency wiring (TemplateRequirement — Scope B from ADR 032):
//
//   apiVersion: templates.holos.run/v1alpha1
//   kind: TemplateRequirement
//   metadata:
//     name: all-projects-require-platform-config
//     namespace: holos-folder-platform     # folder namespace (not a project namespace)
//   spec:
//     requires:
//       namespace: prj-shared-infra        # project namespace that owns the template
//       name: shared-configmap
//     targetRefs:
//       - kind: Deployment
//         name: "*"           # all deployments
//         projectName: "*"    # all projects reachable from holos-folder-platform
//     cascadeDelete: true
//
// The TemplateRequirement must live in a folder or org namespace — it is
// silently ignored (and rejected by admission policy) when stored in a project
// namespace. Each matched deployment gets a singleton of this template in its
// project namespace (ADR 032 Decision 1 Scope B).
//
// The top-level fields (displayName, name, description, cueTemplate) are the
// registry metadata. The cueTemplate field contains the full CUE template body
// as a multi-line string so the outer file is valid CUE while the body can
// freely reference #PlatformInput, #ProjectInput, etc.

displayName: "Shared Platform ConfigMap (v1)"
name:        "shared-configmap-v1"
description: "Emits a shared ConfigMap with platform-wide configuration for all deployments in the project namespace. Demonstrates Scope B (project-wide mandate) from ADR 032: a TemplateRequirement in a folder namespace mandates a singleton of this template in every matching project."

cueTemplate: """
	// defaults declares the template's default values as concrete CUE data.
	// The backend reads this block (via ExtractDefaults) to pre-fill the Create
	// Deployment form. See ADR 027 for the authoritative pre-fill behavior.
	defaults: #ProjectInput & {
		name:        "platform-config"
		description: "Shared platform configuration for all deployments."
	}

	// Use generated type definitions from api/v1alpha2 (prepended by renderer).
	input: #ProjectInput & {
		name: *defaults.name | (string & =~"^[a-z][a-z0-9-]*$") // DNS label
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

	// projectResources collects all rendered Kubernetes resources.
	// This is a Scope B (project-wide) dependency: the ConfigMap is placed in
	// each project namespace by the singleton materialisation triggered by
	// the TemplateRequirement in the folder namespace.
	//
	// ADR 032 Dependency Wiring (Scope B — project):
	//   A TemplateRequirement in a folder namespace mandates that every
	//   Deployment matching its targetRefs has a singleton of this template
	//   deployed in the same project namespace. No per-project TemplateDependency
	//   is needed — the TemplateRequirement is the single source of truth.
	projectResources: {
		namespacedResources: #Namespaced & {
			(platform.namespace): {
				// ConfigMap carries shared platform-wide configuration values.
				// All deployments in the namespace can mount or reference this ConfigMap.
				ConfigMap: (input.name): {
					apiVersion: "v1"
					kind:       "ConfigMap"
					metadata: {
						name:        input.name
						namespace:   platform.namespace
						labels:      _labels
						annotations: _annotations
					}
					data: {
						// organization is the logical grouping name for this deployment.
						organization: platform.organization
						// project is the platform project name for label/annotation use.
						project: platform.project
						// namespace is the Kubernetes namespace for cross-resource references.
						namespace: platform.namespace
						// gatewayNamespace is the ingress gateway namespace for HTTPRoute wiring.
						gatewayNamespace: platform.gatewayNamespace
					}
				}
			}
		}

		// clusterResources organizes cluster-scoped resources (none for this template).
		clusterResources: #Cluster & {}
	}
	"""

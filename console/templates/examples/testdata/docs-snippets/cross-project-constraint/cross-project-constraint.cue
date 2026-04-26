// Cross-project constraint — Project A constrains Project B (Scope B: project mandate).
//
// This demo snippet demonstrates ADR 032 Scope B: a TemplateRequirement in
// project-a's folder namespace mandates that all deployments in project-b
// (and all other projects in the same folder) require a singleton of the
// "shared-configmap" template before they can render.
//
// The template itself is a simple project-level template that emits a
// ConfigMap with shared configuration. The cross-project constraint is
// expressed via the companion CRDs shown in the comments below.
//
// === Scope B CRD wiring: Project A constrains Project B ===
//
// Assuming:
//   - project-a is in folder "platform-team-folder" (namespace: holos-folder-platform)
//   - project-b is a sibling project under the same folder
//   - shared-configmap template lives in project-a's namespace: prj-project-a
//
// Step 1: TemplateGrant in project-a's namespace authorises project-b to
//         declare a cross-namespace dependency (Scope C enabled):
//
//   apiVersion: templates.holos.run/v1alpha1
//   kind: TemplateGrant
//   metadata:
//     name: allow-sibling-projects
//     namespace: prj-project-a
//   spec:
//     from:
//       - namespace: "*"    # permit all sibling project namespaces
//
// Step 2: TemplateRequirement in the shared folder namespace mandates
//         that all projects in the folder have the shared-configmap singleton
//         (Scope B: project-wide mandate from the platform team):
//
//   apiVersion: templates.holos.run/v1alpha1
//   kind: TemplateRequirement
//   metadata:
//     name: all-projects-require-platform-config
//     namespace: holos-folder-platform    # folder namespace (not a project namespace)
//   spec:
//     requires:
//       namespace: prj-project-a          # project-a owns the required template
//       name: shared-configmap            # the template project-b must have
//     targetRefs:
//       - kind: Deployment
//         name: "*"           # all deployments
//         projectName: "*"    # all projects reachable via ancestor walk
//     cascadeDelete: true
//
// This is a textbook "Project A constrains Project B" pattern from ADR 032:
//   - The TemplateRequirement is owned by the platform team in the folder namespace.
//   - The required template lives in project-a (owned by the platform team).
//   - Project-b's deployments are mandated to have the singleton without any
//     action by project-b's service owner — the mandate is authoritative.
//
// Note: This file is a CUE template body (not a registry example). It is
// stored in holos-console-docs/demo/ as demo walkthrough material and mirrored
// as a testdata/docs-snippets/ sync copy in holos-console. Both must compile
// against the v1alpha2 generated schema (enforced by docs_sync_test.go).

// platform is available because platform templates are unified with the
// deployment template before evaluation (ADR 016 Decision 8).
platform: #PlatformInput

// projectResources emits the shared ConfigMap.
// In Scope B, this template is instantiated as a singleton in every project
// namespace matched by the TemplateRequirement's targetRefs.
projectResources: {
	namespacedResources: {
		(platform.namespace): {
			// ConfigMap carries shared platform-wide configuration values.
			// Deployed as a singleton in every project namespace that the
			// TemplateRequirement targets.
			ConfigMap: "platform-config": {
				apiVersion: "v1"
				kind:       "ConfigMap"
				metadata: {
					name:      "platform-config"
					namespace: platform.namespace
					labels: {
						"app.kubernetes.io/managed-by": "console.holos.run"
						"app.kubernetes.io/name":       "platform-config"
					}
					annotations: {
						// Records the ADR scope for observability.
						"console.holos.run/dependency-scope": "project"
						// Records which project mandated this singleton.
						"console.holos.run/required-by": "all-projects-require-platform-config"
					}
				}
				data: {
					// organization is the logical grouping name for this deployment.
					organization: platform.organization
					// project is the platform project name.
					project: platform.project
					// namespace is the Kubernetes namespace.
					namespace: platform.namespace
					// gatewayNamespace is the ingress gateway namespace.
					gatewayNamespace: platform.gatewayNamespace
				}
			}
		}
	}
	clusterResources: {}
}

// Project namespace description annotation — org/folder-level platform template.
// Unifies a single "description" annotation onto the new project's namespace.
// No other resources are produced. Use this as the minimal starting point for
// any template that only needs to label or annotate a namespace at creation time.
//
// To activate: create a TemplatePolicyBinding that references this template and
// sets targetRefs.kind = ProjectNamespace.
//
// Example TemplatePolicyBinding snippet:
//
//   apiVersion: console.holos.run/v1alpha2
//   kind: TemplatePolicyBinding
//   metadata:
//     name: namespace-description
//     namespace: holos-org-acme          # ancestor (org or folder) namespace
//   spec:
//     targetRefs:
//       - kind: ProjectNamespace
//     templateRef:
//       name: namespace-description-template
//
// The top-level fields (displayName, name, description, cueTemplate) are the
// registry metadata. The cueTemplate field contains the full CUE template body
// as a multi-line string so the outer file is valid CUE while the body can
// freely reference #PlatformInput, #ProjectInput, etc.

displayName: "Project Namespace Description Annotation (v1)"
name:        "project-namespace-description-annotation-v1"
description: "Adds a description annotation to the new project's namespace. Minimal ProjectNamespace example — no other resources."

cueTemplate: """
	// platform is available because platform templates are unified with the
	// deployment template before evaluation (ADR 016 Decision 8).
	platform: #PlatformInput

	// platformResources holds resources the platform team manages. The renderer
	// reads platformResources from org/folder-level templates when a
	// TemplatePolicyBinding targets ProjectNamespace (ADR 034 Decision 2).
	platformResources: {
		// Patch the new project's namespace with a human-readable description.
		// The Namespace object is merged (not replaced) into the base namespace
		// the RPC constructs, so existing labels and annotations are preserved.
		clusterResources: {
			Namespace: (platform.namespace): {
				apiVersion: "v1"
				kind:       "Namespace"
				metadata: {
					name: platform.namespace
					annotations: {
						"console.holos.run/description": "Managed by the Holos platform team."
					}
				}
			}
		}
		namespacedResources: {}
	}
	"""

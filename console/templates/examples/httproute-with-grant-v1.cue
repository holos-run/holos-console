// HTTPRoute with TemplateGrant — folder-level platform template (Scope C: remote-project).
// Emits an HTTPRoute in the org-configured ingress gateway namespace.
// A companion TemplateGrant (shown in the comments below) must be created in the
// gateway platform namespace to authorise project namespaces to declare cross-namespace
// TemplateDependency objects referencing this template.
//
// Demonstrates Scope C (remote-project scope) from ADR 032: a TemplateDependency
// in a project namespace references a template owned by a different namespace
// (the gateway platform namespace). The TemplateGrant in the gateway namespace
// authorises the cross-namespace reference.
//
// Dependency wiring (TemplateGrant + TemplateDependency — Scope C from ADR 032):
//
//   # Step 1: TemplateGrant in the gateway/platform namespace authorises project namespaces.
//   apiVersion: templates.holos.run/v1alpha1
//   kind: TemplateGrant
//   metadata:
//     name: allow-project-namespaces
//     namespace: prj-gateway-platform      # namespace that owns the gateway template
//   spec:
//     from:
//       - namespace: "*"                   # allow any project namespace to reference
//
//   # Step 2: TemplateDependency in the app's project namespace (Scope C).
//   apiVersion: templates.holos.run/v1alpha1
//   kind: TemplateDependency
//   metadata:
//     name: my-app-requires-httproute
//     namespace: prj-my-project
//   spec:
//     dependent:
//       namespace: prj-my-project
//       name: my-app
//     requires:
//       namespace: prj-gateway-platform    # different namespace → Scope C
//       name: httproute-with-grant
//     cascadeDelete: false                 # gateway lifecycle is decoupled from app
//
// Because requires.namespace != dependent.namespace this is Scope C (remote-project).
// The TemplateGrant in prj-gateway-platform is mandatory; without it the
// TemplateDependency reconciler sets ResolvedRefs=False/GrantNotFound.
//
// The top-level fields (displayName, name, description, cueTemplate) are the
// registry metadata. The cueTemplate field contains the full CUE template body
// as a multi-line string so the outer file is valid CUE while the body can
// freely reference #PlatformInput, #ProjectInput, etc.

displayName: "HTTPRoute with TemplateGrant (v1)"
name:        "httproute-with-grant-v1"
description: "Exposes project services via an HTTPRoute in the org gateway namespace. Demonstrates Scope C (remote-project) from ADR 032: the TemplateGrant in the platform namespace authorises project TemplateDependency objects to declare a cross-namespace dependency on this template."

cueTemplate: """
	// input and platform are available because platform templates are unified with
	// the deployment template before evaluation (ADR 016 Decision 8).
	input: #ProjectInput & {
		port: >0 & <=65535 | *8080
	}
	platform: #PlatformInput

	// platformResources holds resources the platform team manages. The renderer
	// reads these only from organization/folder-level templates — project templates
	// that define platformResources are silently ignored (ADR 016 Decision 8).
	platformResources: {
		namespacedResources: (platform.gatewayNamespace): {
			// HTTPRoute routes traffic from the gateway to the project Service on port 80.
			// Lives in the gateway namespace (Scope C: remote-project dependency wiring).
			HTTPRoute: (input.name): {
				apiVersion: "gateway.networking.k8s.io/v1"
				kind:       "HTTPRoute"
				metadata: {
					name:      input.name
					namespace: platform.gatewayNamespace
					labels: {
						"app.kubernetes.io/managed-by": "console.holos.run"
						"app.kubernetes.io/name":       input.name
					}
					// ADR 032 annotation: records the scope for observability.
					annotations: {
						"console.holos.run/dependency-scope": "remote-project"
					}
				}
				spec: {
					parentRefs: [{
						group:     "gateway.networking.k8s.io"
						kind:      "Gateway"
						namespace: platform.gatewayNamespace
						name:      "default"
					}]
					rules: [{
						backendRefs: [{
							name:      input.name
							namespace: platform.namespace
							port:      80
						}]
					}]
				}
			}
		}
		clusterResources: {}
	}
	"""

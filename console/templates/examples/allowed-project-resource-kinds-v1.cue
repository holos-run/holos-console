// Allowed project resource kinds — folder/org-level platform template.
// Closes projectResources.namespacedResources to Deployment, Service, and
// ServiceAccount (ADR 016 Decision 9). Drop this into a folder's or org's
// template set to enforce the kind constraint across all projects in scope.
//
// The top-level fields (displayName, name, description, cueTemplate) are the
// registry metadata. The cueTemplate field contains the full CUE template body
// as a multi-line string so the outer file is valid CUE while the body can
// freely reference #PlatformInput, #ProjectInput, etc.

displayName: "Allowed Project Resource Kinds (v1)"
name:        "allowed-project-resource-kinds-v1"
description: "Closes projectResources.namespacedResources to Deployment, Service, and ServiceAccount (ADR 016 Decision 9)."

cueTemplate: """
	// platform is available because platform templates are unified with the
	// deployment template before evaluation (ADR 016 Decision 8).
	platform: #PlatformInput

	// Close projectResources.namespacedResources so that every namespace bucket
	// may only contain Deployment, Service, or ServiceAccount. Using close() with
	// optional fields is the correct CUE pattern: the close() call marks the struct
	// as closed (no additional fields allowed), and the ? marks each listed field
	// as optional (a namespace bucket need not contain all three). Any unlisted
	// Kind key — such as RoleBinding — is a CUE constraint violation at evaluation
	// time, before any Kubernetes API call (ADR 016 Decision 9).
	projectResources: namespacedResources: [_]: close({
		Deployment?:     _
		Service?:        _
		ServiceAccount?: _
	})
	"""

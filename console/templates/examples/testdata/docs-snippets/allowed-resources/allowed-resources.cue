// a209178-materia / Folders / default / Platform Templates / allowed-resources

// Close projectResources.namespacedResources so that every namespace bucket
// may only contain Deployment, Service, or ServiceAccount. Using close() with
// optional fields is the correct CUE pattern: the close() call marks the struct
// as closed (no additional fields allowed), and the ? marks each listed field
// as optional (a namespace bucket need not contain all three). Any unlisted
// Kind key — such as RoleBinding — is a CUE constraint violation at evaluation
// time, before any Kubernetes API call (ADR 016 Decision 9).
projectResources: {
	namespacedResources: close({
		// constrain the project template to the project namespace
		(platform.namespace): close({
			// constrain the project template to these allowed resource kinds
			Deployment?:     _
			Service?:        _
			ServiceAccount?: _
			ReferenceGrant?: _
		})
	})
	// Disallow project templates from managing cluster scoped resources.
	clusterResources: close({})
}

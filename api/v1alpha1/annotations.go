package v1alpha1

const (
	// APIVersion is the schema version for v1alpha1 types.
	APIVersion = "console.holos.run/v1alpha1"

	// Resource kinds.
	KindResourceSet  = "ResourceSet"
	KindOrganization = "Organization"
	KindProject      = "Project"

	// Labels.
	LabelManagedBy    = "app.kubernetes.io/managed-by"
	LabelResourceType = "console.holos.run/resource-type"
	LabelOrganization = "console.holos.run/organization"
	LabelProject      = "console.holos.run/project"

	// Label values.
	ManagedByValue               = "console.holos.run"
	ResourceTypeOrganization     = "organization"
	ResourceTypeProject          = "project"
	ResourceTypeDeployment       = "deployment"
	ResourceTypeDeploymentTemplate = "deployment-template"
	ResourceTypeSystemTemplate   = "system-template"

	// Annotations.
	AnnotationDisplayName       = "console.holos.run/display-name"
	AnnotationDescription       = "console.holos.run/description"
	AnnotationCreatorEmail      = "console.holos.run/creator-email"
	AnnotationShareUsers        = "console.holos.run/share-users"
	AnnotationShareRoles        = "console.holos.run/share-roles"
	AnnotationDefaultShareUsers = "console.holos.run/default-share-users"
	AnnotationDefaultShareRoles = "console.holos.run/default-share-roles"
	AnnotationDeployment        = "console.holos.run/deployment"
	AnnotationDeployerEmail     = "console.holos.run/deployer-email"
	AnnotationURL               = "console.holos.run/url"
	AnnotationMandatory         = "console.holos.run/mandatory"
	AnnotationEnabled           = "console.holos.run/enabled"
	AnnotationSettings          = "console.holos.run/project-settings"
)

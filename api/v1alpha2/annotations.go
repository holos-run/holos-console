package v1alpha2

const (
	// APIVersion is the schema version for v1alpha2 types.
	APIVersion = "console.holos.run/v1alpha2"

	// Resource kinds.
	KindResourceSet  = "ResourceSet"
	KindOrganization = "Organization"
	KindFolder       = "Folder"
	KindProject      = "Project"

	// Labels.
	LabelManagedBy    = "app.kubernetes.io/managed-by"
	LabelResourceType = "console.holos.run/resource-type"
	LabelOrganization = "console.holos.run/organization"
	LabelFolder       = "console.holos.run/folder"
	LabelProject      = "console.holos.run/project"

	// Label values.
	ManagedByValue                 = "console.holos.run"
	ResourceTypeOrganization       = "organization"
	ResourceTypeFolder             = "folder"
	ResourceTypeProject            = "project"
	ResourceTypeDeployment         = "deployment"
	ResourceTypeDeploymentTemplate = "deployment-template"
	ResourceTypeOrgTemplate        = "org-template"

	// Annotations.
	AnnotationDisplayName  = "console.holos.run/display-name"
	AnnotationDescription  = "console.holos.run/description"
	AnnotationCreatorEmail = "console.holos.run/creator-email"
	AnnotationShareUsers   = "console.holos.run/share-users"
	AnnotationShareRoles   = "console.holos.run/share-roles"
	// AnnotationDefaultShareUsers specifies the default share users annotation.
	// This annotation appears on org, folder, and project namespaces and drives
	// the default-share cascade chain applied when a new Secret is created
	// (ADR 020 Decision 9).
	AnnotationDefaultShareUsers = "console.holos.run/default-share-users"
	// AnnotationDefaultShareRoles specifies the default share roles annotation.
	// This annotation appears on org, folder, and project namespaces and drives
	// the default-share cascade chain applied when a new Secret is created
	// (ADR 020 Decision 9).
	AnnotationDefaultShareRoles = "console.holos.run/default-share-roles"
	AnnotationDeployment        = "console.holos.run/deployment"
	AnnotationDeployerEmail     = "console.holos.run/deployer-email"
	AnnotationURL               = "console.holos.run/url"
	AnnotationMandatory         = "console.holos.run/mandatory"
	AnnotationEnabled           = "console.holos.run/enabled"
	AnnotationSettings          = "console.holos.run/project-settings"
	// AnnotationParent is the Kubernetes namespace name of the immediate parent
	// (organization namespace or folder namespace). Added in v1alpha2 and
	// present on both Folder and Project namespaces. The hierarchy walk follows
	// this label upward to collect templates and resolve permissions (ADR 020
	// Decision 3 and Decision 6).
	AnnotationParent = "console.holos.run/parent"
	// AnnotationLinkedOrgTemplates stores the list of explicitly linked platform
	// template names as a JSON array on a deployment template ConfigMap.
	// Only enabled (non-mandatory) org templates appear here; mandatory templates
	// always participate in render regardless of this annotation.
	// Example: ["microservice-v2", "istio-gateway"]
	AnnotationLinkedOrgTemplates = "console.holos.run/linked-org-templates"
)

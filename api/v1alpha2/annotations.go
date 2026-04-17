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
	// ResourceTypeTemplate is the unified v1alpha2 template resource type.
	// It replaces ResourceTypeDeploymentTemplate (project-scoped) and
	// ResourceTypeOrgTemplate (org-scoped) with a single label value used
	// across all hierarchy levels (ADR 021 Decision 4).
	ResourceTypeTemplate = "template"
	// ResourceTypeTemplatePolicy is the resource type label value for
	// TemplatePolicy ConfigMaps. TemplatePolicy objects bind REQUIRE/EXCLUDE
	// rules to templates and replace the removed `mandatory` flag on Template
	// and LinkableTemplate (HOL-554/HOL-555). Policy ConfigMaps live only in
	// organization or folder namespaces; project-scoped storage is forbidden
	// because a project owner could otherwise tamper with the very policy the
	// platform meant to constrain them with.
	ResourceTypeTemplatePolicy = "template-policy"

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
	// AnnotationDefaultFolder stores the identifier (slug) of the default folder
	// for an organization. Written when the org is created and updatable via
	// UpdateOrganization. New projects without an explicit parent are placed in
	// this folder (ADR 022 Decision 3).
	AnnotationDefaultFolder = "console.holos.run/default-folder"
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
	// AnnotationLinkedTemplates stores the list of explicitly linked cross-level
	// template references as a JSON array of LinkedTemplateRef objects on a
	// template ConfigMap. Replaces AnnotationLinkedOrgTemplates in v1alpha2.
	// Example: [{"scope":"organization","scope_name":"acme","name":"microservice-v2"}]
	AnnotationLinkedTemplates = "console.holos.run/linked-templates"
	// LabelTemplateScope identifies the hierarchy level of a template ConfigMap.
	// Values: "organization", "folder", "project" (ADR 021 Decision 4).
	LabelTemplateScope = "console.holos.run/template-scope"
	// AnnotationTemplatePolicyRules stores the JSON-serialized list of
	// TemplatePolicyRule entries for a TemplatePolicy ConfigMap. The handler
	// serializes the proto rules on write and round-trips them back on read;
	// this mirrors the AnnotationLinkedTemplates pattern used on Template
	// ConfigMaps (HOL-556).
	AnnotationTemplatePolicyRules = "console.holos.run/template-policy-rules"

	// ResourceTypeRenderState labels a ConfigMap whose sole purpose is to hold
	// the last-applied TemplatePolicy render set for a single target (a
	// ProjectTemplate or a Deployment). These ConfigMaps live exclusively in
	// folder or organization namespaces, never in project namespaces, so a
	// project owner cannot tamper with the drift marker the platform reads to
	// detect policy drift (HOL-557 storage-isolation rule).
	//
	// We chose shape (b) from the HOL-557 acceptance criteria — a dedicated
	// resource-typed ConfigMap per target — rather than an annotation on the
	// per-deployment/per-template ConfigMap, because the per-target
	// ConfigMaps live in the project namespace where the project owner can
	// write to them. Storing the drift marker separately in the folder
	// namespace keeps the invariant enforceable.
	ResourceTypeRenderState = "render-state"

	// AnnotationRenderStateTarget records the target kind a render-state
	// ConfigMap belongs to. Values are the string form of the
	// policyresolver.TargetKind enum ("project-template" or "deployment") so
	// the storage helper can list the applied sets for a single kind without
	// parsing the ConfigMap name.
	AnnotationRenderStateTarget = "console.holos.run/render-state-target"

	// AnnotationRenderStateProject records the project name a render-state
	// ConfigMap's target belongs to. Folder-namespace ConfigMaps inherit the
	// folder's scope from their own namespace; this annotation adds the
	// missing project dimension so a single folder namespace can hold drift
	// state for every project nested underneath it.
	AnnotationRenderStateProject = "console.holos.run/render-state-project"

	// AnnotationRenderStateTargetName records the target resource name
	// (deployment name or project-template name) the render-state ConfigMap
	// describes. The ConfigMap name itself is a deterministic hash of
	// (project, target-kind, target-name) to stay within the 63-char DNS
	// label limit; this annotation preserves the original target name for
	// humans and for resolver lookups.
	AnnotationRenderStateTargetName = "console.holos.run/render-state-target-name"

	// RenderStateAppliedSetKey is the ConfigMap data key that holds the
	// JSON-serialized slice of LinkedTemplateRef values for the last-applied
	// render set. The resolver deserializes this on every drift check.
	RenderStateAppliedSetKey = "applied-set.json"

	// Release ConfigMap labels and annotations (ADR 024).

	// ResourceTypeTemplateRelease is the resource type label value for release
	// ConfigMaps, distinguishing them from live template ConfigMaps.
	ResourceTypeTemplateRelease = "template-release"
	// LabelReleaseOf identifies which template a release ConfigMap belongs to.
	LabelReleaseOf = "console.holos.run/release-of"
	// AnnotationTemplateVersion stores the semver version string of a release.
	AnnotationTemplateVersion = "console.holos.run/template-version"
	// ChangelogKey is the ConfigMap data key for the release changelog.
	ChangelogKey = "changelog"
	// UpgradeAdviceKey is the ConfigMap data key for upgrade advice text.
	UpgradeAdviceKey = "upgrade-advice"
	// TemplateScopeOrganization is the LabelTemplateScope value for org-level templates.
	TemplateScopeOrganization = "organization"
	// TemplateScopeFolder is the LabelTemplateScope value for folder-level templates.
	TemplateScopeFolder = "folder"
	// TemplateScopeProject is the LabelTemplateScope value for project-level templates.
	TemplateScopeProject = "project"
)

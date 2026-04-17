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
	// ResourceTypeRenderState is the resource type label value for
	// applied-render-set ConfigMaps (HOL-557/HOL-567). A render-state
	// ConfigMap records the effective set of LinkedTemplateRef values last
	// applied to a given render target (deployment or project-scope
	// template). It is keyed by `(targetKind, targetName)` within a single
	// folder or organization namespace. Render-state ConfigMaps MUST live
	// in the folder namespace that owns the project (falling back to the
	// organization namespace when the project's immediate parent is the
	// organization) — NEVER in a project namespace, because project owners
	// have namespace-scoped write access and could otherwise forge drift
	// evidence.
	ResourceTypeRenderState = "render-state"
	// LabelRenderTargetKind records the kind of render target a render-state
	// ConfigMap belongs to. Values are "deployment" (mirrors
	// ResourceTypeDeployment) or "project-template".
	LabelRenderTargetKind = "console.holos.run/render-target-kind"
	// LabelRenderTargetProject records the project slug owning the render
	// target. Folder-namespace render-state ConfigMaps carry this label so
	// the handler can list by project without re-walking the ancestor
	// chain.
	LabelRenderTargetProject = "console.holos.run/render-target-project"
	// LabelRenderTargetName records the render target's own name
	// (deployment name or template name within the project).
	LabelRenderTargetName = "console.holos.run/render-target-name"
	// AnnotationAppliedRenderSet stores the JSON-serialized list of
	// LinkedTemplateRef values applied at the last successful render of the
	// target. Used to detect policy drift by comparing the stored set
	// against the current resolver output.
	AnnotationAppliedRenderSet = "console.holos.run/applied-render-set"
	// RenderTargetKindDeployment is the LabelRenderTargetKind value for
	// Deployment render targets.
	RenderTargetKindDeployment = "deployment"
	// RenderTargetKindProjectTemplate is the LabelRenderTargetKind value
	// for project-scope Template render targets.
	RenderTargetKindProjectTemplate = "project-template"

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
	// AnnotationRequiredTemplate stamps ownership on resources that were
	// applied by the project-creation-time REQUIRE-rule evaluator
	// (HOL-571). It lives in a separate namespace from AnnotationDeployment
	// so that a project's deployment whose name happens to match a
	// required-template name cannot adopt, delete, or otherwise collide
	// with required-template resources via the deployment reconcile/cleanup
	// label selector (`project=X,deployment=Y`).
	AnnotationRequiredTemplate = "console.holos.run/required-template"
	AnnotationURL               = "console.holos.run/url"
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
	// Superseded in v1alpha2 by AnnotationLinkedTemplates (which also carries
	// scope information and version constraints).
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

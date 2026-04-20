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
	ResourceTypeOrganization = "organization"
	ResourceTypeFolder       = "folder"
	ResourceTypeProject      = "project"
	ResourceTypeDeployment   = "deployment"
	// ResourceTypeTemplate is the unified v1alpha2 template resource type.
	// A single label value is used across every hierarchy level (ADR 021
	// Decision 4).
	ResourceTypeTemplate = "template"
	// ResourceTypeTemplatePolicy is the resource type label value for
	// TemplatePolicy ConfigMaps. TemplatePolicy objects bind REQUIRE/EXCLUDE
	// rules to templates and replace the removed `mandatory` flag on Template
	// and LinkableTemplate (HOL-554/HOL-555). Policy ConfigMaps live only in
	// organization or folder namespaces; project-scoped storage is forbidden
	// because a project owner could otherwise tamper with the very policy the
	// platform meant to constrain them with.
	ResourceTypeTemplatePolicy = "template-policy"
	// ResourceTypeTemplatePolicyBinding is the resource type label value for
	// TemplatePolicyBinding ConfigMaps. A TemplatePolicyBinding attaches a
	// single TemplatePolicy to an explicit list of project templates and/or
	// deployments, replacing the glob-based target selector on
	// TemplatePolicyRule (ADR 029, HOL-590). Like TemplatePolicy, bindings
	// live only in organization or folder namespaces; project-scope storage
	// is forbidden because a project owner could otherwise tamper with the
	// very binding the platform meant to constrain them with (HOL-554).
	ResourceTypeTemplatePolicyBinding = "template-policy-binding"
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
	AnnotationURL               = "console.holos.run/url"
	AnnotationEnabled           = "console.holos.run/enabled"
	AnnotationSettings          = "console.holos.run/project-settings"
	// AnnotationDefaultFolder stores the identifier (slug) of the default folder
	// for an organization. Written when the org is created and updatable via
	// UpdateOrganization. New projects without an explicit parent are placed in
	// this folder (ADR 022 Decision 3).
	AnnotationDefaultFolder = "console.holos.run/default-folder"
	// AnnotationGatewayNamespace stores the Kubernetes namespace that hosts
	// the platform Gateway referenced by templates rendered for an
	// organization. Lives on the organization namespace; surfaced to template
	// inputs via `platform.gatewayNamespace` and writable via
	// UpdateOrganization (HOL-526).
	AnnotationGatewayNamespace = "console.holos.run/gateway-namespace"
	// AnnotationParent is the Kubernetes namespace name of the immediate parent
	// (organization namespace or folder namespace). Added in v1alpha2 and
	// present on both Folder and Project namespaces. The hierarchy walk follows
	// this label upward to collect templates and resolve permissions (ADR 020
	// Decision 3 and Decision 6).
	AnnotationParent = "console.holos.run/parent"
	// AnnotationLinkedTemplates is the wire format used to bridge
	// Template CRDs to the deployment handler. Template CRD storage
	// keeps linked refs in a structured Spec field (HOL-621), but the
	// deployment handler still consumes deployment-template ConfigMaps
	// and reads the linked refs off this JSON-annotation. The
	// templateCRDToConfigMap adapter in console/templates writes the
	// annotation on the fly so deployments can keep its existing
	// reader. Once the deployments package is ported off ConfigMaps
	// (HOL-615 Phase 6 and friends), this constant and its adapter
	// both go away.
	// Example: [{"scope":"organization","scope_name":"acme","name":"microservice-v2"}]
	AnnotationLinkedTemplates = "console.holos.run/linked-templates"
	// LabelTemplateScope identifies the hierarchy level of a Template.
	// Values: "organization", "folder", "project" (ADR 021 Decision 4).
	// Still surfaced on CRD-stored Templates via the scheme label
	// setters in console/templates.
	LabelTemplateScope = "console.holos.run/template-scope"

	// AnnotationExternalLinkPrefix is the Holos-authored annotation-key
	// prefix for external links surfaced on a deployment. Links are keyed
	// by suffix (for example, `console.holos.run/external-link.logs`),
	// where the suffix serves as a stable per-deployment identity for
	// de-duplication across resources. The annotation value is a JSON
	// object of the form `{"url": "...", "title": "...", "description":
	// "..."}`; only `url` is required. Title falls back to the suffix when
	// omitted; description is optional. Parsed by `console/links` and
	// aggregated by `console/deployments` (the cached set is stored on the
	// deployment ConfigMap as AnnotationAggregatedLinks). See ADR 028 in
	// the cartographer repo for the design rationale and HOL-550 for the
	// parent plan.
	AnnotationExternalLinkPrefix = "console.holos.run/external-link."
	// AnnotationPrimaryURL names the single "primary" deployment URL a
	// template wants the UI to treat as canonical. May be attached to any
	// resource owned by the deployment (the aggregator picks the first
	// occurrence in scan order and warns on conflicts). The value is a
	// JSON object of the form `{"url": "...", "title": "...",
	// "description": "..."}`; only `url` is required. The promoted URL
	// fills `DeploymentOutput.url` so list-page renderers that only
	// consume the primary URL keep working. When no resource publishes a
	// primary-url annotation, `DeploymentOutput.url` is whatever the
	// template's `output.url` (cached as `console.holos.run/output-url` on
	// the deployment ConfigMap) holds. See ADR 028 in the cartographer
	// repo and HOL-550 for the parent plan.
	AnnotationPrimaryURL = "console.holos.run/primary-url"
	// AnnotationAggregatedLinks is the optional JSON cache of the fully
	// resolved Link list for a deployment, written onto the deployment
	// ConfigMap by the Create/UpdateDeployment handlers so list-view RPCs
	// (ListDeployments, GetDeploymentStatusSummary) can return links
	// without re-walking the workload annotations. The cached payload is
	// `{"links": [...], "primary_url": "..."}`. Treated as a cache, not
	// the source of truth: the authoritative links still live on the
	// resources themselves and the GetDeployment refresh path
	// re-aggregates and rewrites this annotation when it disagrees with
	// the live resources.
	AnnotationAggregatedLinks = "console.holos.run/links"
	// AnnotationArgoCDLinkPrefix is the annotation-key prefix Argo CD uses
	// to attach external links to Kubernetes resources. Values are bare
	// URL strings (no JSON envelope); the suffix doubles as the link
	// title. See
	// https://argo-cd.readthedocs.io/en/stable/user-guide/external-url/
	// for the upstream convention. Holos harvests these alongside its own
	// `console.holos.run/external-link.*` annotations on read; templates
	// MUST NOT write to this prefix from Holos because Argo CD would
	// render Holos's JSON-valued external-link annotations as broken
	// URLs.
	AnnotationArgoCDLinkPrefix = "link.argocd.argoproj.io/"

	// TemplateScopeOrganization is the LabelTemplateScope value for org-level templates.
	TemplateScopeOrganization = "organization"
	// TemplateScopeFolder is the LabelTemplateScope value for folder-level templates.
	TemplateScopeFolder = "folder"
	// TemplateScopeProject is the LabelTemplateScope value for project-level templates.
	TemplateScopeProject = "project"
)

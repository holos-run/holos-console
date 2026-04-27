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
	ManagedByValue           = "console.holos.run"
	ResourceTypeOrganization = "organization"
	ResourceTypeFolder       = "folder"
	ResourceTypeProject      = "project"
	ResourceTypeDeployment   = "deployment"
	// ResourceTypeTemplatePolicyBinding is the resource type label value for
	// TemplatePolicyBinding ConfigMaps. A TemplatePolicyBinding attaches a
	// single TemplatePolicy to an explicit list of project templates and/or
	// deployments, replacing the glob-based target selector on
	// TemplatePolicyRule (ADR 029, HOL-590). Like TemplatePolicy, bindings
	// live only in organization or folder namespaces; project-scope storage
	// is forbidden because a project owner could otherwise tamper with the
	// very binding the platform meant to constrain them with (HOL-554).
	ResourceTypeTemplatePolicyBinding = "template-policy-binding"
	// ResourceTypeTemplateRequirement is the resource type label value for
	// TemplateRequirement CRDs. Requirements live only in organization or
	// folder namespaces and gate matching project templates or deployments on
	// a prerequisite template.
	ResourceTypeTemplateRequirement = "template-requirement"
	// ResourceTypeTemplateGrant is the resource type label value for
	// TemplateGrant CRDs. Grants live only in organization or folder
	// namespaces and authorize cross-namespace template references, mirroring
	// the Gateway API ReferenceGrant pattern.
	ResourceTypeTemplateGrant = "template-grant"

	// Annotations.
	AnnotationDisplayName    = "console.holos.run/display-name"
	AnnotationDescription    = "console.holos.run/description"
	AnnotationCreatorEmail   = "console.holos.run/creator-email"
	AnnotationCreatorSubject = "console.holos.run/creator-sub"
	AnnotationShareUsers     = "console.holos.run/share-users"
	AnnotationShareRoles     = "console.holos.run/share-roles"
	AnnotationRBACShareUsers = "console.holos.run/rbac-share-users"
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

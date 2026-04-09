# Glossary

This glossary defines canonical terminology for the holos-console deployment
feature. Consult it whenever writing documentation, code comments, or prose that
mentions templates, resources, or the configuration management hierarchy.

## Naming History Note

ADR 013 introduced the concept of an organization-level CUE template and called
it a "system template." ADR 016 renamed the concept to "platform template" to
align with the resource collection names (`platformResources`) and the input
name (`PlatformInput`). Code identifiers (`SystemTemplate`, `SystemTemplateService`,
file paths such as `console/system_templates/`, and proto service names) were
not renamed because renaming code identifiers carries a higher migration cost and
the product is pre-release. In prose, always use the canonical term "platform
template." Reserve the identifier spelling (`SystemTemplate`) for references to
specific code artifacts.

---

## Terms

### Platform template (code: `SystemTemplate`, proto: `SystemTemplateService`)

An organization-level CUE template managed by platform engineers. Platform
templates are stored as Kubernetes ConfigMaps in the organization namespace and
unified with the deployment template at render time. They can produce resources
in `platformResources` (e.g., HTTPRoutes in the gateway namespace) and define
CUE constraints over `projectResources` (e.g., closing the struct to restrict
allowed resource kinds). Platform templates may be marked `mandatory` (applied
to project namespaces at creation time) and/or `enabled` (unified at deploy
time). Edit access requires `PERMISSION_SYSTEM_DEPLOYMENTS_EDIT`, granted only
to org-level OWNERs. See [ADR 013](adrs/013-separate-system-user-template-input.md)
and [ADR 016](adrs/016-config-management-resource-schema.md) for the design
rationale.

### Deployment template (code: `DeploymentTemplate`, proto: `DeploymentTemplateService`)

A project-level CUE template that defines the application resources for a
deployment. Deployment templates are stored as Kubernetes ConfigMaps in the
project namespace and written by product engineers. They produce resources in
`projectResources` (Deployments, Services, ServiceAccounts) and may carry
`DeploymentDefaults` (image, tag, port, etc.) that pre-fill the Create
Deployment form. At render time, the deployment template is unified with all
enabled platform templates for the organization.

### Platform resources (`platformResources`)

The collection of Kubernetes resources managed by platform and SRE engineers.
These resources typically live outside the project namespace — for example, an
`HTTPRoute` in the gateway namespace or a `ReferenceGrant` that allows
cross-namespace traffic. The renderer reads `platformResources` only from
templates at the organization or folder level; a project-level deployment
template that defines `platformResources` fields has no effect (this boundary is
enforced by the Go renderer, not by CUE). In CUE templates the field is
`platformResources`.

### Project resources (`projectResources`)

The collection of Kubernetes resources managed by product engineers, scoped to
the project namespace. Typical kinds include `Deployment`, `Service`, and
`ServiceAccount`. The renderer reads `projectResources` from both deployment
templates and platform templates. In CUE templates the field is
`projectResources`.

### Platform input (`PlatformInput`, CUE path: `platform`)

The trusted set of values injected by the backend before CUE evaluation. Because
these values come from the authenticated server context — not from the user's API
request — template authors can rely on them being correct and verified. Fields
include `project`, `namespace`, `gatewayNamespace`, `organization`, and `claims`
(OIDC ID token claims). In CUE templates, platform input is accessed via the
`platform` top-level identifier (e.g., `platform.namespace`,
`platform.claims.email`). Go type: `api/v1alpha1.PlatformInput`.

### Project input (`ProjectInput`, CUE path: `input`)

The user-supplied deployment parameters provided via the deployment creation
form or the template preview editor. Fields include `name`, `image`, `tag`,
`command`, `args`, `env`, and `port`. Template authors should treat these as
user-controlled values and apply appropriate CUE constraints. In CUE templates,
project input is accessed via the `input` top-level identifier (e.g.,
`input.name`, `input.port`). Go type: `api/v1alpha1.ProjectInput`.

### Resource set

The complete output produced by unifying all templates in the hierarchy with
their inputs. A resource set contains both `platformResources` and
`projectResources`. It is the artifact that the renderer validates and then
applies to Kubernetes via server-side apply. The Go type `api/v1alpha1.ResourceSet`
represents this concept; `ResourceSetSpec` groups the input and output sections.

### Template unification

The CUE operation that combines templates from every level of the hierarchy into
a single evaluated value. CUE unification is commutative, associative, and
idempotent — the order of template collection does not affect the result. Values
at any level may be concrete (e.g., `replicas: 3`), constraints (e.g.,
`replicas: >=2`), types, or top (`_`); CUE treats them all as plain values and
unifies them. A conflict (bottom, `_|_`) at any field causes evaluation to fail
with a CUE error before any Kubernetes API call. See
[ADR 016](adrs/016-config-management-resource-schema.md) Decision 8.

### Hierarchy levels

The organizational nesting within which templates are collected and unified at
render time:

- **Organization** — the top level. Platform templates at this level apply
  across all projects in the organization.
- **Folder** (planned, `v1alpha2`) — an optional intermediate level between
  Organization and Project. Up to three folder levels are supported. Useful for
  applying SRE standards to a subset of projects.
- **Project** — the leaf level. The deployment template lives here and is
  written by the product engineer who owns the application.

The renderer walks the hierarchy upward from the project, collects templates at
each level, and unifies them all. See
[ADR 016](adrs/016-config-management-resource-schema.md) Decision 4 and Decision 8.

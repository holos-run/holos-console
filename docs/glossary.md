# Glossary

This glossary defines canonical terminology for the holos-console deployment
feature. Consult it whenever writing documentation, code comments, or prose that
mentions templates, resources, or the configuration management hierarchy.

## Naming History Note

ADR 013 introduced the concept of an organization-level CUE template and called
it a "system template." ADR 016 renamed the concept to "platform template" to
align with the resource collection names (`platformResources`) and the input
name (`PlatformInput`). Code identifiers were subsequently renamed in phases
1–3 of issue #558: `SystemTemplate` → `OrgTemplate`, `SystemTemplateService` →
`OrgTemplateService`, `console/system_templates/` → `console/org_templates/`,
and `system_templates.proto` → `org_templates.proto`. In prose, always use the
canonical term "platform template."

---

## Terms

### Platform template (code: `OrgTemplate`, proto: `OrgTemplateService`)

An organization-level CUE template managed by platform engineers. Platform
templates are stored as Kubernetes ConfigMaps in the organization namespace and
unified with the deployment template at render time (subject to the explicit
linking formula — see [Linked platform template](#linked-platform-template) and
[Mandatory platform template](#mandatory-platform-template) below). They can
produce resources in `platformResources` (e.g., HTTPRoutes in the gateway
namespace) and define CUE constraints over `projectResources` (e.g., closing
the struct to restrict allowed resource kinds). Platform templates may be marked
`mandatory` (always applied at render time and at project namespace creation)
and/or `enabled` (available for linking and render-time unification). Edit
access requires `PERMISSION_ORG_TEMPLATES_WRITE`, granted only to org-level
OWNERs. See [ADR 013](adrs/013-separate-system-user-template-input.md),
[ADR 016](adrs/016-config-management-resource-schema.md), and
[ADR 019](adrs/019-explicit-template-linking.md) for the design rationale.

### Deployment template (code: `DeploymentTemplate`, proto: `DeploymentTemplateService`)

A project-level CUE template that defines the application resources for a
deployment. Deployment templates are stored as Kubernetes ConfigMaps in the
project namespace and written by product engineers. They produce resources in
`projectResources` (Deployments, Services, ServiceAccounts) and may carry
`DeploymentDefaults` (image, tag, port, etc.) that pre-fill the Create
Deployment form. At render time, the deployment template is unified with the
applicable platform templates — always including mandatory platform templates,
plus any enabled platform templates explicitly linked via the
`console.holos.run/linked-org-templates` annotation (see
[Linked platform template](#linked-platform-template)).

### Linked platform template

An enabled org-level platform template that a deployment template has explicitly
opted into by including its name in the `console.holos.run/linked-org-templates`
annotation on the deployment template ConfigMap. Linked non-mandatory templates
are included in the render set for that deployment template. The product engineer
selects linked templates using the "Linked Platform Templates" section in the
deployment template editor. See [ADR 019](adrs/019-explicit-template-linking.md).

### Mandatory platform template

An org-level platform template with `mandatory = true`. Mandatory templates are
applied to every project namespace at creation time AND are always unified with
every deployment template at render time, regardless of whether the deployment
template links them. Platform engineers set the `mandatory` flag when a policy
must apply uniformly across all deployments in the organization with no opt-out.
Mandatory templates appear pre-checked with a lock icon in the deployment
template editor. See [ADR 019](adrs/019-explicit-template-linking.md).

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

### Folder

An optional intermediate grouping level in the organization hierarchy, sitting
between an Organization and a Project (or between two Folder levels). Stored as
a Kubernetes Namespace with `console.holos.run/resource-type: folder` and a
`console.holos.run/parent` label pointing to the immediate parent namespace (an
organization or another folder). Up to three folder levels are supported between
any Organization and Project (ADR 016 Decision 4). Introduced in `v1alpha2`; not
present in `v1alpha1`. See [ADR 020](adrs/020-v1alpha2-folder-hierarchy.md).

### Hierarchy levels

The organizational nesting within which templates are collected and unified at
render time:

- **Organization** — the top level. Platform templates at this level apply
  across all projects in the organization.
- **Folder** (`v1alpha2`) — an optional intermediate level between Organization
  and Project. Up to three folder levels are supported. Useful for applying SRE
  standards to a subset of projects. See [ADR 020](adrs/020-v1alpha2-folder-hierarchy.md).
- **Project** — the leaf level. The deployment template lives here and is
  written by the product engineer who owns the application.

The renderer walks the hierarchy upward from the project, collects templates at
each level, and unifies them all. See
[ADR 016](adrs/016-config-management-resource-schema.md) Decision 4 and Decision 8.

### Hierarchy walk

The algorithm that traverses the Organization → Folder(s) → Project chain
upward from a given namespace to collect templates or resolve effective
permissions. The walk follows `console.holos.run/parent` labels, terminating at
the Organization namespace. Bounded to 5 levels (1 org + up to 3 folders + 1
project); results are cached per-request via a `context.Context`-attached
`sync.Map`. See [ADR 020](adrs/020-v1alpha2-folder-hierarchy.md) Decision 6 and
Decision 7.

### Default-share cascade

The mechanism by which a new resource (e.g., Secret) inherits share grants from
its ancestor chain at creation time. Each ancestor's
`console.holos.run/default-share-*` annotations are merged into the new
resource's initial share state (highest role wins per principal). Runtime access
to an existing resource requires explicit grants; the default-share cascade
applies only at the moment of resource creation. This extends the non-cascading
access model from [ADR 007](adrs/007-org-grants-no-cascade.md) with creation-time
default propagation introduced in `v1alpha2`. See
[ADR 020](adrs/020-v1alpha2-folder-hierarchy.md) Decision 9.

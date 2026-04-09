# ADR 017: Configuration Management RBAC Levels

## Status

Accepted — revokes [ADR 015](015-config-management-rbac-levels.md)

## CUE Unification: No Distinction Between "Write" and "Constrain"

This section corrects a fundamental misconception in ADR 015.

ADR 015 distinguished between "writing to" and "constraining" a resource
collection. For example, it stated that folder and organization templates "can
constrain `projectResources`" but "cannot write to `projectResources`." **This
distinction does not exist in CUE.**

In CUE, types, constraints, and concrete values are all **plain old values**
that get **unified** together. There is no separate "constrain" operation
distinct from a "write" operation. When a folder-level template says
`projectResources: namespacedResources: Deployment: "my-app": spec: replicas: >=2`
and a project-level template says
`projectResources: namespacedResources: Deployment: "my-app": spec: replicas: 3`,
CUE unifies these two values. The result is `3` because `3` satisfies `>=2`.
Both templates wrote to the same field — one wrote a constraint, the other wrote
a concrete value. CUE does not care which is which; it unifies them.

This means the correct model is simpler than ADR 015 described:

- **Every template at every level can define values for both
  `projectResources` and `platformResources`.** Those values may be concrete
  (`replicas: 3`), constraints (`replicas: >=2`), types (`replicas: int`),
  top (`_`), or bottom (`_|_`). CUE unifies all of them.

- **The renderer controls which collections it reads from which level.** This
  is the real access boundary — not a CUE-level distinction between writing
  and constraining, but a Go-level decision about which fields the renderer
  extracts from which template source.

The rest of this ADR preserves the RBAC decisions from ADR 015, with the
resource collection scope table (Decision 3) corrected to reflect this reality.

## Context

The console's existing RBAC model (ADR 007, `console/rbac/rbac.go`) authorizes
RPC operations: who can create a deployment, who can read a secret, who can
toggle a project setting. This model uses three roles (Viewer, Editor, Owner),
annotation-based grants on Kubernetes Namespace and Secret objects, and cascade
tables that map parent-level roles to child-level permissions.

ADR 016 defines a configuration management hierarchy (Organization ->
Folder(s) -> Project) where templates at each level contribute to different
resource collections (`platformResources` vs. `projectResources`). This
hierarchy needs its own RBAC model that determines:

1. Who can author or modify templates at each level.
2. Which resource collections the renderer reads from a template at a given level.
3. How permissions cascade through the hierarchy.

The RPC authorization model and the configuration RBAC model are related but
distinct. RPC authorization controls API access ("can this user call
`CreateDeployment`?"). Configuration RBAC controls template scope ("which
collections does the renderer read from a template at this level?"). Both
models must align: a user who lacks RPC authorization to modify a folder should
also be unable to modify templates stored in that folder.

### Audience

This ADR is written for three audiences:

- **Product engineers** who write deployment templates for their applications
  and need to understand what they can and cannot do.
- **Site reliability engineers (SREs)** who write templates at folder levels to
  enforce operational standards and need to understand how their values
  interact with project templates via CUE unification.
- **Platform engineers** who write templates at the organization level to
  enforce platform-wide policy and need to understand the full scope model.

Kubernetes expertise is not assumed. Where Kubernetes concepts are referenced,
they are explained in terms of what they mean for the template author.

For a visual overview of how the hierarchy, templates, inputs, and resource
collections fit together, see the
[Resource Model diagram](014-resource-model.svg) (originally from ADR 014, now maintained alongside ADR 016 as the active schema reference).

## Decisions

### 1. Three roles, four levels.

The existing role set (Viewer, Editor, Owner) is reused. These roles are granted
at four levels in the hierarchy:

| Level        | What it represents | Example |
|--------------|-------------------|---------|
| Organization | A company, team, or tenant | `acme-corp` |
| Folder       | A grouping of projects (optional, up to 3 deep) | `payments`, `payments/eu` |
| Project      | A single application or service | `checkout-api` |
| Resource     | A specific object within a project (secret, deployment) | `db-password` |

Each level is a Kubernetes Namespace distinguished by a
`console.holos.run/resource-type` label (`organization`, `folder`, or
`project`). Grants are stored as JSON annotations on the Namespace object, using
the same `console.holos.run/share-users` and `console.holos.run/share-roles`
annotation keys used today.

### 2. Role meanings are consistent across all levels.

| Role   | What it means at any level |
|--------|---------------------------|
| Viewer | Read templates and view rendered output. Cannot create, modify, or delete templates. |
| Editor | Everything a Viewer can do, plus create and modify templates. Cannot delete templates or manage who has access. |
| Owner  | Everything an Editor can do, plus delete templates and manage access grants (IAM) for the level. |

A user's effective role at a given level is the highest role from any of these
sources, evaluated in order:

1. Direct grant on the resource at that level.
2. Grant inherited from a parent level via cascade (see Decision 4).

This matches the existing RBAC evaluation pattern in `console/rbac/rbac.go`.

### 3. The renderer determines which resource collections it reads from each template level.

**Note:** ADR 017 is silent on *which specific ancestor templates* participate
in unification at render time. That decision (explicit linking, Model B) is
documented in [ADR 019: Explicit Platform Template Linking](019-explicit-template-linking.md).

Every template at every level in the hierarchy can define values for both
`projectResources` and `platformResources`. In CUE, all values — concrete
data, constraints, types, top, bottom — are unified together. There is no
language-level distinction between "defining" and "constraining" a field.

The access boundary is enforced by the **Go renderer**, which decides which
fields to read from templates at each level:

| Template defined at | Renderer reads `projectResources` | Renderer reads `platformResources` |
|--------------------|-----------------------------------|-------------------------------------|
| Project            | Yes | No  |
| Folder             | Yes | Yes |
| Organization       | Yes | Yes |

**What this means in practice:**

- A **product engineer** writes a template in their project. That template
  defines the Deployment, Service, and ServiceAccount that run their app. These
  resources go into `projectResources`. The renderer does not read
  `platformResources` from this template, so any values the project template
  defines there have no effect.

- An **SRE** writes a template in a folder that covers several projects. That
  template can define values for both collections. It might add an HTTPRoute
  to `platformResources` so that every project under the folder gets external
  traffic routing. It might also define values in `projectResources` — for
  example, requiring every Deployment to have a `resources.limits.memory` field
  by providing a CUE constraint like `memory: string`. Because CUE unifies all
  values, the project template's concrete value (e.g. `memory: "512Mi"`) and
  the folder template's constraint (`memory: string`) are unified into
  `memory: "512Mi"`. If the project template omits the field, CUE evaluation
  produces an incomplete value, which the renderer catches as an error.

- A **platform engineer** writes a template at the organization level. That
  template can define values for both collections, exactly like a folder
  template. It might add a NetworkPolicy to `platformResources` that applies
  to every project in the organization, and define constraints in
  `projectResources` to require a specific label on every resource.

**Enforcement**: The Go renderer reads `platformResources` only from templates
at the folder and organization levels. It reads `projectResources` from all
levels. When a project template defines fields under `platformResources`, they
are ignored by the renderer. This is a hard boundary enforced in Go code — the
renderer simply does not read the field from project-level templates.

**Values flow downward via unification**: Organization and folder templates
define values for `projectResources` that get unified with the project
template's values. This is standard CUE unification — the same mechanism used
everywhere else in the language. For example, a platform template can close the
`projectResources` struct to restrict which resource Kinds a project template
may produce — if a project template tries to add a `ClusterRoleBinding`, CUE
evaluation fails before any Kubernetes API call (see ADR 016, Decision 9 for
the full mechanism and examples). Platform templates can also require labels,
set minimum replica counts, or define any other value that project templates
must satisfy through unification.

Values cannot flow upward: a project template cannot affect `platformResources`
because the renderer does not read that field from project-level templates.

### 4. Permissions cascade downward through the hierarchy, highest role wins.

When checking whether a user can perform an action at a given level, the system
evaluates grants starting from the target level and walking up to the
organization:

```
Organization grant  -->  highest role wins
  Folder-1 grant    -->  at each level,
    Folder-2 grant  -->  check direct grants
      Project grant  -->  (share-users, share-roles)
```

The effective role is the maximum of all grants found during the walk. This
means:

- An **Organization Owner** has Owner access to every folder and project in the
  organization. They do not need separate grants at each level.

- A **Folder Editor** has Editor access to every project under that folder (and
  its sub-folders). They can create and modify templates at those levels.

- A **Project Viewer** can only view templates in that specific project.

This is a change from ADR 007, which stated that organization grants do not
cascade. ADR 007 was correct for the RPC authorization model at that time —
org-level OWNER should not automatically read secrets in projects. For
configuration management, cascade is the right default because template policy
is inherently hierarchical: an organization owner who sets platform-wide policy
must be able to see and modify templates at every level beneath them.

**The RPC authorization model and configuration RBAC cascade independently.**
An org-level Owner has cascading access to *templates* at all levels but does
not automatically gain access to *secrets* or *deployment logs* in projects.
The existing non-cascading RPC authorization (ADR 007) is preserved for
resource-level operations. Configuration RBAC adds a parallel cascade path for
template operations only.

### 5. Template authoring permissions use the cascade table pattern.

New permissions and cascade tables for template operations at each level:

```go
// Template authoring permissions.
const (
    // PermissionTemplatesList allows listing templates at a given level.
    PermissionTemplatesList Permission = ...
    // PermissionTemplatesRead allows reading template source at a given level.
    PermissionTemplatesRead Permission = ...
    // PermissionTemplatesWrite allows creating and modifying templates.
    PermissionTemplatesWrite Permission = ...
    // PermissionTemplatesDelete allows deleting templates.
    PermissionTemplatesDelete Permission = ...
    // PermissionTemplatesAdmin allows managing access grants on templates.
    PermissionTemplatesAdmin Permission = ...
)
```

Cascade tables define what each role grants for template operations at child
levels:

```go
// OrgCascadeTemplatePerms defines template permissions granted by org roles
// to all folders and projects beneath the organization.
var OrgCascadeTemplatePerms = CascadeTable{
    RoleViewer: {
        PermissionTemplatesList: true,
        PermissionTemplatesRead: true,
    },
    RoleEditor: {
        PermissionTemplatesList:  true,
        PermissionTemplatesRead:  true,
        PermissionTemplatesWrite: true,
    },
    RoleOwner: {
        PermissionTemplatesList:   true,
        PermissionTemplatesRead:   true,
        PermissionTemplatesWrite:  true,
        PermissionTemplatesDelete: true,
        PermissionTemplatesAdmin:  true,
    },
}

// FolderCascadeTemplatePerms defines template permissions granted by folder
// roles to sub-folders and projects beneath the folder.
var FolderCascadeTemplatePerms = CascadeTable{
    RoleViewer: {
        PermissionTemplatesList: true,
        PermissionTemplatesRead: true,
    },
    RoleEditor: {
        PermissionTemplatesList:  true,
        PermissionTemplatesRead:  true,
        PermissionTemplatesWrite: true,
    },
    RoleOwner: {
        PermissionTemplatesList:   true,
        PermissionTemplatesRead:   true,
        PermissionTemplatesWrite:  true,
        PermissionTemplatesDelete: true,
        PermissionTemplatesAdmin:  true,
    },
}
```

These tables are structurally identical today. They are defined separately so
that they can diverge independently — for example, a future policy might restrict
folder-level Editor from deleting templates while allowing org-level Editor to
do so.

### 6. Resource collection scope is enforced by the renderer, not by RBAC.

RBAC controls *who* can author templates at each level. The *renderer* controls
*what* resource collections it reads from a template at a given level. These are
separate enforcement points:

- **RBAC check** (at RPC time): "Does this user have `PermissionTemplatesWrite`
  at the folder level?" If no, the RPC returns PermissionDenied.

- **Renderer check** (at evaluation time): "This template is stored at the
  project level, so I read `projectResources` from it but not
  `platformResources`."

This separation means a product engineer who has Editor on a project cannot
affect platform resources by defining `platformResources` values in their
project-level template — the renderer simply does not read that field from
project-level sources. The boundary is enforced in Go code, not in CUE.

### 7. Authorization evaluation walks the hierarchy once per request.

The authorization check for a template operation proceeds as follows:

```
1. Identify the target level (org, folder, or project) from the request.
2. Load the Namespace object for the target level.
3. Read share-users and share-roles annotations.
4. Compute the user's best role from direct grants.
5. Walk up to the parent level (via console.holos.run/parent label).
6. At each parent, read grants and update the best role if higher.
7. Stop at the organization level.
8. Check the best role against the cascade table for the required permission.
```

This walk is bounded by the hierarchy depth (organization + up to 3 folders +
project = 5 levels maximum). Each level requires one Kubernetes API call to
read the Namespace object. The walk is cached per-request to avoid redundant
API calls when multiple permission checks are needed in a single RPC handler.

### 8. The existing `PermissionOrgTemplatesWrite` maps to org-level template authoring.

The current `PermissionOrgTemplatesWrite` permission (used for platform
templates) is the org-level grant for `platformResources` authoring. During
migration from the earlier `PermissionSystemDeploymentsEdit` name:

- `OrgTemplate` objects are organization-level templates that write to
  `platformResources`.
- `PermissionSystemDeploymentsEdit` was replaced by
  `PermissionOrgTemplatesWrite` checked at the organization level.
- The `OrgCascadeSystemTemplatePerms` table was renamed to
  `OrgCascadeTemplatePerms`.

### 9. Alignment between RPC authorization and configuration RBAC.

The RPC authorization model and configuration RBAC model must produce consistent
results. The principle is:

> If a user cannot author a template at level L via configuration RBAC, then no
> RPC should accept a template modification at level L from that user.

This is enforced by having RPC handlers call the same cascade-walk authorization
function used by configuration RBAC before accepting template mutations. The
RPC handler checks `PermissionTemplatesWrite` (or `Delete`, `Admin`) at the
appropriate level using the hierarchy walk described in Decision 7.

Conversely, read-only RPCs (listing templates, rendering previews) check
`PermissionTemplatesRead` or `PermissionTemplatesList`, which cascade from
Viewer and above.

## Consequences

### Positive

- **Clear separation of concerns.** RBAC controls who can author templates;
  the renderer controls what collections it reads from each level. Neither
  system needs to know the details of the other.

- **Correct CUE mental model.** By not distinguishing between "write" and
  "constrain," this ADR accurately reflects how CUE unification works. Template
  authors at all levels define values; CUE unifies them. The renderer decides
  which fields to extract from which levels.

- **Intuitive for template authors.** A product engineer knows: "I write
  templates in my project, the renderer reads my project resources." An SRE
  knows: "I write templates in my folder, the renderer reads both my platform
  resources and my project resource values (which get unified with the
  project's own values)." No Kubernetes expertise is needed to understand this
  boundary.

- **Hierarchical policy.** Organization-level templates apply everywhere.
  Folder-level templates apply to a subtree. Project-level templates apply to
  one project. This mirrors how organizations actually work: platform policy is
  global, team policy covers a product area, application config is per-service.

- **Cascade tables are explicit.** Every permission grant is visible in a
  Go map literal. There is no implicit inheritance or hidden logic. A reviewer
  can read the cascade table and know exactly what each role grants.

- **Compatible with existing model.** The new permissions and cascade tables
  extend the existing RBAC package without modifying the behavior of existing
  permissions. RPC authorization for secrets, deployments, and project settings
  is unchanged.

### Negative

- **Hierarchy walk cost.** Each template authorization check requires reading
  up to 5 Namespace objects. Mitigated by per-request caching and the fact that
  Kubernetes API calls to read Namespaces are fast (in-memory etcd reads).

- **Two cascade paths.** RPC authorization (ADR 007: no cascade from org to
  secrets) and configuration RBAC (this ADR: cascade from org through folders
  to project templates) use different cascade rules. This is intentional but
  requires clear documentation to avoid confusion. The key distinction: RPC
  authorization protects *data* (secrets, logs); configuration RBAC protects
  *policy* (templates). Data access is need-to-know; policy access is
  hierarchical.

- **Folder implementation deferred.** `v1alpha1` does not implement folders.
  The RBAC model is designed for them, but the actual folder-level authorization
  code is deferred to `v1alpha2`. Until then, the hierarchy is Organization ->
  Project (two levels), and the cascade walk is trivially short.

### Risks

- **Unification conflicts.** Two templates in the same hierarchy path could
  define conflicting values for the same field in `projectResources`. CUE
  unification detects this as an evaluation error, which is the correct
  behavior — but the error message may be confusing to a product engineer whose
  template was valid before a new folder-level value was added. Mitigated by
  the `RenderDeploymentTemplate` preview RPC, which evaluates the full
  template stack and reports errors before deployment.

- **Over-constraining.** A platform engineer who defines overly strict values
  at the organization level can break every project's template. This is
  inherent to hierarchical policy and is the same risk as a Kubernetes admission
  webhook that is too restrictive. Mitigated by requiring Owner role for
  template authoring at the organization level, and by the preview RPC which
  lets the platform engineer test values against existing project templates
  before committing.

- **Cascade scope creep.** The decision to cascade template permissions (unlike
  ADR 007's decision not to cascade resource permissions) could set a precedent
  for cascading other permissions in the future. Each new cascade path should be
  evaluated independently — the argument for cascading template permissions
  (policy is hierarchical) does not apply to data access (secrets are need-to-know).

## References

- [ADR 007: Organization Grants Do Not Cascade](007-org-grants-no-cascade.md)
- [ADR 012: Structured Resource Output for CUE Templates](012-structured-resource-output.md)
- [ADR 013: Separate System and User Input Trust Boundary](013-separate-system-user-template-input.md)
- [ADR 014: Configuration Management Resource Schema (revoked)](014-config-management-resource-schema.md)
- [ADR 015: Configuration Management RBAC Levels (revoked)](015-config-management-rbac-levels.md)
- [ADR 016: Configuration Management Resource Schema](016-config-management-resource-schema.md)
- [ADR 019: Explicit Platform Template Linking](019-explicit-template-linking.md) — which ancestor templates participate in unification
- [Permissions Guide](../permissions-guide.md) — cascade table pattern and naming conventions

# ADR 021: Unified Template Service and Collapsed Template Permissions

## Status

Accepted

## Context

In `v1alpha1` there are two separate template services:

- `DeploymentTemplateService` (`console/templates/`, `proto/.../deployment_templates.proto`)
  — manages project-level CUE templates written by product engineers.
- `OrgTemplateService` (`console/org_templates/`, `proto/.../org_templates.proto`)
  — manages organization-level CUE templates (platform templates) written by
  platform engineers.

These services have substantially duplicated CRUD code (Create, Get, Update,
Delete, List handlers with the same Kubernetes ConfigMap backend, same grant
annotation parsing, same CUE source validation). They also have disjoint
permission sets:

- `PERMISSION_DEPLOYMENT_TEMPLATES_{LIST,READ,WRITE,DELETE,ADMIN}` — checked at
  the project level.
- `PERMISSION_ORG_TEMPLATES_WRITE` — a single permission checked at the org level;
  no separate List, Read, Delete, or Admin permissions for org-level templates.

ADR 017 Decision 5 sketched a single `PermissionTemplates*` set that would
collapse these two into one. This ADR pins down the API shape, the migration, and
how the unified service extends to folder-level templates introduced by ADR 020.

`v1alpha2` replaces `v1alpha1`. There are no compatibility shims. This ADR
specifies the exact replacements so that an implementer can proceed without
rediscovering design decisions.

### Design Goals

1. **Eliminate CRUD duplication** — a single `TemplateService` with a scope
   discriminator handles all template levels.
2. **Uniform permission set** — five permissions apply uniformly at every level,
   with level-specific cascade tables that define what each role grants.
3. **Extend ADR 019 to cross-level linking** — project templates can link to
   folder and org templates; folder templates can link to their ancestors.
4. **Preserve ADR 017 Decision 3 resource-collection semantics** — the renderer's
   collection-read policy (`projectResources` from project level only;
   `platformResources` from folder and org) is unchanged.

## Decisions

### 1. Single TemplateService with TemplateScope discriminator.

Replace `DeploymentTemplateService` and `OrgTemplateService` with a single
`TemplateService`. The `TemplateScope` enum identifies the level at which a
template is stored and authored:

```protobuf
// TemplateScope identifies the hierarchy level at which a template is stored.
enum TemplateScope {
    TEMPLATE_SCOPE_UNSPECIFIED  = 0;
    TEMPLATE_SCOPE_ORGANIZATION = 1;  // authored by platform engineers
    TEMPLATE_SCOPE_FOLDER       = 2;  // authored by SREs
    TEMPLATE_SCOPE_PROJECT      = 3;  // authored by product engineers
}
```

Every template RPC carries the scope plus the scope's name (org name, folder
name, or project name) so the handler can locate the owning Kubernetes Namespace.

```protobuf
// TemplateScopeRef identifies the owning scope of a template.
message TemplateScopeRef {
    TemplateScope scope      = 1;
    // scope_name is the org name, folder name, or project name.
    string        scope_name = 2;
}

message CreateTemplateRequest {
    TemplateScopeRef scope    = 1;
    Template         template = 2;
}
message GetTemplateRequest {
    TemplateScopeRef scope = 1;
    string           name  = 2;
}
message UpdateTemplateRequest {
    TemplateScopeRef scope    = 1;
    Template         template = 2;
}
message DeleteTemplateRequest {
    TemplateScopeRef scope = 1;
    string           name  = 2;
}
message ListTemplatesRequest {
    TemplateScopeRef scope = 1;
}
message ListLinkableTemplatesRequest {
    // scope is the deployment's scope (usually SCOPE_PROJECT). The handler
    // walks up the hierarchy and returns all enabled templates in ancestor scopes.
    TemplateScopeRef scope = 1;
}
```

The `TemplateService` proto replaces both `deployment_templates.proto` and
`org_templates.proto`. The new file is `proto/holos/console/v1/templates.proto`.

**Rationale**: One service with a discriminator eliminates duplicated CRUD code
while keeping the same Kubernetes storage pattern (ConfigMap in the owning
namespace) and the same CUE evaluation logic. A per-scope service was considered
(three services: `OrgTemplateService`, `FolderTemplateService`,
`ProjectTemplateService`) but rejected because it triples the CRUD duplication and
adds no expressiveness.

### 2. Collapsed permission set.

Five permissions, applied uniformly at every scope level:

```go
const (
    PermissionTemplatesList   Permission = ...  // list template names
    PermissionTemplatesRead   Permission = ...  // read template CUE source
    PermissionTemplatesWrite  Permission = ...  // create and update templates
    PermissionTemplatesDelete Permission = ...  // delete templates
    PermissionTemplatesAdmin  Permission = ...  // manage template grants (IAM)
)
```

These replace, in full:

| Old permission | Replacement |
|---|---|
| `PERMISSION_DEPLOYMENT_TEMPLATES_LIST` | `PermissionTemplatesList` |
| `PERMISSION_DEPLOYMENT_TEMPLATES_READ` | `PermissionTemplatesRead` |
| `PERMISSION_DEPLOYMENT_TEMPLATES_WRITE` | `PermissionTemplatesWrite` |
| `PERMISSION_DEPLOYMENT_TEMPLATES_DELETE` | `PermissionTemplatesDelete` |
| `PERMISSION_DEPLOYMENT_TEMPLATES_ADMIN` | `PermissionTemplatesAdmin` |
| `PERMISSION_ORG_TEMPLATES_WRITE` | `PermissionTemplatesWrite` (at org scope) |

`PERMISSION_ORG_TEMPLATES_WRITE` had no corresponding List, Read, Delete, or
Admin permissions. `v1alpha2` fills all five slots uniformly at every scope.

### 3. Single unified cascade table applied at every scope level.

A single `TemplateCascadePerms` table defines what each role grants for template
operations. The same table is used at every scope level (organization, folder,
project) — the cascade scope is determined by `ancestor.ResourceType` in the
walker, not by which table is selected. This collapses the three formerly-separate
tables (`OrgCascadeTemplatePerms`, `FolderCascadeTemplatePerms`,
`ProjectCascadeTemplatePerms`) into one.

```go
// TemplateCascadePerms defines template permissions uniformly at every scope
// level (organization, folder, project) per ADR 021 Decision 2.
var TemplateCascadePerms = CascadeTable{
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

**Permission cascade merger**: When checking a permission at a target scope, the
authorization walk (ADR 020 Decision 6) collects the user's best role at each
ancestor level and applies the appropriate cascade table. The effective permission
set is the **union** across all ancestors (highest role wins per ADR 017 Decision
4). An org-level Owner automatically satisfies `PermissionTemplatesWrite` at any
folder or project beneath the org; a folder-level Editor satisfies
`PermissionTemplatesWrite` at all projects beneath the folder.

### 4. Template ConfigMap storage.

Each template is stored as one Kubernetes ConfigMap in the namespace of its
owning scope:

| Template scope | Stored in namespace |
|---|---|
| `TEMPLATE_SCOPE_ORGANIZATION` | Organization namespace (`holos-org-<name>`) |
| `TEMPLATE_SCOPE_FOLDER` | Folder namespace (`holos-fld-<hash>-<slug>`) |
| `TEMPLATE_SCOPE_PROJECT` | Project namespace (`holos-prj-<name>`) |

Labels on the ConfigMap:

```yaml
console.holos.run/resource-type: template
console.holos.run/template-scope: organization   # or folder or project
console.holos.run/template-name: <name>
```

The `template-scope` label matches the string value of the `TemplateScope` enum
minus the `TEMPLATE_SCOPE_` prefix, lowercased. This label enables listing all
templates in a namespace regardless of how the namespace is used.

The mandatory and enabled flags continue to live as annotations, the same as in
`v1alpha1`:

```yaml
console.holos.run/mandatory: "true"   # always unified; applied at project creation
console.holos.run/enabled:   "true"   # available for linking
```

### 5. Extended explicit linking model (cross-level references).

ADR 019 defined explicit linking for Organization → Project only. In `v1alpha2`,
the linking model extends to all ancestor levels:

- A **project-level template** can link to templates in any ancestor scope
  (folder or org).
- A **folder-level template** can link to templates in its own ancestors (parent
  folders or org).

**Linked reference format**: The `console.holos.run/linked-templates` annotation
replaces `console.holos.run/linked-org-templates`. Its value is a JSON array of
scope-qualified template references:

```json
[
  {"scope": "organization", "scope_name": "acme", "name": "microservice-v2"},
  {"scope": "folder",       "scope_name": "payments", "name": "payments-sre-policy"}
]
```

Each element carries:
- `scope` — the string value of `TemplateScope` (minus prefix, lowercase)
- `scope_name` — the org/folder/project name for that scope
- `name` — the template name

The `console.holos.run/linked-org-templates` annotation from `v1alpha1` is
deleted; `v1alpha2` reads only `console.holos.run/linked-templates`.

**Render set formula (extended from ADR 019)**:

```
effective_set =
    (mandatory=true AND enabled=true, any ancestor level)
    UNION
    (enabled=true AND reference IN linked_list)
```

This extends the ADR 019 formula to all ancestor levels. Mandatory templates at
any ancestor (org or folder) are always included. Explicitly linked templates are
included when enabled, regardless of which ancestor level they live at.

### 6. Rendering walk.

The renderer walks **project → folders → org** at evaluation time, collecting the
effective template set:

```
1. Load the deployment template ConfigMap from the project namespace.
2. Read the console.holos.run/linked-templates annotation (JSON array of TemplateRef).
3. Walk the hierarchy upward (ADR 020 Decision 6):
   For each ancestor namespace (folder or org):
     a. Fetch all ConfigMaps with label console.holos.run/resource-type=template.
     b. Include any with mandatory=true AND enabled=true.
     c. Include any with enabled=true AND (scope + scope_name + name) IN linked_list.
4. Deduplicate by (scope, scope_name, name).
5. Assemble CUE source: prepend generated schema, concatenate all sources.
6. Evaluate via cue.Context.
7. Read projectResources from project template + folder/org templates.
8. Read platformResources from folder and org templates only (ADR 017 Decision 3).
```

The renderer calls a new
`TemplateProvider.ListTemplateSourcesForRender(ctx, projectNs, linkedRefs)`
method that encapsulates the walk and deduplication. It replaces the
`v1alpha1` `OrgTemplateProvider.ListOrgTemplateSourcesForRender`.

### 7. ListLinkableTemplates RPC.

A new `ListLinkableTemplates(scope, scope_name)` RPC returns all enabled
templates in the **ancestor chain** of the given scope:

- For a project scope: returns enabled templates from all parent folders and the
  organization.
- For a folder scope: returns enabled templates from all parent folders and the
  organization above it.

Each response item includes:
- The `TemplateScopeRef` (scope + scope_name)
- The template name and display name
- The `mandatory` flag (so the UI can render mandatory templates as always-on)

This RPC replaces `v1alpha1`'s `ListLinkableOrgTemplates`.

### 8. Mandatory template application at project creation.

At `CreateProject` time, the server applies mandatory+enabled templates from the
**full ancestor chain** (all parent folders + the organization). In `v1alpha1`,
only org-level mandatory templates were applied. In `v1alpha2`, a mandatory
folder-level template is also applied at project creation — matching the render-set
formula: if it is mandatory and enabled it always participates.

The `MandatoryTemplateApplier` is updated to walk the hierarchy and collect
mandatory+enabled templates from all ancestors.

### 9. Legacy cleanup (files to delete in v1alpha2).

When implementing `v1alpha2`, delete the following:

```
proto/holos/console/v1/deployment_templates.proto
proto/holos/console/v1/org_templates.proto
console/templates/              (replaced by console/templates/ — new unified package)
console/org_templates/
```

Replace with:

```
proto/holos/console/v1/templates.proto          (new TemplateService proto)
console/templates/                               (new unified handler + k8s + defaults + apply)
```

Note: The package path `console/templates/` is reused for the new unified
package because it most naturally describes the concept. The old project-level
`console/templates/` package is deleted and the new `console/templates/` package
is created with a clean implementation.

### 10. AGENTS.md updates.

The following sections of `AGENTS.md` must be updated when implementing
`v1alpha2`:

- The package structure listing under `console/` must replace the
  `templates/` and `org_templates/` entries with a single `templates/` entry
  describing the unified `TemplateService`.
- The Terminology section must remove the `OrgTemplate` code identifier and
  replace it with `Template` (scoped), noting the scope discriminator.
- The Template Linking Guardrail must be updated to reference
  `console.holos.run/linked-templates` (the new annotation) instead of
  `console.holos.run/linked-org-templates`.
- The terminology rule table must update the code identifier column from
  `OrgTemplateService` to `TemplateService`.

These updates are part of the `v1alpha2` implementation PR, not this ADR.

### 11. Cross-reference updates.

**ADR 016** references: Append after Decision 12 (Package layout):

> **v1alpha2**: `api/v1alpha2/` adds the `Folder` type and extends
> `PlatformInput` with folder ancestry. See [ADR 020](020-v1alpha2-folder-hierarchy.md).
> The unified `TemplateService` replaces `DeploymentTemplateService` and
> `OrgTemplateService`. See [ADR 021](021-unified-template-service.md).

**ADR 017** references: Append after Decision 5 (Template authoring permissions):

> **v1alpha2 implementation**: A single `TemplateCascadePerms` table defined in
> `console/rbac/rbac.go` replaces the three former per-scope tables. See
> [ADR 021](021-unified-template-service.md) for the collapsed permission set.

**ADR 019** references: Append after "Deferred" section:

> **v1alpha2 extension**: The explicit linking model is extended to cross-level
> references (project → folder → org). The `console.holos.run/linked-org-templates`
> annotation is replaced by `console.holos.run/linked-templates` with a JSON
> object array carrying `{scope, scope_name, name}`. See
> [ADR 021](021-unified-template-service.md) Decision 5.

## Glossary additions

The following term should be added to `docs/glossary.md`:

### Template scope

The hierarchy level at which a template is stored and authored: `organization`,
`folder`, or `project`. Encoded as the `TemplateScope` enum in the unified
`TemplateService` proto. The scope determines which resource collections the
renderer reads from the template (ADR 017 Decision 3) and which cascade table
governs authoring permissions (ADR 021 Decision 3). See
[ADR 021](adrs/021-unified-template-service.md).

## Alternatives Rejected

### Per-scope services (OrgTemplateService, FolderTemplateService, ProjectTemplateService)

Three separate services with the same CRUD pattern would triple the code
duplication that motivated this ADR. The scope discriminator in a single service
is just as expressive and avoids the maintenance burden of keeping three services
in sync.

### Keep separate permission sets per scope

The existing `PERMISSION_DEPLOYMENT_TEMPLATES_*` and `PERMISSION_ORG_TEMPLATES_*`
sets could be extended with `PERMISSION_FOLDER_TEMPLATES_*`. Rejected because it
adds five new permission enum values per scope level and makes the cascade tables
harder to read. The single `TemplateCascadePerms` table used uniformly at every
scope achieves the same access control with simpler code.

### Tag-based template selection (Model C from ADR 019)

Already rejected in ADR 019. Not reconsidered here.

### Storing folder-level templates in a central ConfigMap (not in the folder namespace)

Storing all templates in a single namespace with labels would simplify listing
but would break the RBAC ownership model: a folder namespace's grant annotations
currently determine who can modify ConfigMaps in that namespace. Moving templates
elsewhere would require a custom authorization path. Rejected in favour of keeping
templates co-located with their owning namespace.

## Consequences

### Positive

- **Single CRUD implementation.** The unified `TemplateService` handles Create,
  Get, Update, Delete, List for templates at every scope level. Duplicated code
  in `console/templates/` and `console/org_templates/` is deleted.

- **Uniform permissions.** The same five permissions (`List`, `Read`, `Write`,
  `Delete`, `Admin`) apply at every level. Permission checks use the same
  authorization walk (ADR 020 Decision 6). New hierarchy levels (if added beyond
  `v1alpha2`) require only a new cascade table, not new permission enum values.

- **Cross-level linking.** Project templates can link to folder templates, which
  can link to org templates. The render-set formula is consistent regardless of
  how deep the hierarchy goes.

- **ADR 019 semantics preserved.** Mandatory templates at any ancestor level
  remain a non-optional policy floor. The `enabled` flag retains its "available
  for linking" semantics. Product engineers still have explicit control over
  non-mandatory linked templates.

- **Clear legacy cleanup list.** Decision 9 enumerates exactly which files to
  delete, removing ambiguity for the implementer.

### Negative

- **Single-point-of-failure proto surface.** `TemplateService` is a wider
  surface than two narrow services. A breaking change to the `TemplateScope`
  enum or `TemplateScopeRef` message affects all template operations at every
  level. Mitigated by the pre-release policy (no backwards compatibility required).

- **ListLinkableTemplates walks the hierarchy.** The RPC must walk up to 5
  ancestor namespaces and list ConfigMaps at each level. Mitigated by the
  per-request caching from ADR 020 Decision 7.

- **Annotation format change.** The `console.holos.run/linked-org-templates`
  annotation from `v1alpha1` is replaced by `console.holos.run/linked-templates`
  with a different JSON format. All existing deployment template ConfigMaps must
  be migrated. Since this is pre-release, migration tooling is not required, but
  the change must be noted in the implementation PR.

### Risks

- **Scope name ambiguity.** The `scope_name` field in `TemplateScopeRef` is a
  human-readable name, not a namespace name. The handler must resolve it to a
  namespace using the same logic as `OrganizationService` and the folder walk.
  If two folders at different depths have the same display name, the resolution
  must use the parent chain (scope + parent scope name) to disambiguate. This is
  not fully specified here — the implementation must choose between passing the
  folder namespace directly (simpler) or the display name (user-friendly). The
  implementer should default to passing the namespace name for folder scopes to
  avoid ambiguity.

## References

- [ADR 017: Configuration Management RBAC Levels](017-config-management-rbac-levels.md) — Decision 5 sketches the collapsed permission set
- [ADR 019: Explicit Platform Template Linking](019-explicit-template-linking.md) — extended by this ADR to cross-level references
- [ADR 020: v1alpha2 Folder Hierarchy, Package Layout, and Secrets Semantics](020-v1alpha2-folder-hierarchy.md) — the hierarchy walk used by the rendering walk and permission checks
- [ADR 016: Configuration Management Resource Schema](016-config-management-resource-schema.md) — resource collection policy that the renderer implements

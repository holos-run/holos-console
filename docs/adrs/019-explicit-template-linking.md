# ADR 019: Explicit Platform Template Linking

## Status

Accepted

## Context

ADR 017 defines who can author templates at each level and which resource
collections the renderer reads from each level. It is silent on **which specific
ancestor templates participate in unification when a project is rendered**.

Before this decision, the renderer collected *all enabled* org-level templates
and unified them with the project template at deploy time (Model A: positional).
This model has three significant problems:

1. **Cannot support multiple reusable archetypes at the org level.** Two sibling
   org templates (e.g. `microservice-v2` and `batch-job`) that each define
   different values for `projectResources` would conflict with each other during
   CUE unification. Unification is commutative and associative — if both templates
   define `projectResources.namespacedResources.Deployment`, the result is a
   conflict error, not a choice.

2. **No mechanism to select one archetype vs. another in v1alpha1.** Folder-level
   templates that would provide per-folder archetype selection are deferred to
   v1alpha2 (ADR 016 Decision 4). In v1alpha1, there is no way for a product
   engineer to pick one archetype and exclude another.

3. **Product engineers have no visibility or control.** Org templates silently
   affect every deployment in the org. A product engineer cannot see which org
   templates will unify with their deployment template before deploying.

Issue #577 analyzed three models:

- **Model A (positional)**: All enabled org templates always unify. Simple but
  cannot support multiple archetypes at the same level.
- **Model B (explicit selection)**: The deployment template explicitly lists
  which org templates to link against. Product engineer chooses; platform
  engineer provides options.
- **Model C (tag-based filtering)**: Org templates carry tags; deployment
  templates filter by tag. Adds complexity (tag namespace management, tag
  discovery UI) without improving the mental model over explicit naming.

## Decision

**Implement Model B: explicit selection.**

The deployment template explicitly lists which org templates it links against.
At render time, the renderer collects:

1. **Mandatory + enabled** org templates — the platform policy floor, applied to
   every deployment whether linked or not.
2. **Explicitly linked** org templates — opt-in archetypes the product engineer
   chose from the set of enabled (non-mandatory) org templates.

The effective set of org templates unified at render time is:

```
(mandatory AND enabled) UNION explicitly_linked
```

A linked template that is `enabled=false` is rejected at create/update time
with `InvalidArgument`. A template that is both mandatory and explicitly linked
is deduplicated (appears once in the union).

### Semantic change to the `enabled` flag

| Flag combination | Before (Model A) | After (Model B) |
|---|---|---|
| `mandatory=true`, `enabled=true` | Policy floor — applies to every deployment | Unchanged — still policy floor |
| `mandatory=false`, `enabled=true` | Unified at deploy time on every project | Available for linking; NOT automatically unified unless explicitly linked |
| `enabled=false` | Hidden, never applied | Hidden, never applied; cannot be linked |

The `enabled` flag now means **available for selection** rather than **always
applied**. Mandatory templates remain a policy floor regardless of linking.

### Linking list storage

The linking list is stored as a JSON array in the annotation
`console.holos.run/linked-org-templates` on the deployment template ConfigMap.
Example:

```json
["microservice-v2", "istio-gateway"]
```

This annotation is written by `CreateDeploymentTemplate` and
`UpdateDeploymentTemplate` and read by the render pipeline.

### Listing linkable org templates

A new `ListLinkableOrgTemplates` RPC on `DeploymentTemplateService` returns all
enabled org templates for the project's org, including mandatory ones. The
response includes a `mandatory` flag per template so the UI can render mandatory
templates as always-on with a lock icon.

### Render-time resolution

1. Resolve the deployment template ConfigMap from K8s.
2. Read the `console.holos.run/linked-org-templates` annotation (JSON array).
3. Call `ListOrgTemplateSourcesForRender(ctx, org, linkedNames)` on the
   `OrgTemplate` provider:
   - Return all org templates where `mandatory=true AND enabled=true`.
   - Additionally return all org templates where `enabled=true AND name IN linkedNames`.
   - Deduplicate by name.
4. Unify the resulting CUE sources with the deployment template source.

The `RenderDeploymentTemplate` preview RPC accepts a `linked_org_templates`
field so draft templates can preview their effective unification before saving.

## Alternatives Rejected

### Model A (positional / all enabled)

Rejected because:
- Cannot support multiple archetypes at the org level without conflicts.
- Gives product engineers no control over which org templates affect their deployment.
- Sibling archetype templates would conflict on shared `projectResources` fields.

### Model C (tag-based filtering)

Rejected because:
- Adds tag namespace management, tag discovery, and tag-based filtering logic.
- Does not improve the mental model: a product engineer still needs to know
  which tags correspond to which archetypes.
- Explicit naming (Model B) is simpler and equally expressive for the use cases
  identified in v1alpha1.

## Consequences

### Positive

- **Multiple archetypes coexist.** Platform engineers can publish `microservice-v2`,
  `batch-job`, and `worker` org templates simultaneously. Product engineers choose
  one (or more) per deployment template. No conflicts because non-selected
  archetypes do not participate in unification.

- **Product engineer visibility.** The create/edit UI shows which org templates
  are linked and previews the effective rendered output before saving.

- **Mandatory policy floor preserved.** Platform engineers can still enforce
  org-wide constraints (NetworkPolicy, label requirements, etc.) via mandatory
  templates without requiring product engineer opt-in.

- **Backwards compatible.** The `linked_org_templates` field is an additive
  proto field. Existing deployment templates with no annotation get an empty
  linking list — only mandatory org templates apply, which matches the pre-ADR
  behavior for templates that linked no non-mandatory templates.

- **Validation at write time.** Linking a non-existent or disabled template
  is caught at `Create`/`Update` time with `InvalidArgument`, not silently at
  render time.

### Negative

- **Product engineers must explicitly link archetypes.** Before this change,
  a platform engineer could deploy a new org template and it would automatically
  apply to all projects. After this change, non-mandatory templates require
  product engineer action (linking). This is intentional — it gives product
  engineers control — but it means org-wide rollout of non-mandatory archetypes
  requires a migration step.

- **Linked list is by name, not by reference.** Renaming an org template breaks
  the link silently (the `UpdateDeploymentTemplate` validation checks existence
  at write time, not at render time). Mitigated by the `RenderDeploymentTemplate`
  preview, which catches dangling links before deployment.

### Deferred

- **Template versioning and pinning.** Linked templates are resolved by name to
  the latest version at render time. Pinning to a specific template content hash
  is deferred to a future release.

- **Opt-out of mandatory templates.** Mandatory templates apply unconditionally.
  A per-project opt-out mechanism is deferred.

- **Folder-level linking.** This ADR applies to Organization → Project linking.
  Folder-level templates (v1alpha2) will extend this model.

## References

- [Issue #577 — Analysis: Template Selection and Linking Mechanism](https://github.com/holos-run/holos-console/issues/577)
- [Issue #579 — Parent implementation plan](https://github.com/holos-run/holos-console/issues/579)
- [ADR 016: Configuration Management Resource Schema](016-config-management-resource-schema.md)
- [ADR 017: Configuration Management RBAC Levels](017-config-management-rbac-levels.md)
- [ADR 018: CUE Template Default Values](018-cue-template-default-values.md)

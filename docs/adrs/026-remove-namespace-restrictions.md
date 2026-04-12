# ADR 026: CUE-First Policy Model; Remove Go-Level Namespace Restrictions

## Status

Accepted -- partially revokes [ADR 016](016-config-management-resource-schema.md) Decision 6

## Context

ADR 016 Decision 6 established that resources in both `platformResources` and
`projectResources` carry a `NamespacedResources` map keyed by namespace, but the
Go render pipeline enforces that **every** namespaced resource must reside in the
project's own namespace. Specifically, `walkNamespacedResources()` in
`console/deployments/render.go` rejects any resource whose
`metadata.namespace` does not equal the project namespace passed by the caller:

```go
// Enforce project namespace constraint.
if u.GetNamespace() != expectedNamespace {
    return nil, fmt.Errorf(
        "%s resource %s/%s: namespace %q does not match project namespace %q",
        fieldPath, u.GetKind(), u.GetName(), u.GetNamespace(), expectedNamespace)
}
```

Similarly, the `Apply`, `Reconcile`, and `Cleanup` functions in
`console/deployments/apply.go` accept a single `namespace` parameter and perform
all Kubernetes operations within that one namespace, regardless of what the
resource's own `metadata.namespace` says.

This restriction was appropriate in the initial design when all resources were
assumed to live in the project namespace, but it now prevents legitimate
multi-namespace use cases:

1. **Platform resources in gateway namespaces.** An HTTPRoute in
   `platformResources` targets the `istio-ingress` namespace, not the project
   namespace. The current restriction forces the render pipeline to reject it.

2. **Shared infrastructure resources.** A folder-level template may produce
   ConfigMaps or Secrets in a shared monitoring namespace. The per-project
   namespace lock prevents this.

3. **Cross-namespace ReferenceGrants.** Gateway API ReferenceGrants may need to
   be created in a target namespace to allow cross-namespace routing. The current
   model cannot express this.

The CUE template schema already supports multi-namespace resources --
`NamespacedResources` is `map[string]map[string]map[string]Resource` where the
outer key is the namespace. The restriction exists only in Go code, not in the
data model. Removing it aligns the Go pipeline with the schema.

### Relationship to ADR 016

ADR 016 established two enforcement layers for resource safety:

1. **CUE-level enforcement** -- platform templates close the `projectResources`
   struct to restrict allowed Kinds (Decision 9). This runs at CUE evaluation
   time.

2. **Go-level enforcement** -- the renderer validates kind allowlists and
   namespace constraints after CUE evaluation (Decisions 6 and 9).

This ADR shifts the primary policy mechanism from Go-level namespace enforcement
to CUE unification. The Go kind allowlist and managed-by label check remain as
backend safety nets. The namespace restriction in `walkNamespacedResources()` is
removed entirely.

**What is preserved from ADR 016:**
- Decision 1 (Go structs define the template API contract)
- Decision 2 (TypeMeta for version discrimination)
- Decision 3 (Metadata carries name and annotations)
- Decision 4 (Organization -> Folder -> Project hierarchy)
- Decision 5 (platformInput / projectInput split)
- Decision 6 (platformResources / projectResources split) -- the collection
  split is preserved; only the Go-level namespace enforcement is revoked
- Decision 7 (no package clause in templates)
- Decision 8 (CUE unification merges templates from all levels)
- Decision 9 (platform templates close projectResources struct)
- Decision 10 (ResourceSet type)
- Decision 11 (version-agnostic Renderer interface)
- Decision 12 (package layout)

**What is revoked from ADR 016:**
- The namespace enforcement aspect of Decision 6, specifically the
  `walkNamespacedResources()` check that `metadata.namespace == expectedNamespace`.
  Resources in both `platformResources` and `projectResources` may now target
  any namespace.

## Decisions

### 1. `walkNamespacedResources()` no longer enforces `metadata.namespace == expectedNamespace`.

The function retains the struct-key/metadata consistency check -- it still
verifies that the CUE struct key matches the resource's `metadata.namespace`,
`kind`, and `metadata.name`. This catches authoring errors (a resource filed
under the wrong key). But the check that the namespace equals the project
namespace is removed.

Before:

```go
// Enforce struct-key / metadata consistency.
if u.GetNamespace() != nsKey {
    return nil, fmt.Errorf("metadata.namespace %q does not match struct key %q", ...)
}
// Enforce project namespace constraint.
if u.GetNamespace() != expectedNamespace {
    return nil, fmt.Errorf("namespace %q does not match project namespace %q", ...)
}
```

After:

```go
// Enforce struct-key / metadata consistency.
if u.GetNamespace() != nsKey {
    return nil, fmt.Errorf("metadata.namespace %q does not match struct key %q", ...)
}
// No project namespace constraint -- resources may target any namespace.
// CUE unification (closed structs, constraints) at folder/org scope is the
// primary mechanism for restricting which namespaces a template may target.
```

The `expectedNamespace` parameter is removed from the function signature since
it is no longer used.

### 2. `Apply`, `Reconcile`, and `Cleanup` use each resource's own `metadata.namespace`.

The `Apply` function currently receives a single `namespace` parameter and
applies every resource to that namespace via
`a.client.Resource(gvr).Namespace(namespace).Patch(...)`.

After this change, each resource is applied to its own `metadata.namespace`
using `r.GetNamespace()`:

```go
_, err = a.client.Resource(gvr).Namespace(r.GetNamespace()).Patch(...)
```

The `Reconcile` function changes similarly: the orphan-cleanup phase lists
owned resources across all namespaces that appear in the desired set, then
deletes any owned resources not in the desired set. The `Cleanup` function
lists and deletes owned resources across all namespaces tracked by the
deployment.

The single `namespace` parameter is removed from these function signatures in
favor of reading the namespace from each resource.

### 3. CUE unification is the primary restriction mechanism.

Platform engineers use CUE closed structs and constraints in folder-level and
organization-level templates to control which namespaces project templates may
target. This is standard CUE unification -- the same mechanism described in ADR
016 Decision 9 for restricting Kinds.

For example, an organization-level template can restrict `projectResources` to
only the project namespace:

```cue
// Organization template: restrict project resources to the project namespace.
projectResources: (platform.namespace): _
```

Because this is a closed struct (via CUE's standard struct closing rules when
no other keys are defined), a project template that tries to produce resources
in a different namespace fails at CUE evaluation time:

```
projectResources."other-namespace": field not allowed
```

Alternatively, an organization-level template can allow a known set of
namespaces:

```cue
// Organization template: allow project namespace and istio-ingress.
projectResources: {
    (platform.namespace): _
    "istio-ingress": _
}
```

This approach is strictly more powerful than the Go-level restriction because:

- It is configurable per organization or per folder, not hardcoded.
- It produces CUE-level errors with exact field paths, not opaque Go errors.
- It is visible in the template preview before any deployment.
- It can express allowlists that vary by folder subtree.

### 4. Folder and organization templates support a mandatory flag to push policy.

ADR 019 established the `mandatory` flag on templates: mandatory templates are
always included in unification regardless of whether a project explicitly links
them. This is the enforcement mechanism for namespace restrictions when
platform-wide policy is needed.

A mandatory organization template that closes `projectResources` to the project
namespace applies to every project in the organization. Product engineers cannot
unlink it. This is analogous to GitHub organization settings that are pushed into
every repository -- the organization owner controls the policy, individual
projects cannot override it.

When a platform engineer needs to restrict namespaces for a subset of projects,
they create a mandatory folder-level template on the relevant folder. This
restricts all projects under that folder without affecting the rest of the
organization.

### 5. The Go kind allowlist and managed-by label check remain as backend safety nets.

The `allowedKindSet` in `render.go` and the `allowedKinds` GVR map in
`apply.go` continue to validate that every resource produced by CUE evaluation
is a recognized Kind with a known GroupVersionResource mapping. The
`validateResource()` function in `render.go` continues to enforce:

- `apiVersion` is present
- `kind` is present and in the allowlist
- `metadata.name` is present
- `app.kubernetes.io/managed-by` label equals `console.holos.run`

These checks are backend safety nets, not the primary policy mechanism. They
catch resources that bypass CUE-level restrictions (for example, if no
organization template defines a closed struct for `projectResources`). The kind
allowlist also serves as the GVR lookup table for the dynamic Kubernetes client.

## Consequences

### Positive

- **Multi-namespace resources work.** Templates can produce resources in any
  namespace: HTTPRoutes in `istio-ingress`, monitoring ConfigMaps in a shared
  namespace, ReferenceGrants in target namespaces. The schema already supported
  this; the Go pipeline now matches.

- **CUE errors are better than Go errors.** When a namespace restriction is
  enforced via a closed CUE struct, the error message shows the exact field path
  and is visible in the template preview RPC. The previous Go-level error
  appeared only at render time with a generic message.

- **Configurable per scope.** Different organizations and folders can have
  different namespace policies. A sandbox folder might allow any namespace; a
  production folder might restrict to the project namespace only. This is not
  possible with the current hardcoded Go check.

- **Simpler Go code.** Removing the `expectedNamespace` parameter from
  `walkNamespacedResources()` and the single `namespace` parameter from `Apply`,
  `Reconcile`, and `Cleanup` simplifies the function signatures and eliminates a
  class of bugs where the caller passes the wrong namespace.

- **Aligned with ADR 016 philosophy.** ADR 016 already established that CUE
  unification is the primary mechanism and Go code is the safety net (Decision
  9). This ADR extends that principle to namespace restrictions, making the
  system consistent.

### Negative

- **No namespace restriction by default.** If no organization or folder template
  defines a closed struct for `projectResources`, project templates can produce
  resources in any namespace. Previously, the Go-level check enforced the project
  namespace regardless of template configuration. Platform engineers who want
  namespace restrictions must now configure them explicitly via templates.

- **Migration required for existing deployments.** Organizations that rely on the
  implicit Go-level namespace restriction must create a mandatory organization
  template that closes `projectResources` to the project namespace before the Go
  restriction is removed. This is a one-time migration.

### Risks

- **Privilege escalation via namespace targeting.** A project template that
  produces a RoleBinding in a namespace it does not own could grant permissions
  outside its scope. Mitigated by: (1) the Kind allowlist prevents
  ClusterRoleBindings; (2) mandatory templates can restrict the allowed namespace
  set; (3) Kubernetes RBAC on the console's service account limits which
  namespaces it can write to.

- **Orphan cleanup across namespaces.** The current `Reconcile` and `Cleanup`
  functions list resources in a single namespace. After this change, they must
  track all namespaces a deployment has written to and clean up across all of
  them. If the tracking is incomplete (for example, if a namespace is removed
  from the template between deployments), orphaned resources may remain.
  Mitigated by recording the set of namespaces in the deployment's annotations
  and using the ownership label to find all owned resources.

## References

- [ADR 016: Configuration Management Resource Schema](016-config-management-resource-schema.md) -- establishes the resource schema and CUE-first enforcement philosophy; Decision 6 (namespace enforcement) is partially revoked by this ADR
- [ADR 019: Explicit Platform Template Linking](019-explicit-template-linking.md) -- establishes the mandatory template flag used for policy enforcement
- [ADR 024: Template Versioning, Releases, and Dependency Constraints](024-template-versioning.md) -- immutable releases and version constraints for linked templates
- [Issue #885: Remove per-resource namespace restrictions](https://github.com/holos-run/holos-console/issues/885) -- parent implementation plan
- [`console/deployments/render.go`](../../console/deployments/render.go) -- `walkNamespacedResources()` and `allowedKindSet`
- [`console/deployments/apply.go`](../../console/deployments/apply.go) -- `Apply`, `Reconcile`, `Cleanup`, and `allowedKinds` GVR map

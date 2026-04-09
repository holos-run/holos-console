# ADR 020: v1alpha2 Folder Hierarchy, Package Layout, and Secrets Semantics

## Status

Accepted

## Context

ADR 016 (Decision 4) defines the organizational hierarchy as Organization →
Folder(s) → Project, bounded at five levels total (1 org + up to 3 folder levels
+ 1 project), but explicitly defers the Folder type and all folder-related
implementation to `v1alpha2`. ADR 017 defines RBAC cascade rules for this
hierarchy but similarly defers folder-level authorization code to `v1alpha2`.

This ADR closes that gap. It specifies:

1. The `api/v1alpha2/` Go package layout and the types introduced at this version.
2. How Folder resources are stored in Kubernetes (namespace naming, labels, and
   parent references).
3. The hierarchy walk algorithm used at template evaluation and authorization
   time.
4. How Secrets retain their non-cascading data-access semantics while
   participating in the default-share cascade chain added in `v1alpha2`.

This ADR is a prerequisite for any `v1alpha2` implementation. It replaces the
deferred notes in ADR 016 Decision 4 and ADR 017, which reference cross-linking
decisions recorded here.

### What v1alpha2 Replaces

`v1alpha2` **replaces** `v1alpha1`. There are no compatibility shims, no
dual-stack operation, and no deprecation period. This code is pre-release;
breaking changes are explicitly permitted by the project policy. All
`v1alpha1` service handlers, Go types, and CUE schema definitions are deleted
when `v1alpha2` is implemented.

### Terminology

This ADR follows the terminology in ADR 016:

- **Product engineer** — writes deployment templates at the project level.
- **Site reliability engineer (SRE)** — writes templates at a folder level.
- **Platform engineer** — writes templates at the organization or top-level
  folder.

## Decisions

### 1. api/v1alpha2 Go package layout.

```
api/
  v1alpha1/
    ...                    // existing, unchanged until v1alpha2 is implemented
  v1alpha2/
    doc.go                 // package doc with rationale and migration notes
    types.go               // TypeMeta, Metadata, ResourceSet, ResourceSetSpec
    input.go               // PlatformInput, ProjectInput, FolderInput, Claims, EnvVar
    resources.go           // PlatformResources, ProjectResources, Resource
    iam.go                 // Role, Permission, Principal, Grant (updated cascade tables)
    hierarchy.go           // Organization, Folder, Project Go types
    annotations.go         // annotation and label constants (v1alpha2 additions)
    types_test.go          // CUE round-trip validation tests
```

The `v1alpha2` package reuses type shapes from `v1alpha1` where unchanged (e.g.
`TypeMeta`, `Metadata`, `Resource`). It introduces the `Folder` type and extends
`PlatformInput` with folder ancestry information. The `apiVersion` field on all
top-level types changes to `"console.holos.run/v1alpha2"`.

CUE schema is generated from `api/v1alpha2/` types via `cue get go` and
embedded into the binary, replacing the `v1alpha1` generated schema.

### 2. The Folder Go type.

```go
// Folder represents an intermediate grouping level in the organization
// hierarchy between an Organization and a Project (or between two Folder
// levels). A Folder is stored as a Kubernetes Namespace with labels that
// identify its type, parent, and root organization.
//
// Folders are optional. An Organization may contain Projects directly without
// any Folders.
type Folder struct {
    TypeMeta `json:",inline" yaml:",inline"`
    Metadata Metadata    `json:"metadata" yaml:"metadata" cue:"metadata"`
    Spec     FolderSpec  `json:"spec"     yaml:"spec"     cue:"spec"`
}

// FolderSpec carries the mutable configuration of a Folder.
type FolderSpec struct {
    // DisplayName is a human-readable label for the folder shown in the UI.
    DisplayName string `json:"displayName" yaml:"displayName" cue:"displayName"`

    // Parent is a reference to the parent Folder or Organization namespace.
    // The parent must exist and must be in the same organization.
    //
    // At CreateFolder time the server walks the ancestor chain to enforce the
    // maximum depth of 3 folder levels (ADR 016 Decision 4). A depth violation
    // is rejected with codes.InvalidArgument.
    Parent ParentRef `json:"parent" yaml:"parent" cue:"parent"`
}

// ParentRef identifies the parent scope of a Folder or Project.
type ParentRef struct {
    // Namespace is the Kubernetes namespace of the parent (org or folder).
    Namespace string `json:"namespace" yaml:"namespace" cue:"namespace"`
}
```

### 3. Kubernetes storage: namespace naming, labels, and parent references.

Every folder is stored as a Kubernetes Namespace. The namespace **name** is
derived from a predictable hash so that folder names are unique within their
parent without requiring a global lock:

```
{ns-prefix}{folder-prefix}{parent-ns-hash-6}-{folder-slug}
```

Where:
- `{ns-prefix}` is the value of the `--namespace-prefix` CLI flag (default
  `"holos-"`).
- `{folder-prefix}` is the value of the new `--folder-prefix` CLI flag (default
  `"fld-"`).
- `{parent-ns-hash-6}` is the first six characters of the lowercase hex SHA-256
  of the parent namespace name. This distinguishes sibling folders with the same
  display name that live under different parents.
- `{folder-slug}` is the display name converted to a lowercase DNS label
  (non-alphanumeric characters replaced with hyphens, consecutive hyphens
  collapsed, truncated to 40 characters).

**Example**: A folder named `"Payments EU"` whose parent namespace is
`"holos-org-acme"` gets the namespace name:
`"holos-fld-a4b9c1-payments-eu"`.

This scheme makes the namespace name deterministic given (parent namespace,
folder display name), so a create-then-check is idempotent and re-entrant.

**Labels on the Namespace**:

```yaml
console.holos.run/resource-type: folder
console.holos.run/organization: <root-org-namespace>
console.holos.run/parent: <parent-namespace>
console.holos.run/display-name: <display name>
console.holos.run/creator-email: <oidc-email>
```

The `console.holos.run/parent` label is the single pointer that lets the walk
algorithm traverse the hierarchy. Its value is the Kubernetes namespace name of
the immediate parent (an organization namespace or another folder namespace).

The `console.holos.run/organization` label stores the root organization
namespace name. This allows a single label selector to retrieve all folders that
belong to an organization without walking the tree.

**Labels on a Project Namespace that has a Folder parent**:

```yaml
console.holos.run/resource-type: project
console.holos.run/organization: <root-org-namespace>
console.holos.run/parent: <immediate-parent-namespace>   # folder ns if folders exist, else org ns
console.holos.run/project: <project-name>
```

In `v1alpha1`, the `console.holos.run/parent` label did not exist — projects
stored the organization directly. In `v1alpha2` the `parent` label is the
immediate ancestor, enabling uniform upward traversal regardless of level.

### 4. --folder-prefix CLI flag.

A new flag `--folder-prefix` (default `"fld-"`) prefixes every folder namespace
name, analogous to the existing `--organization-prefix` (`"org-"`) and
`--project-prefix` (`"prj-"`). Rules:

- The flag may be an empty string, disabling the prefix entirely.
- The resulting namespace name must be a valid Kubernetes DNS subdomain label.
  The server validates this at startup and returns an error if the combination of
  `--namespace-prefix` + `--folder-prefix` + hashed slug exceeds 63 characters.
- Changing the flag on a running server **does not** rename existing folder
  namespaces. The flag affects only new folder creation.

### 5. Depth enforcement.

The maximum folder depth is **3** levels between an organization and a project:

```
Organization (depth 0)
  Folder      (depth 1)
    Folder    (depth 2)
      Folder  (depth 3)
        Project
```

Depth is enforced at `CreateFolder` time. The server walks the ancestor chain
from the proposed parent upward, counting intermediate folders. If the count is
already 3 (i.e., the parent is itself at depth 3), the create request is
rejected with `codes.InvalidArgument` and a message stating the maximum depth.

This limit follows the experience of Google Cloud Resource Manager, where
hierarchies deeper than 3 folder levels become difficult to reason about
operationally. It also bounds the worst-case Kubernetes API call count per
hierarchy walk to 5 (1 org + 3 folders + 1 project).

### 6. Hierarchy walk algorithm.

The walk is used at two points:

1. **Template evaluation time** — to collect templates from all ancestor levels.
2. **Authorization time** — to compute a user's effective role at a given level.

**Algorithm (upward walk from a starting namespace)**:

```
walk(ctx, startNamespace) → []Namespace:
  result = []
  current = startNamespace
  for depth in 0..4:
    ns = loadNamespace(ctx, current)  // cached — see Decision 7
    result.append(ns)
    if ns.labels["console.holos.run/resource-type"] == "organization":
      break  // reached the root
    parent = ns.labels["console.holos.run/parent"]
    if parent == "":
      return error("namespace missing parent label: " + current)
    current = parent
  return result
```

The walk terminates when it reaches a namespace with
`console.holos.run/resource-type: organization` or after 5 iterations (the
bounded maximum), whichever comes first. If the loop reaches 5 iterations
without finding an organization namespace, the walk returns an error — this
indicates a data integrity problem (missing labels or a cycle) and the RPC
returns `codes.Internal`.

The result list is ordered from the starting namespace upward to the
organization (project first, org last). Callers that need org-to-project order
reverse the slice.

### 7. Per-request hierarchy walk caching.

Walking the hierarchy can require up to 5 Kubernetes API server reads per
request. To avoid redundant reads within a single RPC, the namespace objects are
cached in the request context.

The cache is attached to `context.Context` by the RBAC interceptor before the
handler is called. It is keyed by namespace name and stores the full
`corev1.Namespace` value. The interceptor creates an empty cache; the walk
function checks the cache before calling the Kubernetes API and populates it on
cache misses.

```go
// hierarchyCacheKey is the context key for the per-request namespace cache.
type hierarchyCacheKey struct{}

// WithHierarchyCache attaches a new empty namespace cache to ctx.
// Called by the RBAC interceptor at the start of each request.
func WithHierarchyCache(ctx context.Context) context.Context {
    return context.WithValue(ctx, hierarchyCacheKey{}, &sync.Map{})
}

// cachedLoadNamespace returns a namespace from the per-request cache, or
// loads it from the Kubernetes API and populates the cache.
func cachedLoadNamespace(ctx context.Context, client kubernetes.Interface, name string) (*corev1.Namespace, error) {
    cache, _ := ctx.Value(hierarchyCacheKey{}).(*sync.Map)
    if cache != nil {
        if v, ok := cache.Load(name); ok {
            return v.(*corev1.Namespace), nil
        }
    }
    ns, err := client.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
    if err != nil {
        return nil, err
    }
    if cache != nil {
        cache.Store(name, ns)
    }
    return ns, nil
}
```

The cache has no TTL — it is scoped to a single RPC invocation and is garbage
collected when the request context is cancelled. It is safe for concurrent reads
(as can happen if multiple goroutines check permissions in the same request) via
`sync.Map`.

### 8. FolderInput: folder ancestry in PlatformInput.

`PlatformInput` in `v1alpha2` gains a `Folders` field that carries ancestry
information for the current project:

```go
// PlatformInput carries values set by platform engineers and the system.
// In v1alpha2, it includes the folder ancestry chain between the organization
// and the project.
type PlatformInput struct {
    Project          string       `json:"project"          yaml:"project"          cue:"project"`
    Namespace        string       `json:"namespace"        yaml:"namespace"        cue:"namespace"`
    GatewayNamespace string       `json:"gatewayNamespace" yaml:"gatewayNamespace" cue:"gatewayNamespace"`
    Organization     string       `json:"organization"     yaml:"organization"     cue:"organization"`
    Claims           Claims       `json:"claims"           yaml:"claims"           cue:"claims"`

    // Folders is the ordered chain of folder names from the organization down
    // to the immediate parent of the project. The first element is the
    // top-level folder (direct child of the organization); the last element is
    // the immediate folder parent of the project. Empty if the project is a
    // direct child of the organization (no folders).
    Folders []FolderInfo `json:"folders,omitempty" yaml:"folders,omitempty" cue:"folders?"`
}

// FolderInfo carries the name and namespace of a folder in the ancestry chain.
type FolderInfo struct {
    // Name is the folder's display name as set by the SRE who created it.
    Name      string `json:"name"      yaml:"name"      cue:"name"`
    // Namespace is the Kubernetes namespace for this folder level.
    Namespace string `json:"namespace" yaml:"namespace" cue:"namespace"`
}
```

CUE templates can access folder ancestry via `platform.folders`, e.g.:

```cue
// Reference the immediate folder parent (last in the chain):
_folderNs: platform.folders[len(platform.folders)-1].namespace
```

### 9. Secrets semantics in v1alpha2.

ADR 007 establishes that organization-level grants **do not cascade** to secret
access. This principle is preserved in `v1alpha2` and extended through the
folder hierarchy:

**Principle (unchanged from ADR 007)**: A user's role at any ancestor level
(organization or folder) does **not** grant access to read or write Secrets at
the project or secret level. Secret *access* requires an explicit grant on the
project namespace or the secret object itself.

**What changes in v1alpha2**: The *default-share cascade chain* for Secrets at
creation time. When a new Secret is created, the system applies default-share
grants by walking the ancestor chain upward from the project and merging
`console.holos.run/default-share-*` annotations:

```
1. Start with the project-level default shares (from the project namespace).
2. Walk to each parent folder; merge the folder's default shares.
3. Walk to the organization; merge the org's default shares.
4. Apply the merged grant set as the initial share state of the new Secret.
   (Highest role wins per principal — same as the existing merge rule.)
```

The `console.holos.run/default-share-users` and
`console.holos.run/default-share-roles` annotations on org, folder, and project
namespaces specify principals and their default roles. These annotations follow
the same JSON format as the existing `console.holos.run/share-users` and
`console.holos.run/share-roles` annotations.

**What does not change**: Reading or writing a Secret after creation requires
explicit grants on that Secret's object (or on the project namespace for
project-level grants). No amount of org- or folder-level role grants access to
existing Secrets. The `SecretsService` RBAC walk in `console/secrets/handler.go`
already implements project + secret two-level evaluation; in `v1alpha2` this walk
is extended with the same upward logic, but only for *default-share propagation
at creation time*, not for runtime access checks.

**Rationale**: Separating *creation-time default-share propagation* (cascade) from
*runtime access* (non-cascade) means an SRE at the folder level can ensure that
new Secrets in their folder are automatically shared with the folder's on-call
rotation at creation time — without that SRE being able to read Secrets that
pre-date the folder grant or that opt out of the default share.

### 10. Cross-reference updates to ADR 016 and ADR 017.

ADR 016 Decision 4 defers folders to `v1alpha2` with no specification. Append
the following note to ADR 016 Decision 4:

> **v1alpha2 specification**: See [ADR 020](020-v1alpha2-folder-hierarchy.md)
> for the complete folder type definition, namespace naming scheme, walk
> algorithm, and per-request caching. ADR 020 is the authoritative
> implementation specification for v1alpha2.

ADR 017 notes that folder-level authorization code is deferred to `v1alpha2`.
Append the following note to the ADR 017 Negative Consequences section:

> **v1alpha2 specification**: See [ADR 020](020-v1alpha2-folder-hierarchy.md)
> for the walk algorithm, per-request caching, and depth enforcement.

## Glossary additions

The following terms should be added to `docs/glossary.md`:

### Folder

An optional intermediate grouping level in the organization hierarchy, sitting
between an Organization and a Project (or between two folder levels). Stored as
a Kubernetes Namespace with `console.holos.run/resource-type: folder` and a
`console.holos.run/parent` label pointing to its immediate parent. Up to three
folder levels are supported between any Organization and Project (ADR 016
Decision 4). Introduced in `v1alpha2`; not present in `v1alpha1`.

### Hierarchy walk

The algorithm that traverses the Organization → Folder(s) → Project chain upward
from a given namespace to collect templates or resolve effective permissions. The
walk follows `console.holos.run/parent` labels, terminating at the Organization
namespace. Bounded to 5 levels; results are cached per-request via a
`context.Context`-attached `sync.Map`. See ADR 020 Decision 6 and Decision 7.

### Default-share cascade

The mechanism by which a new resource (e.g., Secret) inherits share grants from
its ancestor chain at creation time. Each ancestor's
`console.holos.run/default-share-*` annotations are merged into the new
resource's share state (highest role wins per principal). Runtime access
requires explicit grants; default-share cascade applies only at the moment of
resource creation. See ADR 020 Decision 9 and ADR 007.

## Consequences

### Positive

- **Complete specification.** An implementer can build the folder hierarchy,
  walk algorithm, and secret creation semantics from this ADR without
  improvising design trade-offs.

- **Predictable namespace names.** The `{parent-hash}-{slug}` scheme makes
  folder namespace names deterministic and collision-free within a parent without
  requiring a distributed lock or a separate name-registry object.

- **Bounded walk cost.** The maximum hierarchy depth is 5 levels, so the walk
  requires at most 5 Kubernetes API calls. Per-request caching eliminates
  redundant reads within a single RPC.

- **ADR 007 preservation.** Secret access remains non-cascading. The
  default-share chain at creation time does not weaken the data-isolation
  guarantee from ADR 007.

- **Clear migration path.** v1alpha2 replaces v1alpha1 without coexistence or
  deprecation shims, consistent with the project's pre-release policy.

### Negative

- **Two label schemes coexist temporarily.** During the implementation transition
  from `v1alpha1` to `v1alpha2`, existing project namespaces will not have the
  `console.holos.run/parent` label. Tooling must migrate or bootstrap this label
  before enabling the walk.

- **Namespace name stability via hash.** If the parent namespace is renamed
  (which requires migrating all child labels), the computed hash for child
  folders changes. This is acceptable because folder namespaces are not
  user-visible identifiers, but it must be documented in the migration guide.

### Risks

- **Label integrity.** The walk depends on the `parent` label being present and
  correct. Corrupt or missing labels cause the walk to return an error. Mitigated
  by the walk's explicit error path and by admission webhooks (future work) that
  enforce label presence on namespace creation.

- **Hash collisions.** A 6-character hex truncation of SHA-256 has a collision
  probability of ~2.3e-7 per pair of distinct inputs. For a system with O(100)
  folders per parent this is negligible. If collisions become a concern in a
  large deployment, the hash prefix length can be increased to 8 or 12 characters
  in `v1alpha3` without affecting the walk algorithm.

## References

- [ADR 007: Organization Grants Do Not Cascade](007-org-grants-no-cascade.md)
- [ADR 016: Configuration Management Resource Schema](016-config-management-resource-schema.md) — Decision 4 defers folders to v1alpha2
- [ADR 017: Configuration Management RBAC Levels](017-config-management-rbac-levels.md) — Decision 7 defers folder authorization to v1alpha2
- [ADR 019: Explicit Platform Template Linking](019-explicit-template-linking.md) — folder-level linking deferred (extended by ADR 021)
- [ADR 021: Unified Template Service and Collapsed Template Permissions](021-unified-template-service.md)
- [Google Cloud Resource Manager: Resource Hierarchy](https://cloud.google.com/resource-manager/docs/cloud-platform-resource-hierarchy)

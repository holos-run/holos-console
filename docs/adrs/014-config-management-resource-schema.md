# ADR 014: Configuration Management Resource Schema

## Status

Accepted

## Context

The console manages Kubernetes resources through CUE templates that are
evaluated and applied at deployment time. The current system uses ad-hoc Go
structs (`SystemInput`, `UserInput` in `console/deployments/render.go`) and
inline CUE schema definitions (`#Input`, `#System` in template source) with no
formal, versionable API contract. ADR 012 introduced structured output
(`output.namespacedResources`, `output.clusterResources`) and ADR 013 split
system and user inputs. These decisions established the right separation of
concerns but did not define a versioned API type system.

Without `apiVersion`/`kind` discriminators and Go-native type definitions, the
template interface cannot evolve safely. Adding a new input field requires
coordinated changes across Go structs, CUE schemas, and proto messages with no
compile-time enforcement that they agree.

The console also needs to support an organizational hierarchy deeper than the
current two-level model (Organization -> Project). Platform engineers need to
define templates at intermediate "folder" levels that apply to all projects
beneath them, and security engineers need to define constraints at higher folder
levels that cannot be overridden by lower levels. The resource schema must
accommodate this hierarchy from the start.

### Terminology

This ADR uses terms that map to roles in a typical engineering organization:

- **Product engineer** — Writes deployment templates at the project level.
  Defines how their application is deployed (container image, ports, env vars).
  Analogous to a developer who owns a microservice.

- **Site reliability engineer (SRE)** — Writes templates at a folder level to
  enforce operational standards across a set of projects. Defines monitoring
  sidecars, resource limits, or health check requirements. Analogous to a team
  lead who owns reliability for a product area.

- **Platform engineer** — Writes templates at the organization level or a
  high-level folder to enforce platform-wide policy. Defines network policies,
  pod security standards, or namespace-level RBAC. Analogous to an
  infrastructure team member who owns the shared platform.

These roles are not mutually exclusive. A single person may operate at multiple
levels depending on the task. The schema does not enforce role boundaries — RBAC
does (see ADR 015).

### Resource Model Overview

The diagram below shows how templates, inputs, and resource collections fit
together. Start here if you are new to the system and want to build a template.

![Resource Model](014-resource-model.svg)

## Decisions

### 1. Go structs with CUE struct tags define the template API contract.

The template interface is defined as Go structs in a versioned `api/` package.
Each struct field carries three tags — `json`, `yaml`, and `cue` — following the
pattern established by `holos-run/holos` in `api/core/v1alpha6/types.go`:

```go
type ResourceSet struct {
    TypeMeta `json:",inline" yaml:",inline"`
    Metadata Metadata        `json:"metadata"          yaml:"metadata"          cue:"metadata"`
    Spec     ResourceSetSpec `json:"spec"              yaml:"spec"              cue:"spec"`
}
```

Go structs are the single source of truth. CUE schemas are generated from Go
types via `cue get go`, ensuring the CUE evaluation environment and the Go
rendering pipeline always agree on the shape of inputs and outputs.

Proto messages remain the RPC contract. The Go API types and proto messages
address different boundaries: proto defines what travels over the wire between
frontend and backend; Go API types define what travels between the backend and
the CUE evaluator. See the comment on issue #509 for the detailed analysis.

### 2. Every top-level type carries TypeMeta for version discrimination.

```go
// TypeMeta identifies the API version and kind of a resource.
// Every top-level configuration resource embeds TypeMeta so that consumers can
// dispatch on apiVersion and kind without knowing the concrete Go type.
type TypeMeta struct {
    // APIVersion is the versioned schema identifier, e.g. "console.holos.run/v1alpha1".
    APIVersion string `json:"apiVersion" yaml:"apiVersion" cue:"apiVersion"`
    // Kind is the resource type name, e.g. "ResourceSet".
    Kind       string `json:"kind"       yaml:"kind"       cue:"kind"`
}
```

The `apiVersion` field uses the format `console.holos.run/{version}`. The
initial version is `v1alpha1`. When breaking changes are needed, a new version
package (`api/v1alpha2/`) is created with its own types and a migration path
from the previous version.

### 3. Metadata carries the resource name and optional annotations.

```go
// Metadata provides identifying information for a configuration resource.
// It intentionally does not replicate Kubernetes ObjectMeta; it carries only
// what the configuration management system needs.
type Metadata struct {
    // Name is the unique identifier of the resource within its scope.
    Name        string            `json:"name"                  yaml:"name"                  cue:"name"`
    // Annotations carry optional key-value metadata. Used for display names,
    // descriptions, and grant storage (see ADR 015).
    Annotations map[string]string `json:"annotations,omitempty" yaml:"annotations,omitempty" cue:"annotations?"`
}
```

### 4. The organizational hierarchy is: Organization -> Folder(s) -> Project.

The configuration management hierarchy uses Kubernetes Namespaces at every
level. Each level is distinguished by a label on the Namespace:

```
Organization (Namespace, resource-type=organization)
  └── Folder (Namespace, resource-type=folder)     // optional, up to 3 levels
        └── Folder (Namespace, resource-type=folder)
              └── Project (Namespace, resource-type=project)
```

A Folder is a Namespace with `console.holos.run/resource-type: folder` and a
parent reference via `console.holos.run/parent: {parent-namespace}`. Projects
reference their parent folder (or org, if no folders exist) via the same label.
The hierarchy depth is limited to 3 folder levels between an organization and a
project. This limit is based on the experience of Google Cloud IAM, where
hierarchies deeper than 3 levels become difficult to comprehend and reason
about. In practice, it is rare for organizations to need more than 3 levels of
folder hierarchy for project resources. Deeper hierarchies also increase load on
the Kubernetes API server — each level in the walk requires a Namespace read.

This hierarchy is traversed at template evaluation time to collect and unify
templates from every level. It is also traversed at authorization time to
resolve effective permissions (see ADR 015).

The folder concept is planned for `v1alpha2`. The `v1alpha1` schema defines the
Organization and Project types; the Folder type and `folderInput` are deferred
to validate extensibility in `v1alpha2`.

### 5. Input is split into platformInput and projectInput.

Templates receive input from two scopes with different trust levels:

```go
// PlatformInput carries values set by platform engineers and the system.
// Template authors can rely on these values being verified by the backend.
// In CUE templates, these fields are available under the "platform" top-level
// identifier (e.g. platform.namespace, platform.claims.email).
type PlatformInput struct {
    // Project is the parent project name, resolved from the authenticated session.
    Project          string `json:"project"          yaml:"project"          cue:"project"`
    // Namespace is the Kubernetes namespace for the project, resolved by the backend.
    Namespace        string `json:"namespace"        yaml:"namespace"        cue:"namespace"`
    // GatewayNamespace is the namespace of the ingress gateway (default: "istio-ingress").
    GatewayNamespace string `json:"gatewayNamespace" yaml:"gatewayNamespace" cue:"gatewayNamespace"`
    // Organization is the parent organization name.
    Organization     string `json:"organization"     yaml:"organization"     cue:"organization"`
    // Claims carries the OIDC ID token claims of the authenticated user.
    Claims           Claims `json:"claims"           yaml:"claims"           cue:"claims"`
}

// ProjectInput carries values provided by the product engineer via the
// deployment form. Template authors should treat these as user-supplied and
// validate them with CUE constraints.
// In CUE templates, these fields are available under the "input" top-level
// identifier (e.g. input.name, input.image).
type ProjectInput struct {
    // Name is the deployment name. Must be a valid DNS label.
    Name    string      `json:"name"              yaml:"name"              cue:"name"`
    // Image is the container image repository (e.g. "ghcr.io/example/app").
    Image   string      `json:"image"             yaml:"image"             cue:"image"`
    // Tag is the image tag (e.g. "v1.2.3").
    Tag     string      `json:"tag"               yaml:"tag"               cue:"tag"`
    // Command overrides the container ENTRYPOINT.
    Command []string    `json:"command,omitempty"  yaml:"command,omitempty" cue:"command?"`
    // Args overrides the container CMD.
    Args    []string    `json:"args,omitempty"     yaml:"args,omitempty"    cue:"args?"`
    // Env defines container environment variables.
    Env     []EnvVar    `json:"env,omitempty"      yaml:"env,omitempty"     cue:"env?"`
    // Port is the container port the application listens on (default: 8080).
    Port    int         `json:"port"               yaml:"port"              cue:"port"`
}
```

This extends the `system`/`input` split from ADR 013. The naming changes from
`system` to `platform` and from `input` to `input` (unchanged) to align with
the resource collection naming (`platformResources`/`projectResources`) and to
make clear that platform-level values are not just "system" values but
configuration set by platform engineers.

**Migration note**: The existing `system` top-level CUE identifier is renamed to
`platform`. Since the code is pre-release, this is a one-time migration with no
user impact.

### 6. Output is split into platformResources and projectResources.

Resource collections are segregated by who controls them and where they are
applied:

```go
// PlatformResources holds resources managed by platform and security engineers.
// These resources typically live outside the project namespace (e.g., in the
// gateway namespace or at cluster scope) or are platform-mandated resources
// within the project namespace that project templates cannot override.
//
// A template defined at the project level cannot write to platformResources.
// Only templates at the folder level or above can contribute to this collection.
// This is the key RBAC boundary: it ensures a product engineer's template
// cannot inject an HTTPRoute into the gateway namespace or modify a
// NetworkPolicy set by the platform team.
type PlatformResources struct {
    // NamespacedResources maps namespace -> kind -> name -> resource manifest.
    NamespacedResources map[string]map[string]map[string]Resource `json:"namespacedResources,omitempty"`
    // ClusterResources maps kind -> name -> resource manifest.
    ClusterResources    map[string]map[string]Resource             `json:"clusterResources,omitempty"`
}

// ProjectResources holds resources managed by product engineers.
// These resources live within the project namespace. A project-level template
// writes to this collection.
//
// Platform templates at the folder level or above can also constrain
// projectResources — for example, requiring a label on every Deployment —
// but cannot replace or remove resources defined by the project template.
// CUE unification enforces this: constraints add requirements, they do not
// delete existing structure.
type ProjectResources struct {
    // NamespacedResources maps namespace -> kind -> name -> resource manifest.
    NamespacedResources map[string]map[string]map[string]Resource `json:"namespacedResources,omitempty"`
    // ClusterResources maps kind -> name -> resource manifest.
    ClusterResources    map[string]map[string]Resource             `json:"clusterResources,omitempty"`
}

// Resource is an unstructured Kubernetes resource manifest.
type Resource map[string]interface{}
```

This replaces the current four-field output (`output.namespacedResources`,
`output.clusterResources`, `output.systemNamespacedResources`,
`output.systemClusterResources`) from ADR 012 with a cleaner two-collection
model. The names `platformResources` and `projectResources` communicate intent
to template authors who are not familiar with Kubernetes concepts:

- **projectResources** — "resources for my project" (Deployments, Services,
  ServiceAccounts that run my app)
- **platformResources** — "resources for the platform" (HTTPRoutes in the
  gateway namespace, NetworkPolicies, ReferenceGrants)

### 7. Templates do not declare a CUE package clause.

User-authored templates are plain CUE source without a `package` declaration.
The Go renderer controls the package name by prepending it before compilation
or by assigning files to a `build.Instance` where it sets the package.

Template authors write:

```cue
// No package clause — the renderer handles this.

_labels: {
    "app.kubernetes.io/name":       input.name
    "app.kubernetes.io/managed-by": "console.holos.run"
}

projectResources: (platform.namespace): {
    Deployment: (input.name): { ... }
}
```

They do **not** write `package deployment` or any other package declaration.

**Rationale.** The CUE language spec makes the package clause optional — files
without one are valid CUE and can be compiled with `cue.Context.CompileString`,
`CompileBytes`, or added to a `build.Instance` via `AddFile`. The
`cue.Value.Unify` operation is package-agnostic: it works regardless of whether
the source values had package clauses, different package names, or no package at
all. This means the Go renderer can compile each template independently and
unify the results without requiring templates to agree on (or even declare) a
package name.

Keeping the package clause out of user-authored templates has three benefits:

1. **Simpler authoring.** Template authors do not need to know what CUE packages
   are or remember which package name to use. One less thing to get wrong.

2. **Renderer control.** The Go code owns the package namespace. If the internal
   compilation strategy changes (e.g., switching from string concatenation to
   `build.Instance.AddFile`, or changing the package name), no user templates
   need to be updated.

3. **Eliminates a current pain point.** The existing codebase has a
   `stripPackageDecl` function in `console/deployments/render.go` that removes
   `package deployment` lines before concatenating system templates with
   deployment templates. Removing the requirement at the source eliminates this
   workaround entirely.

### 8. CUE unification merges templates from all hierarchy levels.

At evaluation time, the console collects templates from every level in the
hierarchy (organization, folders, project) and unifies them into a single CUE
value. CUE's unification operation is commutative, associative, and idempotent —
the order of template collection does not affect the result.

```
Organization templates   ──┐
  Folder-1 templates     ──┤
    Folder-2 templates   ──┤  CUE unification  ──►  single evaluated value
      Project template   ──┘
```

Every template can reference `platform.*` and `input.*` fields. Templates from
different levels write to different output collections and may constrain
each other:

| Template level  | Writes to             | Can constrain          |
|-----------------|-----------------------|------------------------|
| Organization    | platformResources     | projectResources       |
| Folder          | platformResources     | projectResources       |
| Project         | projectResources      | (nothing above it)     |

**Writing** means defining concrete resources (a Deployment, a Service, an
HTTPRoute). **Constraining** means adding CUE type constraints that resources
defined at a lower level must satisfy — for example, closing the set of allowed
Kinds, requiring a label, or setting a minimum replica count.

Organization and folder templates can explicitly unify with `projectResources`
to add constraints. This is the mechanism by which platform engineers control
what project-level templates are allowed to produce (see Decision 9).

The Go renderer reads each collection from the appropriate CUE path after
unification. A project-level template that attempts to define
`platformResources` fields has no effect because the renderer does not read
`platformResources` from the project template's contribution. This is a hard
boundary enforced by the renderer, not by CUE.

**Constraint flow is one-directional**: higher-level templates can add CUE
constraints to `projectResources` (e.g., requiring a label on all Deployments,
restricting allowed Kinds) but project templates cannot constrain
`platformResources`. This is enforced by CUE evaluation order in the renderer.

### 9. Platform templates close the projectResources struct to restrict allowed resource kinds.

A product engineer's project template can define any CUE value under
`projectResources`. Without constraints, nothing prevents a project template
from producing a `ClusterRole`, a `ClusterRoleBinding`, or any other dangerous
resource kind. The Go renderer validates allowed kinds after evaluation (see
the allowed kinds list in `console/deployments/apply.go`), but that is a
last-resort safety net — errors at apply time are late and opaque compared to
errors at CUE evaluation time.

Platform templates solve this by closing the `projectResources` struct at the
organization or folder level. A closed struct in CUE rejects any field not
explicitly allowed. When a platform template closes `projectResources` to a
specific set of Kind keys, any project template that tries to add a resource of
a disallowed Kind fails at CUE evaluation time with a clear error — before any
Kubernetes API call.

**Example: restricting project resources to safe kinds.** An organization-level
platform template defines the allowed Kind keys:

```cue
// Platform template (org level): close projectResources to safe kinds only.
//
// This constraint is unified with the project template at evaluation time.
// If a project template defines a Kind not listed here, CUE evaluation fails.

import "list"

// _allowedKinds defines the set of resource kinds that project templates may
// produce. Closing the struct to these kinds prevents project authors from
// creating dangerous resources like ClusterRole or ClusterRoleBinding.
_allowedKinds: ["ConfigMap", "Deployment", "Secret", "Service", "ServiceAccount"]

projectResources: [_]: {
    for kind in _allowedKinds {
        (kind): _
    }
}
```

With this constraint in place, a project template that tries to produce a
`ClusterRoleBinding`:

```cue
projectResources: (platform.namespace): {
    ClusterRoleBinding: "escalate": { ... }
}
```

fails at CUE evaluation time:

```
projectResources.<ns>.ClusterRoleBinding: field not allowed
```

**Why CUE-level enforcement matters.** The Go renderer's allowed-kinds
validation (in `apply.go`) is a hard safety net that catches any resource kind
the renderer does not know how to apply. But it operates after CUE evaluation
completes — the template author sees a Go-level error, not a CUE-level error.
By closing the struct in a platform template, the restriction is visible in the
CUE schema, reported as a CUE evaluation error with the exact field path, and
testable in the template preview RPC before any deployment. It also means the
allowed set is configurable per-organization or per-folder, not hardcoded in Go.

**Layered enforcement.** The two enforcement points are complementary:

| Layer            | What it enforces                          | When it runs       |
|------------------|-------------------------------------------|--------------------|
| Platform template (CUE) | Allowed Kind keys in `projectResources`, configurable per org/folder | CUE evaluation time |
| Go renderer (`apply.go`) | Hard-coded Kind allowlist and GVR mapping | After CUE evaluation, before Kubernetes apply |

A Kind must pass both layers. The CUE constraint is the primary control —
platform engineers manage it. The Go allowlist is the fallback — it catches
anything the CUE constraint missed (e.g., if no platform template is defined)
and ensures the renderer has a GVR mapping for every Kind it applies.

### 10. The top-level ResourceSet type composes all of the above.

```go
// ResourceSet is the top-level resource type for the configuration management
// API. It represents the complete set of Kubernetes resources produced by
// unifying templates from all hierarchy levels with their inputs.
//
// A ResourceSet is not specific to applications — it can hold any combination
// of resources: Deployments and Services for an app, Datadog dashboard
// ConfigMaps for observability, NetworkPolicies for security, Argo Rollouts
// for progressive delivery, or any other set of resources managed together.
//
// At evaluation time, the console constructs a ResourceSet by:
//  1. Filling PlatformInput from authenticated server context.
//  2. Filling ProjectInput from the API request.
//  3. Collecting templates from every hierarchy level.
//  4. Unifying all templates with the filled inputs via CUE.
//  5. Reading PlatformResources and ProjectResources from the evaluated value.
//  6. Validating and applying the resources to Kubernetes.
type ResourceSet struct {
    TypeMeta `json:",inline" yaml:",inline"`
    Metadata Metadata        `json:"metadata" yaml:"metadata" cue:"metadata"`
    Spec     ResourceSetSpec `json:"spec"     yaml:"spec"     cue:"spec"`
}

// ResourceSetSpec groups the input and output sections of a ResourceSet.
type ResourceSetSpec struct {
    // PlatformInput is the trusted context set by the backend and platform engineers.
    PlatformInput     PlatformInput     `json:"platformInput"     yaml:"platformInput"     cue:"platformInput"`
    // ProjectInput is the user-provided deployment parameters.
    ProjectInput      ProjectInput      `json:"projectInput"      yaml:"projectInput"      cue:"projectInput"`
    // PlatformResources is the output collection for platform-managed resources.
    PlatformResources PlatformResources `json:"platformResources" yaml:"platformResources" cue:"platformResources"`
    // ProjectResources is the output collection for project-managed resources.
    ProjectResources  ProjectResources  `json:"projectResources"  yaml:"projectResources"  cue:"projectResources"`
}
```

### 11. The Renderer interface is version-agnostic.

The consumer package defines a `Renderer` interface that all versioned types
must satisfy:

```go
// Renderer evaluates a ResourceSet and returns the resource collections.
// Each api/v1alpha* ResourceSet type implements this interface. The consumer
// dispatches to the correct implementation based on TypeMeta.
type Renderer interface {
    // Render evaluates the configuration and returns platform and project
    // resource collections. The caller supplies the filled inputs; the
    // Renderer performs CUE evaluation and validation.
    Render() (*ResourceOutput, error)
}

// ResourceOutput is the version-agnostic result of rendering.
// It uses maps rather than version-specific structs so that the consumer
// does not need to know which API version produced the output.
type ResourceOutput struct {
    PlatformNamespacedResources map[string]map[string]map[string]Resource
    PlatformClusterResources    map[string]map[string]Resource
    ProjectNamespacedResources  map[string]map[string]map[string]Resource
    ProjectClusterResources     map[string]map[string]Resource
}
```

Discrimination happens once at the API boundary: the consumer reads `TypeMeta`
from the serialized configuration, selects the correct versioned type, and calls
`Render()`. All subsequent processing uses the version-agnostic
`ResourceOutput`.

### 12. Package layout.

```
api/
  v1alpha1/
    doc.go           // package doc with rationale and usage examples
    types.go         // TypeMeta, Metadata, ResourceSet, ResourceSetSpec
    input.go         // PlatformInput, ProjectInput, Claims, EnvVar
    resources.go     // PlatformResources, ProjectResources, Resource
    iam.go           // Role, Permission, Principal, Grant (see ADR 015)
    hierarchy.go     // Organization, Folder, Project
    annotations.go   // annotation and label constants
    types_test.go    // CUE round-trip validation tests
  v1alpha2/          // future: adds Folder, securityResources, folderInput
```

## Consequences

### Positive

- **Single source of truth.** Go structs define the schema; CUE schemas and
  proto messages are generated or aligned mechanically. Schema drift between the
  Go renderer and CUE evaluator is eliminated.

- **Version discrimination.** `apiVersion`/`kind` on every top-level type means
  the consumer can dispatch without inspecting field shapes. Adding `v1alpha2`
  types is additive — the consumer learns the new version without modifying old
  code.

- **Clear resource segregation.** `platformResources` and `projectResources`
  are separate fields with separate RBAC rules (ADR 015). A product engineer
  who writes a project template cannot accidentally or intentionally inject
  resources into `platformResources`.

- **Hierarchy-ready.** The schema accommodates the folder concept from the
  start, even though `v1alpha1` only implements Organization -> Project. The
  `v1alpha2` extension path is defined (Decision 4) rather than discovered
  retroactively.

- **Readable names.** `platformResources` and `projectResources` communicate
  intent to engineers who are not Kubernetes experts. "Platform resources" means
  "things the platform team manages"; "project resources" means "things my
  project needs".

### Negative

- **Two schema systems.** Go structs with CUE tags and proto messages are both
  maintained. This is intentional — they serve different boundaries (template
  evaluation vs. RPC) — but it is additional surface area.

- **Naming migration.** The existing `system`/`input` CUE identifiers and
  `output.systemNamespacedResources`/`output.systemClusterResources` paths must
  be renamed to `platform`/`input` and `platformResources`/`projectResources`.
  Since the code is pre-release, this is a one-time cost.

### Risks

- **CUE tag fidelity.** `cue get go` runs at build time via `go generate` to
  produce `.cue` files from the Go struct tags. These generated CUE files are
  embedded into the binary and used for unification at runtime. If a `cue` tag
  constraint diverges from the Go type (e.g., a `cue` tag adds a regex that
  the Go code does not enforce), the CUE evaluation may reject inputs that
  the Go code would accept, or vice versa. Mitigated by round-trip tests in
  `types_test.go` that marshal Go values to JSON and validate them against
  the generated CUE schema, and by `make generate` catching generation
  failures before they reach a build.


## References

- [ADR 012: Structured Resource Output for CUE Templates](012-structured-resource-output.md)
- [ADR 013: Separate System and User Input Trust Boundary](013-separate-system-user-template-input.md)
- [ADR 007: Organization Grants Do Not Cascade](007-org-grants-no-cascade.md)

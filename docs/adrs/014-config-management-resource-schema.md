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

## Decisions

### 1. Go structs with CUE struct tags define the template API contract.

The template interface is defined as Go structs in a versioned `api/` package.
Each struct field carries three tags — `json`, `yaml`, and `cue` — following the
pattern established by `holos-run/holos` in `api/core/v1alpha6/types.go`:

```go
type Platform struct {
    TypeMeta `json:",inline" yaml:",inline"`
    Metadata Metadata          `json:"metadata"          yaml:"metadata"          cue:"metadata"`
    Spec     PlatformSpec      `json:"spec"              yaml:"spec"              cue:"spec"`
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
    // Kind is the resource type name, e.g. "Platform".
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
project.

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

### 7. CUE unification merges templates from all hierarchy levels.

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

Each template declares `package deployment` and can reference `platform.*` and
`input.*` fields. Templates from different levels contribute to different output
collections based on their level:

| Template level  | Can write to          | Cannot write to        |
|-----------------|-----------------------|------------------------|
| Organization    | platformResources     | projectResources       |
| Folder          | platformResources     | projectResources       |
| Project         | projectResources      | platformResources      |

This separation is enforced by the Go renderer, which reads each collection
from the appropriate CUE path. A project-level template that attempts to define
`platformResources` fields has no effect because the renderer does not read
`platformResources` from the project template's contribution.

**Constraint flow is one-directional**: higher-level templates can add CUE
constraints to `projectResources` (e.g., requiring a label on all Deployments)
but project templates cannot constrain `platformResources`. This is also
enforced by CUE evaluation order in the renderer.

### 8. The top-level Platform type composes all of the above.

```go
// Platform is the top-level resource type for the configuration management API.
// It represents the complete evaluated state of a deployment: inputs from the
// platform and the product engineer, and the resulting resource collections.
//
// At evaluation time, the console constructs a Platform value by:
//  1. Filling PlatformInput from authenticated server context.
//  2. Filling ProjectInput from the API request.
//  3. Collecting templates from every hierarchy level.
//  4. Unifying all templates with the filled inputs via CUE.
//  5. Reading PlatformResources and ProjectResources from the evaluated value.
//  6. Validating and applying the resources to Kubernetes.
type Platform struct {
    TypeMeta `json:",inline" yaml:",inline"`
    Metadata Metadata `json:"metadata" yaml:"metadata" cue:"metadata"`
    Spec     PlatformSpec `json:"spec" yaml:"spec" cue:"spec"`
}

// PlatformSpec groups the input and output sections of a Platform resource.
type PlatformSpec struct {
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

### 9. The Renderer interface is version-agnostic.

The consumer package defines a `Renderer` interface that all versioned types
must satisfy:

```go
// Renderer evaluates a configuration and returns the resource collections.
// Each api/v1alpha* type implements this interface. The consumer dispatches
// to the correct implementation based on TypeMeta.
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

### 10. Package layout.

```
api/
  v1alpha1/
    doc.go           // package doc with rationale and usage examples
    types.go         // TypeMeta, Metadata, Platform, PlatformSpec
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

- **CUE tag fidelity.** The `cue` struct tag is interpreted at runtime by `cue
  get go`. If a CUE tag constraint diverges from the Go type (e.g., a `cue`
  tag adds a regex that the Go code does not enforce), the CUE evaluation may
  reject inputs that the Go code would accept, or vice versa. Mitigated by
  round-trip tests in `types_test.go` that marshal Go values to JSON and
  validate them against the generated CUE schema.

- **Folder depth limit.** The 3-folder limit is arbitrary. If a deep hierarchy
  is needed, the limit must be raised, which changes traversal cost and RBAC
  evaluation complexity. The limit can be increased in a future version without
  schema changes.

## References

- [ADR 012: Structured Resource Output for CUE Templates](012-structured-resource-output.md)
- [ADR 013: Separate System and User Input Trust Boundary](013-separate-system-user-template-input.md)
- [ADR 007: Organization Grants Do Not Cascade](007-org-grants-no-cascade.md)
- [Issue #509](https://github.com/holos-run/holos-console/issues/509) — parent plan
- [Issue #510](https://github.com/holos-run/holos-console/issues/510) — v1alpha1 package skeleton
- [Issue #511](https://github.com/holos-run/holos-console/issues/511) — IAM types
- [Issue #512](https://github.com/holos-run/holos-console/issues/512) — resource collection types

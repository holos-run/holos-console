# ADR 018: CUE Template Default Values

## Status

Accepted

## Context

Template default values (image, tag, port, name, description, etc.) are currently stored as
proto-level metadata (`DeploymentDefaults`) in ConfigMap annotations, separate from the CUE
template source. This creates two sources of truth: the CUE template defines the rendering
logic, but defaults live outside it in an annotation. Template authors cannot express defaults
natively in CUE, and users cannot override them at the CUE layer.

Additionally, the `ResourceSetSpec` Go type (ADR 016) does not carry a `Defaults` field,
meaning the CUE schema generated from Go types has no canonical place for template authors to
declare default values. Adding a `Defaults *ProjectInput` field to `ResourceSetSpec` gives
template authors a typed, schema-validated way to declare defaults inside the CUE template
source itself.

The `ProjectInput` type also lacks a `Description` field, so templates cannot supply a
short description that pre-fills the deployment creation form. Adding an optional
`Description` field rounds out the input schema.

## Decisions

### 1. Add `Defaults *ProjectInput` field to `ResourceSetSpec`.

`ResourceSetSpec` in `api/v1alpha1/types.go` gains a pointer field:

```go
// ResourceSetSpec groups the input and output sections of a ResourceSet.
type ResourceSetSpec struct {
    // Defaults carries template-level default values for ProjectInput fields.
    // Template authors populate this field in CUE to declare the default image,
    // tag, port, name, description, etc. for their template. The backend reads
    // this field after CUE evaluation to populate DeploymentDefaults in proto
    // responses, which the frontend uses to pre-fill the Create Deployment form.
    //
    // Example (go-httpbin):
    //
    //  defaults: #ProjectInput & {
    //      name:        "httpbin"
    //      image:       "ghcr.io/mccutchen/go-httpbin"
    //      tag:         "2.21.0"
    //      description: "A simple HTTP Request & Response Service"
    //      port:        8080
    //  }
    Defaults          *ProjectInput     `json:"defaults,omitempty"    yaml:"defaults,omitempty"    cue:"defaults?"`
    // PlatformInput is the trusted context set by the backend and platform engineers.
    PlatformInput     PlatformInput     `json:"platformInput"         yaml:"platformInput"         cue:"platformInput"`
    // ProjectInput is the user-provided deployment parameters.
    ProjectInput      ProjectInput      `json:"projectInput"          yaml:"projectInput"          cue:"projectInput"`
    // PlatformResources is the output collection for platform-managed resources.
    PlatformResources PlatformResources `json:"platformResources"     yaml:"platformResources"     cue:"platformResources"`
    // ProjectResources is the output collection for project-managed resources.
    ProjectResources  ProjectResources  `json:"projectResources"      yaml:"projectResources"      cue:"projectResources"`
}
```

`cue get go` regenerates `api/v1alpha1/` CUE schema from these types, so the
`#ResourceSetSpec.defaults` field is available to template authors immediately after
`make generate`.

### 2. Template `defaults` field carries concrete values, not CUE default syntax.

The `defaults` block in a template holds concrete, closed values. It does **not** use the
`*value | _` CUE default syntax — that is the role of the `input` field (Decision 3).

```cue
defaults: #ProjectInput & {
    name:        "httpbin"
    image:       "ghcr.io/mccutchen/go-httpbin"
    tag:         "2.21.0"
    description: "A simple HTTP Request & Response Service"
    port:        8080
}
```

This keeps `defaults` as a plain data block that the backend can extract with
`value.LookupPath(cue.ParsePath("defaults"))` after evaluation.

### 3. Template `input` field uses CUE default syntax to wire defaults as overridable.

The `input` field uses CUE's `*preferred | alternative` syntax to make every default
overridable by user-supplied values:

```cue
input: #ProjectInput & {
    name:        *defaults.name        | _
    image:       *defaults.image       | _
    tag:         *defaults.tag         | _
    description: *defaults.description | _
    port:        *defaults.port        | _
}
```

At render time, the backend calls `value.FillPath(cue.ParsePath("input"), userInput)` to
unify the user-supplied `ProjectInput` with the `input` field. CUE's default mechanism
ensures that if a field is missing from `userInput` (i.e., left at zero value), the
`*defaults.field` wins. If a field is explicitly set by the user, the user-supplied value
wins because FillPath writes a concrete value, which unifies without ambiguity.

### 4. Backend extracts `defaults` from CUE evaluation to populate proto `DeploymentDefaults`.

After compiling and partially evaluating the template CUE source (without filling `input`),
the backend reads the `defaults` field:

```go
defaultsVal := value.LookupPath(cue.ParsePath("defaults"))
```

It then marshals `defaultsVal` into a `ProjectInput` struct and maps the fields to the
`DeploymentDefaults` proto message. This value is returned in `GetDeploymentTemplate` and
`ListDeploymentTemplates` responses so the frontend can pre-fill the Create Deployment form
without making a second render round-trip.

If the template has no `defaults` block (e.g., legacy templates), the extraction returns
empty and the frontend falls back to its current zero-value defaults. This preserves
backwards compatibility.

### 5. Annotation-stored defaults remain as fallback for existing templates.

The existing mechanism that reads `DeploymentDefaults` from ConfigMap annotations is not
removed. For templates that predate this ADR (no `defaults` block), the annotation values
continue to populate `DeploymentDefaults`. For templates that have a `defaults` block, the
CUE-extracted values take precedence over annotation values.

This ensures zero disruption to existing deployed templates.

### 6. 1:1 template-to-deployment mapping (like CloudFormation stacks) — defer multi-template composition.

A deployment is associated with exactly one project template. Multi-template composition
(combining multiple project templates into a single deployment) is deferred. This
simplification mirrors CloudFormation stacks: one stack definition, one stack instance.

The platform template mechanism (ADR 016, ADR 017) already handles cross-cutting concerns
at the organization level without requiring multi-template composition at the project level.

### 7. Add `Description` field to `ProjectInput`.

`ProjectInput` in `api/v1alpha1/types.go` gains an optional `Description` field:

```go
// Description is a short human-readable description of the deployment.
// Template authors can set this in the defaults block to pre-fill the
// Create Deployment form. Users may override it at deploy time.
Description string `json:"description,omitempty" yaml:"description,omitempty" cue:"description?"`
```

This field is optional (pointer-less, omitempty) so that existing templates that do not
set `description` continue to evaluate correctly. The CUE tag `description?` marks it as
an optional field in the generated CUE schema.

### 8. Add `name` and `description` fields to `DeploymentDefaults` proto.

`DeploymentDefaults` in `proto/holos/console/v1/deployment_templates.proto` gains two
fields:

```proto
message DeploymentDefaults {
  string name        = 1;  // existing
  string image       = 2;  // existing
  string tag         = 3;  // existing
  int32  port        = 4;  // existing
  // New fields:
  string description = 5;
}
```

These are backwards-compatible additions. Existing clients that do not read `name` or
`description` are unaffected.

## CUE Example

The following shows the complete pattern in a deployment template:

```cue
// defaults declares the template's default values as concrete CUE data.
// The backend reads this block to pre-fill the Create Deployment form.
defaults: #ProjectInput & {
    name:        "httpbin"
    image:       "ghcr.io/mccutchen/go-httpbin"
    tag:         "2.21.0"
    description: "A simple HTTP Request & Response Service"
    port:        8080
}

// input wires defaults as overridable via CUE's default syntax.
// User-supplied values from the deployment form override these defaults
// at render time via CUE unification (FillPath at "input" path).
input: #ProjectInput & {
    name:        *defaults.name        | _
    image:       *defaults.image       | _
    tag:         *defaults.tag         | _
    description: *defaults.description | _
    port:        *defaults.port        | _
}

_labels: {
    "app.kubernetes.io/name":       input.name
    "app.kubernetes.io/managed-by": "console.holos.run"
}

projectResources: (platform.namespace): {
    ServiceAccount: (input.name): {
        apiVersion: "v1"
        kind:       "ServiceAccount"
        metadata: {
            name:      input.name
            namespace: platform.namespace
            labels:    _labels
        }
    }
    Deployment: (input.name): {
        apiVersion: "apps/v1"
        kind:       "Deployment"
        metadata: {
            name:      input.name
            namespace: platform.namespace
            labels:    _labels
        }
        spec: {
            selector: matchLabels: _labels
            template: {
                metadata: labels: _labels
                spec: containers: [{
                    name:  input.name
                    image: "\(input.image):\(input.tag)"
                    ports: [{containerPort: input.port}]
                }]
            }
        }
    }
}
```

## Consequences

### Positive

- **Single source of truth.** Template defaults live in the CUE source alongside the
  rendering logic. A template author reads one file to understand both what the template
  produces and what the recommended inputs are.

- **CUE-native overrides.** Because defaults are wired through `*defaults.field | _`,
  users can override them in the standard CUE way. The override mechanism is visible in
  the template source — no hidden Go-level override logic.

- **Form pre-fill without a render round-trip.** The backend extracts `defaults` at read
  time (`GetDeploymentTemplate`, `ListDeploymentTemplates`) and returns them in
  `DeploymentDefaults`. The frontend pre-fills the Create Deployment form without
  triggering a full CUE render, which keeps the UI fast.

- **Backwards compatible.** Templates without a `defaults` block continue to work exactly
  as before. Annotation-stored defaults remain as a fallback.

- **Schema-validated.** Because `Defaults` is typed as `*ProjectInput`, the generated CUE
  schema constrains the `defaults` block to valid `#ProjectInput` fields. Template authors
  get CUE evaluation errors for typos or wrong types at preview time, not at deploy time.

### Negative

- **CUE evaluation cost at read time.** Extracting `defaults` requires compiling and
  partially evaluating the template CUE source for every `GetDeploymentTemplate` and
  `ListDeploymentTemplates` call. For templates stored in-cluster this is a millisecond-
  scale operation, but it is a new cost that did not exist before. Mitigated by keeping
  templates small and deferring caching to a future iteration.

- **Two extraction paths.** For a transition period, the backend must check both the CUE
  `defaults` block and the ConfigMap annotation. This is a small amount of fallback logic
  that can be removed once all templates are migrated.

## References

- [ADR 016: Configuration Management Resource Schema](016-config-management-resource-schema.md)
- [ADR 017: Configuration Management RBAC Levels](017-config-management-rbac-levels.md)
- [ADR 013: Separate System and User Input Trust Boundary](013-separate-system-user-template-input.md)
- [Issue #566: CUE-based template default values and searchable template selection](https://github.com/holos-run/holos-console/issues/566)

# CUE Deployment Template Guide

This guide covers the interface between deployment fields and CUE templates in
holos-console. It explains the inputs the console provides, the output structure
the console reads, and the validation constraints enforced at render time.

## Overview

When a user creates or updates a deployment, the console:

1. Loads the CUE template source from a `DeploymentTemplate` ConfigMap.
2. Builds a `DeploymentInput` from the API request fields.
3. Evaluates the CUE template by unifying the input with the `input` field.
4. Reads the `resources` field from the evaluated CUE value.
5. Validates each resource against safety constraints.
6. Applies the validated resources to Kubernetes via server-side apply.

## Template Input

The console fills the `input` field at render time. Every CUE template must
declare `input: #Input` and define `#Input` with at least the following fields.

### `#Input` Schema

```cue
#Input: {
    name:      string & =~"^[a-z][a-z0-9-]*$"  // DNS label, max 63 chars
    image:     string                            // container image (no tag)
    tag:       string                            // image tag
    project:   string                            // project name
    namespace: string                            // resolved K8s namespace
    command?: [...string]                        // container ENTRYPOINT override
    args?:    [...string]                        // container CMD override
    env:      [...#EnvVar] | *[]                 // environment variables
}
```

### Field Descriptions

| Field       | Type       | Required | Description |
|-------------|------------|----------|-------------|
| `name`      | `string`   | Yes      | Deployment name. Must be a valid DNS label (`^[a-z][a-z0-9-]*$`). |
| `image`     | `string`   | Yes      | Container image repository (e.g. `ghcr.io/holos-run/holos-console`). |
| `tag`       | `string`   | Yes      | Image tag (e.g. `v1.2.3`, `latest`). |
| `project`   | `string`   | Yes      | Parent project name. |
| `namespace` | `string`   | Yes      | Kubernetes namespace resolved from the project name. Not user-supplied; computed by the server using `Resolver.ProjectNamespace()`. |
| `command`   | `[...string]` | No   | Overrides the container `ENTRYPOINT`. Omitted when not set. |
| `args`      | `[...string]` | No   | Overrides the container `CMD`. Omitted when not set. |
| `env`       | `[...#EnvVar]` | No  | Container environment variables. Defaults to `[]`. |

### `#EnvVar` Schema

Each environment variable has exactly one value source:

```cue
#EnvVar: {
    name:               string
    value?:             string       // literal value
    secretKeyRef?:      #KeyRef      // reference to a K8s Secret key
    configMapKeyRef?:   #KeyRef      // reference to a K8s ConfigMap key
}

#KeyRef: {
    name: string   // Secret or ConfigMap name
    key:  string   // key within the resource
}
```

## Template Output

The console reads a single field from the evaluated CUE template:

### `resources` Field

```cue
resources: [...{...}]
```

The `resources` field must be a list of Kubernetes resource manifests. Each
element is a struct with standard Kubernetes fields (`apiVersion`, `kind`,
`metadata`, `spec`, etc.).

The console iterates this list, validates each resource, then applies them to
the cluster. **This is the only output field the console reads.** All other
top-level fields (helper definitions, intermediate values) are ignored.

### Example Output Structure

The default template produces three resources:

```cue
resources: [
    {
        apiVersion: "v1"
        kind:       "ServiceAccount"
        metadata: {
            name:      input.name
            namespace: input.namespace
            labels:    _labels
        }
    },
    {
        apiVersion: "apps/v1"
        kind:       "Deployment"
        metadata: { ... }
        spec: { ... }
    },
    {
        apiVersion: "v1"
        kind:       "Service"
        metadata: { ... }
        spec: { ... }
    },
]
```

## Validation Constraints

Every resource in the `resources` list must satisfy these constraints or the
render is rejected. These are enforced in Go after CUE evaluation, not in CUE
itself.

### Required Fields

Each resource must have:
- `apiVersion` — non-empty
- `kind` — non-empty and in the allowed set
- `metadata.name` — non-empty
- `metadata.namespace` — must exactly match `input.namespace`

### Allowed Resource Kinds

Templates may only produce resources of these kinds:

| Kind             | API Group                     |
|------------------|-------------------------------|
| `Deployment`     | `apps/v1`                     |
| `Service`        | `v1`                          |
| `ServiceAccount` | `v1`                          |
| `Role`           | `rbac.authorization.k8s.io/v1`|
| `RoleBinding`    | `rbac.authorization.k8s.io/v1`|
| `HTTPRoute`      | `gateway.networking.k8s.io/v1`|
| `ConfigMap`      | `v1`                          |
| `Secret`         | `v1`                          |

### Required Labels

Every resource must carry:

```yaml
app.kubernetes.io/managed-by: "console.holos.run"
```

The console additionally injects an ownership label after validation:

```yaml
console.holos.run/deployment: "<deployment-name>"
```

This label is used for cleanup when a deployment is deleted.

### Evaluation Timeout

CUE template evaluation is capped at **5 seconds**. Templates that exceed this
limit are rejected.

## Writing a Custom Template

### Minimal Template

```cue
package deployment

#KeyRef: {
    name: string
    key:  string
}

#EnvVar: {
    name:               string
    value?:             string
    secretKeyRef?:      #KeyRef
    configMapKeyRef?:   #KeyRef
}

#Input: {
    name:      string & =~"^[a-z][a-z0-9-]*$"
    image:     string
    tag:       string
    project:   string
    namespace: string
    command?: [...string]
    args?: [...string]
    env: [...#EnvVar] | *[]
}

input: #Input

_labels: {
    "app.kubernetes.io/name":       input.name
    "app.kubernetes.io/managed-by": "console.holos.run"
}

resources: [
    {
        apiVersion: "apps/v1"
        kind:       "Deployment"
        metadata: {
            name:      input.name
            namespace: input.namespace
            labels:    _labels
        }
        spec: {
            replicas: 1
            selector: matchLabels: "app.kubernetes.io/name": input.name
            template: {
                metadata: labels: _labels
                spec: containers: [{
                    name:  input.name
                    image: input.image + ":" + input.tag
                }]
            }
        }
    },
]
```

### Guidelines

1. **Always declare `package deployment`** — the CUE evaluator expects this package name.
2. **Always declare `input: #Input`** — this is the unification point where the console injects deployment parameters.
3. **Always include the managed-by label** on every resource or validation will reject the render.
4. **Set `metadata.namespace` to `input.namespace`** on every resource — cross-namespace resources are rejected.
5. **Use helper definitions** (prefixed with `_`) for shared values like labels, env transformation, etc. These are not exported and don't affect the output.

### Previewing Templates

Use the `RenderDeploymentTemplate` RPC to preview a template without creating a
deployment. This accepts raw CUE source and example inputs, returning the
rendered YAML. Useful for validating templates during authoring.

## Planned Extensions

### Platform Input

A second input field for platform-mandated configuration is planned. This will
allow platform teams to inject organization-wide policy (e.g., security
contexts, resource limits, network policies, sidecar containers) separately from
user-controlled deployment parameters.

The planned interface:

```cue
input:    #Input           // user-controlled deployment parameters (existing)
platform: #PlatformInput   // platform-mandated configuration (planned)
```

Template authors will be able to reference `platform` fields to apply
organization-level policy without requiring users to specify them per deployment.

### Structured Resource Output

The current `resources` field is a flat list. A planned refactoring will
organize output resources into a structured format with separate categories for
namespaced and cluster-scoped resources. See
[ADR 012](adrs/012-structured-resource-output.md) for the architectural
decision.

## Appendix: Source Code Reference

This section maps the template interface to its implementation across the
codebase. Use it for advanced troubleshooting or when developing new features.

### CUE Template Source

| File | Purpose |
|------|---------|
| `console/templates/default_template.cue` | Default CUE template with `#Input` schema, env var transformation, and resource definitions. Embedded into the Go binary via `console/templates/embed.go`. |
| `console/templates/embed.go` | `//go:embed` directive that loads `default_template.cue` as the fallback template. |

### Go Rendering Pipeline

| File | Purpose |
|------|---------|
| `console/deployments/render.go` | `CueRenderer.Render()` — compiles CUE source, marshals `DeploymentInput` to JSON, unifies via `FillPath("input")`, walks structured `namespaced`/`cluster` output fields, validates. |
| `console/deployments/render.go:44-54` | `DeploymentInput` struct — the Go representation of `#Input`, serialized to JSON for CUE unification. |
| `console/deployments/render.go` | `validateResource()` — enforces kind allowlist and managed-by label on a single resource. `evaluateStructured()` adds namespace-match and struct-key consistency checks. |
| `console/deployments/apply.go` | `Applier.Apply()` — injects ownership label, performs server-side apply with field manager `console.holos.run`. |
| `console/deployments/apply.go:96-127` | `Applier.Cleanup()` — deletes all resources matching the ownership label selector. |

### Template Service

| File | Purpose |
|------|---------|
| `console/templates/handler.go` | `DeploymentTemplateService` handler — CRUD for templates stored as ConfigMaps. |
| `console/templates/k8s.go` | ConfigMap storage: templates stored with `template.cue` data key, `deployment-template` resource-type label. |
| `console/templates/render_adapter.go` | `CueRendererAdapter` — wraps `deployments.CueRenderer` to produce YAML strings for the template preview RPC. |

### Deployment Service

| File | Purpose |
|------|---------|
| `console/deployments/handler.go:240-269` | Create flow — builds `DeploymentInput`, calls `Render()`, then `Apply()`. |
| `console/deployments/handler.go:607-656` | `protoToEnvVarInput()` / `envVarInputToProto()` — converts between protobuf `EnvVar` and `EnvVarInput` for CUE. |
| `console/deployments/k8s.go` | ConfigMap storage for deployment state: image, tag, template, command, args, env stored as data keys. |

### Protobuf Definitions

| File | Purpose |
|------|---------|
| `proto/holos/console/v1/deployments.proto` | `Deployment`, `EnvVar`, `SecretKeyRef`, `ConfigMapKeyRef` messages; `DeploymentService` RPCs. |
| `proto/holos/console/v1/deployment_templates.proto` | `DeploymentTemplate` message; `DeploymentTemplateService` RPCs including `RenderDeploymentTemplate`. |

### Generated Code

| Directory | Purpose |
|-----------|---------|
| `gen/holos/console/v1/` | Go protobuf structs (`*_pb.go`) and ConnectRPC bindings (`consolev1connect/`). |
| `frontend/src/gen/` | TypeScript protobuf types for the UI. |

### Kind-to-GVR Mapping

The `allowedKinds` map in `console/deployments/apply.go:25-34` maps each
allowed Kind to its Kubernetes `GroupVersionResource`, used for dynamic client
operations during apply and cleanup.

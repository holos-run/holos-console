# CUE Deployment Template Guide

This guide covers the interface between deployment fields and CUE templates in
holos-console. It explains how to write a custom template to deploy your app,
and provides reference material on the inputs, output structure, and validation
constraints enforced at render time.

## Overview

When a user creates or updates a deployment, the console:

1. Loads the CUE template source from a `DeploymentTemplate` ConfigMap.
2. Loads all enabled `SystemTemplate` sources for the organization.
3. Builds a `SystemInput` (project, namespace, gatewayNamespace, claims) from authenticated server context and a `UserInput` (name, image, tag, etc.) from the API request fields.
4. Unifies the deployment template with enabled system templates by concatenating them (they share the same `package deployment` declaration) before CUE compilation.
5. Fills `UserInput` at the `input` path and `SystemInput` at the `system` path.
6. Reads all four output fields from the evaluated CUE value:
   - `output.namespacedResources` — user-controlled namespaced resources
   - `output.clusterResources` — user-controlled cluster-scoped resources
   - `output.systemNamespacedResources` — operator-controlled namespaced resources (from system templates)
   - `output.systemClusterResources` — operator-controlled cluster-scoped resources (from system templates)
7. Validates each resource against safety constraints.
8. Applies the validated resources to Kubernetes via server-side apply.

The architectural decision to use structured output is recorded in
[ADR 012](adrs/012-structured-resource-output.md). The decision to split
system and user inputs is recorded in
[ADR 013](adrs/013-separate-system-user-template-input.md).

## Writing a Custom Template

This section walks through deploying a web service end-to-end: from a complete
working template to making it accessible outside the cluster.

### Complete Template Example

The template below mirrors the built-in default. It produces a `ServiceAccount`,
a `Deployment`, a `Service`, and a `ReferenceGrant` — everything needed to run a
container, reach it from inside the cluster, and allow an HTTPRoute from the
gateway namespace to reference the Service.

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
    name:    string & =~"^[a-z][a-z0-9-]*$"
    image:   string
    tag:     string
    command?: [...string]
    args?: [...string]
    env:  [...#EnvVar] | *[]
    port: int & >0 & <=65535 | *8080
}

#Claims: {
    iss:            string
    sub:            string
    exp:            int
    iat:            int
    email:          string
    email_verified: bool
    name?:          string
    groups?: [...string]
    ... // allow provider-specific claims
}

#System: {
    project:          string
    namespace:        string
    gatewayNamespace: string | *"istio-ingress"
    claims:           #Claims
}

input:  #Input
system: #System

_labels: {
    "app.kubernetes.io/name":       input.name
    "app.kubernetes.io/managed-by": "console.holos.run"
}

_annotations: {
    "console.holos.run/deployer-email": system.claims.email
}

_envSpec: [for e in input.env {
    name: e.name
    if e.value != _|_ {
        value: e.value
    }
    if e.secretKeyRef != _|_ {
        valueFrom: secretKeyRef: {
            name: e.secretKeyRef.name
            key:  e.secretKeyRef.key
        }
    }
    if e.configMapKeyRef != _|_ {
        valueFrom: configMapKeyRef: {
            name: e.configMapKeyRef.name
            key:  e.configMapKeyRef.key
        }
    }
}]

#Namespaced: [Namespace=string]: [Kind=string]: [Name=string]: {
    kind: Kind
    metadata: {
        name:      Name
        namespace: Namespace
        ...
    }
    ...
}

#Cluster: [Kind=string]: [Name=string]: {
    kind: Kind
    metadata: {
        name: Name
        ...
    }
    ...
}

// output collects all rendered Kubernetes resources.
output: {
    namespacedResources: #Namespaced & {
        (system.namespace): {
            ServiceAccount: (input.name): {
                apiVersion: "v1"
                kind:       "ServiceAccount"
                metadata: {
                    name:        input.name
                    namespace:   system.namespace
                    labels:      _labels
                    annotations: _annotations
                }
            }

            Deployment: (input.name): {
                apiVersion: "apps/v1"
                kind:       "Deployment"
                metadata: {
                    name:        input.name
                    namespace:   system.namespace
                    labels:      _labels
                    annotations: _annotations
                }
                spec: {
                    replicas: 1
                    selector: matchLabels: "app.kubernetes.io/name": input.name
                    template: {
                        metadata: labels: _labels
                        spec: {
                            serviceAccountName: input.name
                            containers: [{
                                name:  input.name
                                image: input.image + ":" + input.tag
                                if len(_envSpec) > 0 {
                                    env: _envSpec
                                }
                                ports: [{containerPort: input.port, name: "http"}]
                                if input.command != _|_ {
                                    command: input.command
                                }
                                if input.args != _|_ {
                                    args: input.args
                                }
                            }]
                        }
                    }
                }
            }

            Service: (input.name): {
                apiVersion: "v1"
                kind:       "Service"
                metadata: {
                    name:        input.name
                    namespace:   system.namespace
                    labels:      _labels
                    annotations: _annotations
                }
                spec: {
                    selector: "app.kubernetes.io/name": input.name
                    ports: [{port: 80, targetPort: "http", name: "http"}]
                }
            }

            // ReferenceGrant allows HTTPRoute resources in the gateway namespace
            // to reference Service resources in the project namespace.
            // This enables system templates (such as the example HTTPRoute template)
            // to expose deployments via the gateway without requiring changes to
            // the user deployment template.
            ReferenceGrant: "allow-gateway-httproute": {
                apiVersion: "gateway.networking.k8s.io/v1beta1"
                kind:       "ReferenceGrant"
                metadata: {
                    name:        "allow-gateway-httproute"
                    namespace:   system.namespace
                    labels:      _labels
                    annotations: _annotations
                }
                spec: {
                    from: [{
                        group:     "gateway.networking.k8s.io"
                        kind:      "HTTPRoute"
                        namespace: system.gatewayNamespace
                    }]
                    to: [{
                        group: ""
                        kind:  "Service"
                    }]
                }
            }
        }
    }
    clusterResources: #Cluster & {}
}
```

### Port Flow

The `input.port` field is the container port your application listens on
(default `8080`). The template wires it through to the `Service` so that
Kubernetes can route traffic correctly:

```
input.port  (e.g. 8080)
  → container: ports: [{containerPort: input.port, name: "http"}]
  → Service:   ports: [{port: 80, targetPort: "http", name: "http"}]
  → HTTPRoute: backendRef: {name: input.name, port: 80}   // optional, for external access
```

The container port is given the name `"http"`. The Service then targets that
named port (`targetPort: "http"`) rather than a hard-coded number, so changing
`input.port` only requires updating one place in the template.

### Networking: Cluster-Internal and External Access

**Cluster-internal access** is provided automatically by the `Service`. Once the
deployment is created, any workload inside the cluster can reach it at:

```
http://<name>.<namespace>.svc.cluster.local
```

No additional configuration is needed.

**External access** (traffic from outside the cluster) requires an `HTTPRoute`
pointing to a `Gateway`. The Gateway controller and a `Gateway` resource must
already exist in the cluster — ask your platform team for the gateway name and
namespace.

Add the following resource to your template's `output.namespacedResources` block when external
access is needed:

```cue
HTTPRoute: (input.name): {
    apiVersion: "gateway.networking.k8s.io/v1"
    kind:       "HTTPRoute"
    metadata: {
        name:        input.name
        namespace:   system.namespace
        labels:      _labels
        annotations: _annotations
    }
    spec: {
        parentRefs: [{
            name:      "prod-gateway"   // name of the Gateway in your cluster
            namespace: "infra"          // namespace where the Gateway lives
        }]
        rules: [{
            backendRefs: [{
                name: input.name
                port: 80
            }]
        }]
    }
}
```

Replace `"prod-gateway"` and `"infra"` with the actual gateway name and
namespace in your cluster. The `backendRef` port `80` matches the `Service`
port defined above — do not use `input.port` here.

`HTTPRoute` is in the allowed kinds list, so no other configuration is needed.

### Guidelines

1. **Always declare `package deployment`** — the CUE evaluator expects this package name.
2. **Always declare `input: #Input` and `system: #System`** — these are the unification points where the console injects user and system parameters respectively.
3. **Always declare `output.namespacedResources` and `output.clusterResources` output fields** — the console requires the structured output format.
4. **Always include the managed-by label** on every resource or validation will reject the render.
5. **Set `metadata.namespace` to `system.namespace`** on every namespaced resource — cross-namespace resources are rejected.
6. **Match struct keys to metadata** — `output.namespacedResources.<ns>.<Kind>.<name>` must exactly match the resource `metadata.namespace`, `kind`, and `metadata.name`.
7. **Use helper definitions** (prefixed with `_`) for shared values like labels, env transformation, etc. These are not exported and don't affect the output.
8. **Never place project or namespace in `input`** — these are system-provided values available at `system.project` and `system.namespace`.
9. **Use the named port `"http"` and Service port `80`** when adding an `HTTPRoute` — the `backendRef.port` must match the `Service` port (`80`), not the container port (`input.port`).
10. **Never define `output.systemNamespacedResources` or `output.systemClusterResources` in a user deployment template** — these fields are reserved for system templates and are managed by the platform operator.

### Previewing Your Template

Use the `RenderDeploymentTemplate` RPC to preview a template without creating a
deployment. This accepts a `cue_template` (raw CUE source) and a `cue_input`
(valid CUE source that supplies concrete values for template parameters),
returning the rendered resources as multi-document YAML (`rendered_yaml`) and as
a pretty-printed JSON array (`rendered_json`). Useful for validating templates
during authoring.

The RPC accepts two separate CUE input fields:

- `cue_system_input` — trusted system context (project, namespace, claims); populated by the backend from authenticated context when provided by the caller
- `cue_input` — user-provided deployment parameters (name, image, tag, env, etc.)

Both are valid CUE source. The backend combines them into a single document before unifying with the template, so both `system` and `input` top-level fields are available.

Example `cue_system_input` (trusted, set from authenticated context):

```cue
system: {
    project:   "my-project"
    namespace: "holos-prj-my-project"
    claims: {
        iss:            "https://dex.example.com"
        sub:            "user-123"
        exp:            9999999999
        iat:            1700000000
        email:          "user@example.com"
        email_verified: true
    }
}
```

Example `cue_input` (user-provided parameters):

```cue
input: {
    name:  "my-app"
    image: "ghcr.io/example/my-app"
    tag:   "v1.0.0"
}
```

## Template Input

The console fills two separate CUE fields at render time:

- **`input`** — user-provided deployment parameters (name, image, tag, etc.)
- **`system`** — trusted values set by the console backend from authenticated context (project, namespace, OIDC claims)

This separation enforces a trust boundary: templates can reference `system.namespace` for the project namespace and `system.claims` for the authenticated user's identity without risk of user-supplied values overriding them.

### `#Input` Schema

```cue
#Input: {
    name:    string & =~"^[a-z][a-z0-9-]*$"  // DNS label, max 63 chars
    image:   string                            // container image (no tag)
    tag:     string                            // image tag
    command?: [...string]                      // container ENTRYPOINT override
    args?:   [...string]                       // container CMD override
    env:     [...#EnvVar] | *[]                // environment variables
    port:    int & >0 & <=65535 | *8080        // container port (default 8080)
}
```

### `#System` Schema

The `#System` definition groups the two trusted CUE definitions — `#Claims` and
the outer `#System` struct — that the console backend fills unconditionally from
authenticated context.

```cue
// #Claims carries OIDC ID token claims of the authenticated user.
// Standard claims are required; provider-specific claims are allowed via the
// open struct (...) so templates remain compatible with any OIDC provider.
#Claims: {
    iss:            string      // issuer URL
    sub:            string      // subject (unique user ID)
    exp:            int         // expiration time (Unix seconds)
    iat:            int         // issued-at time (Unix seconds)
    email:          string      // user email address
    email_verified: bool        // whether the email was verified by the provider
    name?:          string      // display name (optional)
    groups?: [...string]        // role memberships from the configured OIDC claim
    ...                         // allow provider-specific claims
}

#System: {
    project:          string             // parent project name
    namespace:        string             // resolved K8s namespace (from Resolver.ProjectNamespace())
    gatewayNamespace: string | *"istio-ingress" // namespace of the gateway (for ReferenceGrant/HTTPRoute)
    claims:           #Claims            // OIDC ID token claims of the authenticated user
}
```

### `#Input` Field Descriptions

| Field       | Type       | Required | Description |
|-------------|------------|----------|-------------|
| `name`      | `string`   | Yes      | Deployment name. Must be a valid DNS label (`^[a-z][a-z0-9-]*$`). |
| `image`     | `string`   | Yes      | Container image repository (e.g. `ghcr.io/holos-run/holos-console`). |
| `tag`       | `string`   | Yes      | Image tag (e.g. `v1.2.3`, `latest`). |
| `command`   | `[...string]` | No   | Overrides the container `ENTRYPOINT`. Omitted when not set. |
| `args`      | `[...string]` | No   | Overrides the container `CMD`. Omitted when not set. |
| `env`       | `[...#EnvVar]` | No  | Container environment variables. Defaults to `[]`. |
| `port`      | `int`      | No       | Container port the application listens on. Must be between 1 and 65535. Defaults to `8080`. The default template names this port `"http"` and creates a Service that maps port 80 to this target. |

### `#System` Field Descriptions

| Field                  | Type          | Description |
|------------------------|---------------|-------------|
| `project`              | `string`      | Parent project name. |
| `namespace`            | `string`      | Kubernetes namespace resolved from the project name. Computed by the server using `Resolver.ProjectNamespace()`. |
| `gatewayNamespace`     | `string`      | Kubernetes namespace of the gateway resource (default: `"istio-ingress"`). Used in ReferenceGrant `from` stanzas and HTTPRoute `parentRefs`. |
| `claims.iss`           | `string`      | OIDC issuer URL (e.g. `https://dex.example.com`). |
| `claims.sub`           | `string`      | OIDC subject (unique user ID). |
| `claims.exp`           | `int`         | Token expiration time as Unix epoch seconds. |
| `claims.iat`           | `int`         | Token issued-at time as Unix epoch seconds. |
| `claims.email`         | `string`      | Authenticated user's email address. |
| `claims.email_verified`| `bool`        | Whether the provider verified the email address. |
| `claims.name`          | `string`      | User's display name (optional; provider-dependent). |
| `claims.groups`        | `[...string]` | User's role memberships from the configured OIDC claim (optional). |

### Using Claims in Templates

Templates can reference any `system.claims` field to annotate or configure
resources with the identity of the user who last rendered the deployment. The
default template uses this to stamp every resource with the deployer's email:

```cue
_annotations: {
    "console.holos.run/deployer-email": system.claims.email
}
```

Apply these annotations in resource `metadata`:

```cue
metadata: {
    name:        input.name
    namespace:   system.namespace
    labels:      _labels
    annotations: _annotations
}
```

Because `#Claims` is an open struct (`...`), templates can also reference
provider-specific claims not listed above. These pass through without CUE
constraint errors as long as the field is present in the token.

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

The console reads structured fields nested under `output` from the evaluated CUE template:

### `output.namespacedResources` Field

```cue
// Structure: output.namespacedResources.<namespace>.<Kind>.<name>
output: namespacedResources: [Namespace=string]: [Kind=string]: [Name=string]: {
    kind: Kind
    metadata: {
        name:      Name
        namespace: Namespace
        ...
    }
    ...
}
```

The `output.namespacedResources` field organizes resources that live within a Kubernetes
namespace. Resources are indexed by namespace, then by Kind, then by name. This
three-level nesting enforces uniqueness per Kind/name within a namespace at the
CUE level — duplicates cause a CUE evaluation error before any Kubernetes call.

### `output.clusterResources` Field

```cue
// Structure: output.clusterResources.<Kind>.<name>
output: clusterResources: [Kind=string]: [Name=string]: {
    kind: Kind
    metadata: {
        name: Name
        ...
    }
    ...
}
```

The `output.clusterResources` field organizes cluster-scoped resources (resources without a
namespace, such as `Namespace`, `ClusterRole`, or `ClusterRoleBinding`). The
initial implementation keeps the cluster allowlist empty; it will be extended
incrementally as cluster resource support is added.

### `output.systemNamespacedResources` and `output.systemClusterResources` Fields

These fields follow the same structure as `namespacedResources` and `clusterResources`
respectively, but are reserved for system template resources managed by the platform
operator. User-authored deployment templates should NOT define these fields.

**System Template Unification**

At deploy time, the console unifies enabled system templates with the deployment
template before CUE compilation. Because system templates share the same
`package deployment` declaration, they have full access to all `input.*` and
`system.*` fields defined by the deployment template — including `input.name`,
`input.port`, `system.namespace`, and `system.gatewayNamespace`.

System templates contribute their resources to `output.systemNamespacedResources`
and `output.systemClusterResources` so that they do not conflict with the user
template's `output.namespacedResources` and `output.clusterResources` fields.

**Operator Guarantees**

Resources placed in `output.systemNamespacedResources` and
`output.systemClusterResources` by system templates are:

- **Not modifiable by user templates** — user-authored deployment templates cannot
  override or replace resources in the `system*` output fields. The fields are
  explicitly separate, so CUE unification does not allow a user template to conflict
  with a system template's output.
- **Always applied alongside user resources** — the render engine collects resources
  from all four output fields together and applies them in a single server-side apply
  pass. This guarantees that operator-required resources (e.g., `HTTPRoute`, network
  policies) are always present whenever the deployment is applied.

**Operator Constraints via System Templates**

Operators can use system templates to constrain user-controlled resources. Because
system templates are unified with the deployment template before compilation, a
system template can add CUE constraints on `output.namespacedResources` (the user
output fields). For example, a system template could enforce a required label or
annotation on all namespaced resources — any user template that violates the
constraint will fail at CUE evaluation time before any Kubernetes call.

**Example: HTTPRoute System Template**

The built-in `default_referencegrant.cue` system template seeds a disabled HTTPRoute
example. When enabled, it adds an `HTTPRoute` to `output.systemNamespacedResources`
that routes all gateway traffic to the deployment's `Service`:

```cue
package deployment

output: {
    systemNamespacedResources: (system.namespace): {
        HTTPRoute: (input.name): {
            apiVersion: "gateway.networking.k8s.io/v1"
            kind:       "HTTPRoute"
            metadata: {
                name:      input.name
                namespace: system.namespace
                labels: {
                    "app.kubernetes.io/managed-by": "console.holos.run"
                    "app.kubernetes.io/name":       input.name
                }
            }
            spec: {
                parentRefs: [{
                    group:     "gateway.networking.k8s.io"
                    kind:      "Gateway"
                    namespace: system.gatewayNamespace
                    name:      "default"
                }]
                rules: [{
                    backendRefs: [{
                        name: input.name
                        port: 80
                    }]
                }]
            }
        }
    }
    systemClusterResources: {}
}
```

This system template references `input.name` (user-supplied deployment name),
`system.namespace` (resolved project namespace), and `system.gatewayNamespace`
(operator-configured gateway namespace) — all available because system templates
are unified with the deployment template before evaluation.

### Struct Key Consistency

CUE constraints enforce that the struct path keys match the resource metadata:

- `output.namespacedResources.<namespace>` must match `metadata.namespace`
- `output.namespacedResources.<namespace>.<Kind>` must match `kind`
- `output.namespacedResources.<namespace>.<Kind>.<name>` must match `metadata.name`
- `output.clusterResources.<Kind>` must match `kind`
- `output.clusterResources.<Kind>.<name>` must match `metadata.name`

A mismatch is a CUE evaluation error caught before any Kubernetes API call.

### Example Output Structure

The default template produces four namespaced resources (ServiceAccount, Deployment, Service, and ReferenceGrant):

```cue
output: {
    namespacedResources: (system.namespace): {
        ServiceAccount: (input.name): {
            apiVersion: "v1"
            kind:       "ServiceAccount"
            metadata: {
                name:      input.name
                namespace: system.namespace
                labels:    _labels
            }
        }
        Deployment: (input.name): {
            apiVersion: "apps/v1"
            kind:       "Deployment"
            metadata: { ... }
            spec: { ... }
        }
        Service: (input.name): {
            apiVersion: "v1"
            kind:       "Service"
            metadata: { ... }
            spec: { ... }
        }
        ReferenceGrant: "allow-gateway-httproute": {
            apiVersion: "gateway.networking.k8s.io/v1beta1"
            kind:       "ReferenceGrant"
            metadata: { ... }
            spec: { ... }
        }
    }
    clusterResources: {}
}
```

## Validation Constraints

Every resource collected from all four output fields (`output.namespacedResources`,
`output.clusterResources`, `output.systemNamespacedResources`, and
`output.systemClusterResources`) must satisfy these constraints or the render is
rejected. These are enforced in Go after CUE evaluation, not in CUE itself.

### Required Fields

Each resource must have:
- `apiVersion` — non-empty
- `kind` — non-empty and in the allowed set
- `metadata.name` — non-empty

Namespaced resources additionally require:
- `metadata.namespace` — must exactly match the struct key and `system.namespace`

Cluster resources additionally require:
- `metadata.namespace` — must be absent (cluster-scoped resources have no namespace)

### Allowed Resource Kinds

Templates may only produce namespaced resources of these kinds:

| Kind             | API Group                          |
|------------------|------------------------------------|
| `Deployment`     | `apps/v1`                          |
| `Service`        | `v1`                               |
| `ServiceAccount` | `v1`                               |
| `Role`           | `rbac.authorization.k8s.io/v1`     |
| `RoleBinding`    | `rbac.authorization.k8s.io/v1`     |
| `HTTPRoute`      | `gateway.networking.k8s.io/v1`     |
| `ReferenceGrant` | `gateway.networking.k8s.io/v1beta1`|
| `ConfigMap`      | `v1`                               |
| `Secret`         | `v1`                               |

The cluster allowlist is initially empty. Cluster-scoped kind support will be
added incrementally.

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

## Appendix: Source Code Reference

This section maps the template interface to its implementation across the
codebase. Use it for advanced troubleshooting or when developing new features.

### CUE Template Source

| File | Purpose |
|------|---------|
| `console/templates/default_template.cue` | Default CUE template with `#Input`, `#Claims`, and `#System` schema definitions, env var transformation, `_annotations` helper (stamps `system.claims.email`), `#Namespaced`/`#Cluster` constraints, and resource definitions nested under `output.namespacedResources`/`output.clusterResources`. Embedded into the Go binary via `console/templates/embed.go`. |
| `console/templates/embed.go` | `//go:embed` directive that loads `default_template.cue` as the fallback template. |
| `console/system_templates/default_referencegrant.cue` | Built-in example HTTPRoute system template using `package deployment`. References `input.name` and `system.gatewayNamespace` — designed to be unified with the deployment template at deploy time. Seeded as disabled (not mandatory) on first `ListSystemTemplates` access. Embedded via `console/system_templates/embed.go`. |

### Go Rendering Pipeline

Two render paths exist — one for the deployment service and one for the template preview RPC:

| File | Purpose |
|------|---------|
| `console/deployments/render.go` | `CueRenderer.Render()` — deployment service path: compiles CUE source, marshals `UserInput` to JSON and fills `"input"`, marshals `SystemInput` and fills `"system"`, walks structured `output.namespacedResources`/`output.clusterResources` fields (and optionally `output.systemNamespacedResources`/`output.systemClusterResources`), validates. |
| `console/deployments/render.go` | `CueRenderer.RenderWithSystemTemplates()` — unifies zero or more system template CUE sources with the deployment template by concatenating them (same package) before compilation, then fills in system and user inputs. Used at deploy time to produce both user and system resources in a single evaluation. |
| `console/deployments/render.go` | `CueRenderer.RenderWithCueInput()` — template preview path: concatenates CUE source with a raw CUE input string before compilation so cross-references (e.g. input.name used in system templates) resolve correctly. Extracts `system.namespace` from the compiled value. |
| `console/deployments/render.go` | `ClaimsInput`, `SystemInput`, `UserInput` structs — split Go representation of template inputs. `SystemInput` (project, namespace, gatewayNamespace, claims) is trusted backend context; `UserInput` (name, image, tag, etc.) is user-supplied. |
| `console/deployments/render.go` | `validateResource()` — enforces kind allowlist and managed-by label on a single resource. `evaluateStructured()` reads from `output.*` paths, dispatches to `walkNamespacedResources()` and `walkClusterResources()` which add namespace-match and struct-key consistency checks. |
| `console/deployments/apply.go` | `Applier.Apply()` — injects ownership label, performs server-side apply with field manager `console.holos.run`. |
| `console/deployments/apply.go:96-127` | `Applier.Cleanup()` — deletes all resources matching the ownership label selector. |

### Template Service

| File | Purpose |
|------|---------|
| `console/templates/handler.go` | `DeploymentTemplateService` handler — CRUD for templates stored as ConfigMaps. |
| `console/templates/k8s.go` | ConfigMap storage: templates stored with `template.cue` data key, `deployment-template` resource-type label. |
| `console/templates/render_adapter.go` | `CueRendererAdapter` — wraps `deployments.CueRenderer` to produce YAML and structured object data for the template preview RPC. |

### System Template Service

| File | Purpose |
|------|---------|
| `console/system_templates/handler.go` | `SystemTemplateService` handler — CRUD and render for org-scoped system templates stored as ConfigMaps. Edit access requires `PERMISSION_SYSTEM_DEPLOYMENTS_EDIT`. |
| `console/system_templates/k8s.go` | ConfigMap storage: templates stored with `template.cue` data key, `system-template` resource-type label, `mandatory` and `enabled` annotations. Seeds `default_referencegrant.cue` (HTTPRoute example) on first `ListSystemTemplates`. `ListEnabledSystemTemplateSources()` returns CUE sources for enabled templates (satisfies `deployments.SystemTemplateProvider`). |
| `console/system_templates/apply.go` | `MandatoryTemplateApplier.ApplyMandatorySystemTemplates()` — called by the projects service after project namespace creation to apply templates that are both `mandatory=true` AND `enabled=true`. |

### Deployment Service

| File | Purpose |
|------|---------|
| `console/deployments/handler.go` | Create/Update flow — builds `SystemInput` (including `GatewayNamespace`) from authenticated context and `UserInput` from API request fields, calls `renderResources()` (which unifies enabled system templates via `RenderWithSystemTemplates`), then `Apply()`. |
| `console/deployments/handler.go:607-656` | `protoToEnvVarInput()` / `envVarInputToProto()` — converts between protobuf `EnvVar` and `EnvVarInput` for CUE. |
| `console/deployments/k8s.go` | ConfigMap storage for deployment state: image, tag, template, command, args, env stored as data keys. |

### Protobuf Definitions

| File | Purpose |
|------|---------|
| `proto/holos/console/v1/deployments.proto` | `Deployment`, `EnvVar`, `SecretKeyRef`, `ConfigMapKeyRef` messages; `DeploymentService` RPCs. |
| `proto/holos/console/v1/deployment_templates.proto` | `DeploymentTemplate` message; `DeploymentTemplateService` RPCs including `RenderDeploymentTemplate`. |
| `proto/holos/console/v1/system_templates.proto` | `SystemTemplate` message; `SystemTemplateService` RPCs including `RenderSystemTemplate`. |

### Generated Code

| Directory | Purpose |
|-----------|---------|
| `gen/holos/console/v1/` | Go protobuf structs (`*_pb.go`) and ConnectRPC bindings (`consolev1connect/`). |
| `frontend/src/gen/` | TypeScript protobuf types for the UI. |

### Kind-to-GVR Mapping

The `allowedKinds` map in `console/deployments/apply.go:25-34` maps each
allowed Kind to its Kubernetes `GroupVersionResource`, used for dynamic client
operations during apply and cleanup.

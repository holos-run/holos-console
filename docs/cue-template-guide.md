# CUE Deployment Template Guide

This guide covers the interface between deployment fields and CUE templates in
holos-console. It explains how to write a custom template to deploy your app,
and provides reference material on the inputs, output structure, and validation
constraints enforced at render time.

## Overview

When a user creates or updates a deployment, the console:

1. Loads the CUE template source from a `DeploymentTemplate` ConfigMap.
2. Loads all enabled `SystemTemplate` sources for the organization.
3. Builds a `PlatformInput` (project, namespace, gatewayNamespace, claims) from authenticated server context and a `ProjectInput` (name, image, tag, etc.) from the API request fields.
4. Prepends the generated CUE schema (produced from `api/v1alpha1` Go types via `cue get go`) before compiling templates. The renderer concatenates all template sources into a single compilation unit.
5. Fills `ProjectInput` at the `input` path and `PlatformInput` at the `platform` path.
6. Reads structured output fields based on the render level (ADR 016 Decision 8):
   - Always reads `projectResources.namespacedResources` and `projectResources.clusterResources`.
   - When system templates are present (organization/folder level), also reads `platformResources.namespacedResources` and `platformResources.clusterResources`.
   - At the project level (`Render()`), `platformResources` is intentionally skipped — a project template that defines `platformResources` fields has no effect.
7. Validates each resource against safety constraints.
8. Applies the validated resources to Kubernetes via server-side apply.

The architectural decision to use structured output is recorded in
[ADR 012](adrs/012-structured-resource-output.md), refined by
[ADR 016](adrs/016-config-management-resource-schema.md). The decision to split
platform and project inputs is recorded in
[ADR 013](adrs/013-separate-system-user-template-input.md), extended by ADR 016.

## Writing a Custom Template

This section walks through deploying a web service end-to-end: from a complete
working template to making it accessible outside the cluster.

### Complete Template Example

The template below is the actual built-in default. It produces a `ServiceAccount`,
a `Deployment`, a `Service`, and a `ReferenceGrant` — everything needed to run a
container, reach it from inside the cluster, and allow an HTTPRoute from the
gateway namespace to reference the Service.

Templates do **not** declare a `package` clause — the renderer prepends the
generated schema and controls the CUE package. CUE type definitions (`#ProjectInput`,
`#PlatformInput`, `#Claims`, `#EnvVar`, `#KeyRef`) are generated from Go types in
`api/v1alpha1` via `cue get go` and are available automatically; do not redefine
them in your template.

```cue
// Use generated type definitions from api/v1alpha1 (prepended by renderer).
// Additional CUE constraints narrow the generated types for this template.
input: #ProjectInput & {
	name: =~"^[a-z][a-z0-9-]*$" // DNS label
	env:  [...#EnvVar] | *[]
	port: >0 & <=65535 | *8080
}
platform: #PlatformInput

// _labels are the standard labels required on every resource.
// app.kubernetes.io/managed-by MUST equal "console.holos.run" or the
// render will be rejected.
_labels: {
	"app.kubernetes.io/name":       input.name
	"app.kubernetes.io/managed-by": "console.holos.run"
}

// _annotations are standard annotations applied to every resource.
// console.holos.run/deployer-email records the identity of the user
// who last rendered and applied this resource.
_annotations: {
	"console.holos.run/deployer-email": platform.claims.email
}

// _envSpec transforms the env input into Kubernetes container env format.
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

// #Namespaced constrains namespaced resource struct keys to match resource metadata.
// Structure: namespaced.<namespace>.<Kind>.<name>
// The struct path keys must match the corresponding resource metadata fields.
#Namespaced: [Namespace=string]: [Kind=string]: [Name=string]: {
	kind: Kind
	metadata: {
		name:      Name
		namespace: Namespace
		...
	}
	...
}

// #Cluster constrains cluster-scoped resource struct keys to match resource metadata.
// Structure: cluster.<Kind>.<name>
// The struct path keys must match the corresponding resource metadata fields.
#Cluster: [Kind=string]: [Name=string]: {
	kind: Kind
	metadata: {
		name: Name
		...
	}
	...
}

// projectResources collects all rendered Kubernetes resources.
// namespacedResources organizes resources that live within a Kubernetes namespace.
// The struct key path (namespace/Kind/name) must match the resource metadata.
// clusterResources organizes cluster-scoped resources.
projectResources: {
	namespacedResources: #Namespaced & {
		(platform.namespace): {
			// ServiceAccount provides a Kubernetes identity for the pods.
			ServiceAccount: (input.name): {
				apiVersion: "v1"
				kind:       "ServiceAccount"
				metadata: {
					name:        input.name
					namespace:   platform.namespace
					labels:      _labels
					annotations: _annotations
				}
			}

			// Deployment runs the container image.
			Deployment: (input.name): {
				apiVersion: "apps/v1"
				kind:       "Deployment"
				metadata: {
					name:        input.name
					namespace:   platform.namespace
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

			// Service exposes port 80 → container port input.port (named "http").
			Service: (input.name): {
				apiVersion: "v1"
				kind:       "Service"
				metadata: {
					name:        input.name
					namespace:   platform.namespace
					labels:      _labels
					annotations: _annotations
				}
				spec: {
					selector: "app.kubernetes.io/name": input.name
					ports: [{port: 80, targetPort: "http", name: "http"}]
				}
			}

			// ReferenceGrant allows HTTPRoute resources in the gateway namespace to
			// reference Service resources in the project namespace.
			// This enables system templates (such as the example HTTPRoute template)
			// to expose deployments via the gateway.
			// See: https://gateway-api.sigs.k8s.io/api-types/referencegrant/
			ReferenceGrant: "allow-gateway-httproute": {
				apiVersion: "gateway.networking.k8s.io/v1beta1"
				kind:       "ReferenceGrant"
				metadata: {
					name:        "allow-gateway-httproute"
					namespace:   platform.namespace
					labels:      _labels
					annotations: _annotations
				}
				spec: {
					from: [{
						group:     "gateway.networking.k8s.io"
						kind:      "HTTPRoute"
						namespace: platform.gatewayNamespace
					}]
					to: [{
						group: ""
						kind:  "Service"
					}]
				}
			}
		}
	}

	// clusterResources organizes cluster-scoped resources. Initially empty;
	// extended as cluster resource support is added.
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

Add the following resource to your template's `projectResources.namespacedResources` block when external
access is needed:

```cue
HTTPRoute: (input.name): {
    apiVersion: "gateway.networking.k8s.io/v1"
    kind:       "HTTPRoute"
    metadata: {
        name:        input.name
        namespace:   platform.namespace
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

1. **Never declare a `package` clause** — the renderer prepends the generated schema and controls the CUE package. Templates with a `package` declaration will cause a compilation error.
2. **Do not redefine `#ProjectInput`, `#PlatformInput`, `#Claims`, `#EnvVar`, or `#KeyRef`** — these types are generated from `api/v1alpha1` Go types via `cue get go` and are prepended by the renderer automatically.
3. **Always declare `input: #ProjectInput` and `platform: #PlatformInput`** — these are the unification points where the console injects project and platform parameters respectively.
4. **Always declare `projectResources.namespacedResources` and `projectResources.clusterResources` output fields** — the console requires the structured output format.
5. **Always include the managed-by label** on every resource or validation will reject the render.
6. **Set `metadata.namespace` to `platform.namespace`** on every namespaced resource — cross-namespace resources are rejected.
7. **Match struct keys to metadata** — `projectResources.namespacedResources.<ns>.<Kind>.<name>` must exactly match the resource `metadata.namespace`, `kind`, and `metadata.name`.
8. **Use helper definitions** (prefixed with `_`) for shared values like labels, env transformation, etc. These are not exported and don't affect the output.
9. **Never place project or namespace in `input`** — these are platform-provided values available at `platform.project` and `platform.namespace`.
10. **Use the named port `"http"` and Service port `80`** when adding an `HTTPRoute` — the `backendRef.port` must match the `Service` port (`80`), not the container port (`input.port`).
11. **Never define `platformResources` in a user deployment template** — the project-level renderer does not read `platformResources` (ADR 016 Decision 8). Any values defined there are silently ignored. Platform resources are defined in system templates evaluated at the organization/folder level.

### Previewing Your Template

Use the `RenderDeploymentTemplate` RPC to preview a template without creating a
deployment. This accepts a `cue_template` (raw CUE source) and a `cue_input`
(valid CUE source that supplies concrete values for template parameters),
returning the rendered resources as multi-document YAML (`rendered_yaml`) and as
a pretty-printed JSON array (`rendered_json`). Useful for validating templates
during authoring.

The RPC accepts two separate CUE input fields:

- `cue_system_input` — trusted platform context (project, namespace, claims); populated by the backend from authenticated context when provided by the caller
- `cue_input` — user-provided deployment parameters (name, image, tag, env, etc.)

Both are valid CUE source. The backend combines them into a single document before unifying with the template, so both `platform` and `input` top-level fields are available.

Example `cue_system_input` (trusted, set from authenticated context):

```cue
platform: {
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

- **`input`** — project-provided deployment parameters (name, image, tag, etc.); typed as `#ProjectInput`
- **`platform`** — trusted values set by the console backend from authenticated context (project, namespace, OIDC claims); typed as `#PlatformInput`

This separation enforces a trust boundary: templates can reference `platform.namespace` for the project namespace and `platform.claims` for the authenticated user's identity without risk of user-supplied values overriding them.

### `#ProjectInput` Schema

The `#ProjectInput` type is **generated** from the `ProjectInput` Go struct in `api/v1alpha1/input.go` via `cue get go`. The renderer prepends this generated schema before compiling any template — do not redefine it in your template.

The effective schema is:

```cue
// Generated from api/v1alpha1.ProjectInput — do not redefine in templates.
#ProjectInput: {
    name:    string             // deployment name
    image:   string             // container image repository (no tag)
    tag:     string             // image tag
    command?: [...string]       // container ENTRYPOINT override
    args?:   [...string]        // container CMD override
    env?:    [...#EnvVar]       // environment variables
    port:    int                // container port (default applied by template)
}
```

### `#PlatformInput` Schema

The `#PlatformInput` type is **generated** from the `PlatformInput` Go struct in `api/v1alpha1/input.go` via `cue get go`. The `#Claims` type is similarly generated from `Claims`. The renderer prepends these generated schemas automatically.

The effective schema is:

```cue
// Generated from api/v1alpha1.PlatformInput — do not redefine in templates.
#PlatformInput: {
    project:          string             // parent project name
    namespace:        string             // resolved K8s namespace (from Resolver.ProjectNamespace())
    gatewayNamespace: string             // namespace of the gateway (for ReferenceGrant/HTTPRoute)
    organization:     string             // parent organization name
    claims:           #Claims            // OIDC ID token claims of the authenticated user
}

// Generated from api/v1alpha1.Claims — do not redefine in templates.
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
```

### `#ProjectInput` Field Descriptions

| Field       | Type       | Required | Description |
|-------------|------------|----------|-------------|
| `name`      | `string`   | Yes      | Deployment name. Must be a valid DNS label (`^[a-z][a-z0-9-]*$`). |
| `image`     | `string`   | Yes      | Container image repository (e.g. `ghcr.io/holos-run/holos-console`). |
| `tag`       | `string`   | Yes      | Image tag (e.g. `v1.2.3`, `latest`). |
| `command`   | `[...string]` | No   | Overrides the container `ENTRYPOINT`. Omitted when not set. |
| `args`      | `[...string]` | No   | Overrides the container `CMD`. Omitted when not set. |
| `env`       | `[...#EnvVar]` | No  | Container environment variables. Defaults to `[]`. |
| `port`      | `int`      | No       | Container port the application listens on. Must be between 1 and 65535. Defaults to `8080`. The default template names this port `"http"` and creates a Service that maps port 80 to this target. |

### `#PlatformInput` Field Descriptions

| Field                  | Type          | Description |
|------------------------|---------------|-------------|
| `project`              | `string`      | Parent project name. |
| `namespace`            | `string`      | Kubernetes namespace resolved from the project name. Computed by the server using `Resolver.ProjectNamespace()`. |
| `gatewayNamespace`     | `string`      | Kubernetes namespace of the gateway resource (default: `"istio-ingress"`). Used in ReferenceGrant `from` stanzas and HTTPRoute `parentRefs`. |
| `organization`         | `string`      | Parent organization name. |
| `claims.iss`           | `string`      | OIDC issuer URL (e.g. `https://dex.example.com`). |
| `claims.sub`           | `string`      | OIDC subject (unique user ID). |
| `claims.exp`           | `int`         | Token expiration time as Unix epoch seconds. |
| `claims.iat`           | `int`         | Token issued-at time as Unix epoch seconds. |
| `claims.email`         | `string`      | Authenticated user's email address. |
| `claims.email_verified`| `bool`        | Whether the provider verified the email address. |
| `claims.name`          | `string`      | User's display name (optional; provider-dependent). |
| `claims.groups`        | `[...string]` | User's role memberships from the configured OIDC claim (optional). |

### Using Claims in Templates

Templates can reference any `platform.claims` field to annotate or configure
resources with the identity of the user who last rendered the deployment. The
default template uses this to stamp every resource with the deployer's email:

```cue
_annotations: {
    "console.holos.run/deployer-email": platform.claims.email
}
```

Apply these annotations in resource `metadata`:

```cue
metadata: {
    name:        input.name
    namespace:   platform.namespace
    labels:      _labels
    annotations: _annotations
}
```

Because `#Claims` is an open struct (`...`), templates can also reference
provider-specific claims not listed above. These pass through without CUE
constraint errors as long as the field is present in the token.

### `#EnvVar` Schema

The `#EnvVar` and `#KeyRef` types are **generated** from Go structs in `api/v1alpha1/input.go` via `cue get go`. Do not redefine them in your template.

Each environment variable has exactly one value source:

```cue
// Generated from api/v1alpha1.EnvVar — do not redefine in templates.
#EnvVar: {
    name:               string
    value?:             string       // literal value
    secretKeyRef?:      #KeyRef      // reference to a K8s Secret key
    configMapKeyRef?:   #KeyRef      // reference to a K8s ConfigMap key
}

// Generated from api/v1alpha1.KeyRef — do not redefine in templates.
#KeyRef: {
    name: string   // Secret or ConfigMap name
    key:  string   // key within the resource
}
```

## Template Output

The console reads structured fields nested under `projectResources` and `platformResources` from the evaluated CUE template:

### `projectResources.namespacedResources` Field

```cue
// Structure: projectResources.namespacedResources.<namespace>.<Kind>.<name>
projectResources: namespacedResources: [Namespace=string]: [Kind=string]: [Name=string]: {
    kind: Kind
    metadata: {
        name:      Name
        namespace: Namespace
        ...
    }
    ...
}
```

The `projectResources.namespacedResources` field organizes resources that live within a Kubernetes
namespace. Resources are indexed by namespace, then by Kind, then by name. This
three-level nesting enforces uniqueness per Kind/name within a namespace at the
CUE level — duplicates cause a CUE evaluation error before any Kubernetes call.

### `projectResources.clusterResources` Field

```cue
// Structure: projectResources.clusterResources.<Kind>.<name>
projectResources: clusterResources: [Kind=string]: [Name=string]: {
    kind: Kind
    metadata: {
        name: Name
        ...
    }
    ...
}
```

The `projectResources.clusterResources` field organizes cluster-scoped resources (resources without a
namespace, such as `Namespace`, `ClusterRole`, or `ClusterRoleBinding`). The
initial implementation keeps the cluster allowlist empty; it will be extended
incrementally as cluster resource support is added.

### `platformResources.namespacedResources` and `platformResources.clusterResources` Fields

These fields follow the same structure as `projectResources.namespacedResources` and `projectResources.clusterResources`
respectively, but are reserved for system template resources managed by the platform
operator. User-authored deployment templates should NOT define these fields.

**System Template Unification**

At deploy time, the console unifies enabled system templates with the deployment
template before CUE compilation. Because all templates share the same generated
schema (prepended by the renderer), they have full access to all `input.*` and
`platform.*` fields — including `input.name`, `input.port`, `platform.namespace`,
and `platform.gatewayNamespace`.

System templates contribute their resources to `platformResources.namespacedResources`
and `platformResources.clusterResources` so that they do not conflict with the project
template's `projectResources.namespacedResources` and `projectResources.clusterResources` fields.

**Operator Guarantees**

Resources placed in `platformResources.namespacedResources` and
`platformResources.clusterResources` by system templates are:

- **Not produced by project-level templates** — the renderer enforces a hard boundary
  (ADR 016 Decision 8) in Go code: when rendering a project-level template (`Render()`),
  it does not read `platformResources` from the evaluated CUE value. A project template
  that defines `platformResources` fields is valid CUE but the values are silently
  ignored. Only the organization/folder-level path (`RenderWithSystemTemplates()`) reads
  both collections.
- **Always applied alongside user resources** — the render engine collects resources
  from all four output fields at the organization/folder level and applies them in a
  single server-side apply pass. This guarantees that operator-required resources
  (e.g., `HTTPRoute`, network policies) are always present whenever the deployment
  is applied.

**Operator Constraints via System Templates**

Operators can use system templates to constrain user-controlled resources. Because
system templates are unified with the deployment template before compilation, a
system template can add CUE constraints on `projectResources.namespacedResources` (the project
output fields). For example, a system template could enforce a required label or
annotation on all namespaced resources — any user template that violates the
constraint will fail at CUE evaluation time before any Kubernetes call.

**Example: HTTPRoute System Template**

The built-in `default_referencegrant.cue` system template seeds a disabled HTTPRoute
example. When enabled, it adds an `HTTPRoute` to `platformResources.namespacedResources`
that routes all gateway traffic to the deployment's `Service`:

```cue
// platformResources contributes platform-managed Kubernetes resources.
// Platform templates define resources under platformResources so they do not
// conflict with the project template's projectResources fields.
platformResources: {
    // namespacedResources organizes platform-managed namespaced resources.
    namespacedResources: (platform.namespace): {
        // HTTPRoute exposes the deployment's Service via the gateway.
        // It routes all traffic from the gateway to the Service named input.name
        // on port 80 (the Service port, which forwards to containerPort input.port).
        // See: https://gateway-api.sigs.k8s.io/api-types/httproute/
        HTTPRoute: (input.name): {
            apiVersion: "gateway.networking.k8s.io/v1"
            kind:       "HTTPRoute"
            metadata: {
                name:      input.name
                namespace: platform.namespace
                labels: {
                    "app.kubernetes.io/managed-by": "console.holos.run"
                    "app.kubernetes.io/name":       input.name
                }
            }
            spec: {
                parentRefs: [{
                    group:     "gateway.networking.k8s.io"
                    kind:      "Gateway"
                    namespace: platform.gatewayNamespace
                    // Change "default" to the name of your Gateway resource.
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

    // clusterResources organizes platform-managed cluster-scoped resources (none for this template).
    clusterResources: {}
}
```

This system template references `input.name` (project-supplied deployment name),
`platform.namespace` (resolved project namespace), and `platform.gatewayNamespace`
(operator-configured gateway namespace) — all available because system templates
are unified with the deployment template before evaluation.

### Struct Key Consistency

CUE constraints enforce that the struct path keys match the resource metadata:

- `projectResources.namespacedResources.<namespace>` must match `metadata.namespace`
- `projectResources.namespacedResources.<namespace>.<Kind>` must match `kind`
- `projectResources.namespacedResources.<namespace>.<Kind>.<name>` must match `metadata.name`
- `projectResources.clusterResources.<Kind>` must match `kind`
- `projectResources.clusterResources.<Kind>.<name>` must match `metadata.name`

A mismatch is a CUE evaluation error caught before any Kubernetes API call.

### Example Output Structure

The default template produces four namespaced resources (ServiceAccount, Deployment, Service, and ReferenceGrant):

```cue
projectResources: {
    namespacedResources: (platform.namespace): {
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

Every resource collected from all four output fields (`projectResources.namespacedResources`,
`projectResources.clusterResources`, `platformResources.namespacedResources`, and
`platformResources.clusterResources`) must satisfy these constraints or the render is
rejected. These are enforced in Go after CUE evaluation, not in CUE itself.

### Required Fields

Each resource must have:
- `apiVersion` — non-empty
- `kind` — non-empty and in the allowed set
- `metadata.name` — non-empty

Namespaced resources additionally require:
- `metadata.namespace` — must exactly match the struct key and `platform.namespace`

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
| `console/templates/default_template.cue` | Default CUE template. Narrows generated `#ProjectInput` and `#PlatformInput` types with additional CUE constraints, defines `_envSpec` env var transformation, `_annotations` helper (stamps `platform.claims.email`), `#Namespaced`/`#Cluster` structural constraints, and resource definitions nested under `projectResources.namespacedResources`/`projectResources.clusterResources`. Embedded into the Go binary via `console/templates/embed.go`. No `package` declaration — the renderer prepends the generated schema. |
| `console/templates/embed.go` | `//go:embed` directive that loads `default_template.cue` as the fallback template. |
| `console/system_templates/default_referencegrant.cue` | Built-in example HTTPRoute system template. References `input.name` and `platform.gatewayNamespace` — designed to be unified with the deployment template at deploy time. Contributes resources to `platformResources.namespacedResources`. Seeded as disabled (not mandatory) on first `ListSystemTemplates` access. Embedded via `console/system_templates/embed.go`. No `package` declaration. |
| `api/v1alpha1/` | Go type definitions for `PlatformInput`, `ProjectInput`, `Claims`, `EnvVar`, `KeyRef`, `PlatformResources`, `ProjectResources`. CUE schemas (`#PlatformInput`, `#ProjectInput`, etc.) are generated from these types via `cue get go` and embedded into the binary. The renderer prepends the generated schema before compiling any template. |

### Go Rendering Pipeline

Two render paths exist — one for the deployment service and one for the template preview RPC:

| File | Purpose |
|------|---------|
| `console/deployments/render.go` | `CueRenderer.Render()` — project-level render path: prepends generated schema, compiles CUE source, fills `"input"` and `"platform"`, then calls `evaluateStructured(..., false)` which reads only `projectResources` (ADR 016 Decision 8 hard boundary — `platformResources` is intentionally skipped). |
| `console/deployments/render.go` | `CueRenderer.RenderWithSystemTemplates()` — organization/folder-level render path: unifies system template sources with the deployment template before compilation, then calls `evaluateStructured(..., true)` which reads both `projectResources` and `platformResources`. |
| `console/deployments/render.go` | `CueRenderer.RenderWithCueInput()` — template preview path: concatenates generated schema, CUE source, and a raw CUE input string before compilation so cross-references (e.g. `input.name` used in system templates) resolve correctly. Extracts `platform.namespace` from the compiled value. Calls `evaluateStructured(..., true)` to read both collections. |
| `console/deployments/render.go` | `PlatformInput`, `ProjectInput` structs in `api/v1alpha1` — split Go representation of template inputs. `PlatformInput` (project, namespace, gatewayNamespace, organization, claims) is trusted backend context; `ProjectInput` (name, image, tag, etc.) is user-supplied. |
| `console/deployments/render.go` | `validateResource()` — enforces kind allowlist and managed-by label on a single resource. `evaluateStructured(unified, ns, readPlatformResources)` reads `projectResources.*` always and `platformResources.*` only when `readPlatformResources` is true; dispatches to `walkNamespacedResources()` and `walkClusterResources()` which add namespace-match and struct-key consistency checks. |
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
| `console/deployments/handler.go` | Create/Update flow — builds `PlatformInput` (including `GatewayNamespace`) from authenticated context and `ProjectInput` from API request fields, calls `renderResources()` (which unifies enabled system templates via `RenderWithSystemTemplates`), then `Apply()`. |
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
| `api/v1alpha1/cue.mod/` | Generated CUE schema files produced by `cue get go ./api/v1alpha1/...`. The renderer embeds and prepends these before compiling any template. |

### Kind-to-GVR Mapping

The `allowedKinds` map in `console/deployments/apply.go:25-34` maps each
allowed Kind to its Kubernetes `GroupVersionResource`, used for dynamic client
operations during apply and cleanup.

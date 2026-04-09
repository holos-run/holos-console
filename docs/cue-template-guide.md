# CUE Deployment Template Guide

This guide covers the interface between deployment fields and CUE templates in
holos-console. It explains how to write a custom template to deploy your app,
and provides reference material on the inputs, output structure, and validation
constraints enforced at render time.

## Overview

When a user creates or updates a deployment, the console:

1. Loads the CUE template source from a `DeploymentTemplate` ConfigMap.
2. Loads the set of platform templates that participate in this render: mandatory AND enabled templates always participate; additionally, enabled templates that are explicitly linked to the deployment template are included (see [Linking Platform Templates](#linking-platform-templates)).
3. Builds a `PlatformInput` (project, namespace, gatewayNamespace, claims) from authenticated server context and a `ProjectInput` (name, image, tag, etc.) from the API request fields.
4. Prepends the generated CUE schema (produced from `api/v1alpha1` Go types via `cue get go`) before compiling templates. The renderer concatenates all template sources into a single compilation unit.
5. Fills `ProjectInput` at the `input` path and `PlatformInput` at the `platform` path.
6. Reads structured output fields based on the render level (ADR 016 Decision 8):
   - Always reads `projectResources.namespacedResources` and `projectResources.clusterResources`.
   - When platform templates are present (organization/folder level), also reads `platformResources.namespacedResources` and `platformResources.clusterResources`.
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
			// This enables platform templates (such as the example HTTPRoute template)
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
11. **Never define `platformResources` in a user deployment template** — the project-level renderer does not read `platformResources` (ADR 016 Decision 8). Any values defined there are silently ignored. Platform resources are defined in platform templates evaluated at the organization/folder level.
12. **Declare a `defaults` block** to pre-fill the Create Deployment form — add a `defaults: #ProjectInput & { ... }` block at the top of the template and wire each field in `input` as `*defaults.field | _`. See [Template Defaults](#template-defaults) for the complete pattern.

### Platform and Project Templates Working Together

This section walks through a concrete two-template scenario: an org-level
platform template that provides an HTTPRoute and constrains which resource kinds
project templates may produce, paired with a project-level template that deploys
[go-httpbin](https://github.com/mccutchen/go-httpbin). Use this as a reference
for the ADR 016 Decision 9 constraint pattern. The examples below are the actual
embedded templates (`console/org_templates/example_httpbin_platform.cue` and
`console/templates/example_httpbin.cue`) available via the **Load httpbin
Example** buttons in the platform template create dialog and the deployment
template create page.

#### Organization-Level Platform Template

The org-level platform template does two things in a single CUE file:

1. **Provides the HTTPRoute** in `platformResources` so the gateway routes
   traffic to the deployment's `Service`.
2. **Closes `projectResources.namespacedResources`** to prevent project
   templates from producing any resource kind other than `Deployment`,
   `Service`, and `ServiceAccount`.

```cue
// Org-level platform template — evaluated at organization scope.
// Any changes here affect every project in the org.

// input and platform are available because platform templates are unified with
// the deployment template before evaluation (ADR 016 Decision 8).
input: #ProjectInput & {
    port: >0 & <=65535 | *8080
}
platform: #PlatformInput

// ── Platform resources (managed by the platform team) ───────────────────────

// platformResources holds resources the platform team manages. The renderer
// reads these only from organization/folder-level templates — project templates
// that define platformResources are silently ignored (ADR 016 Decision 8).
platformResources: {
    namespacedResources: (platform.namespace): {
        // HTTPRoute exposes the deployment's Service via the gateway.
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
    clusterResources: {}
}

// ── Project resource constraints (enforced by the platform team) ─────────────

// Close projectResources.namespacedResources so that every namespace bucket
// may only contain Deployment, Service, or ServiceAccount. Using close() with
// optional fields is the correct CUE pattern: the close() call marks the struct
// as closed (no additional fields allowed), and the ? marks each listed field
// as optional (a namespace bucket need not contain all three). Any unlisted
// Kind key — such as RoleBinding — is a CUE constraint violation at evaluation
// time, before any Kubernetes API call (ADR 016 Decision 9).
projectResources: namespacedResources: [_]: close({
    Deployment?:     _
    Service?:        _
    ServiceAccount?: _
})
```

Key points:
- **`platformResources`** is used because only the platform template render path
  reads it — project templates that accidentally define `platformResources` are
  silently ignored. This keeps platform resources exclusively under platform
  control.
- **`projectResources.namespacedResources: [_]: close({ ... })`** matches every
  namespace bucket. The `close()` call marks the struct as closed so no additional
  fields are allowed. Each listed Kind field carries `?` (optional) so a namespace
  bucket need not contain all three kinds. Any unlisted Kind key — such as
  `RoleBinding` — causes a CUE evaluation error before any Kubernetes API call.
- The `input.name` and `platform.namespace` references work because platform
  templates are concatenated with the deployment template before compilation —
  both `input` and `platform` are fully resolved.

#### Project-Level Deployment Template

The project-level template deploys `go-httpbin`. It produces only
`ServiceAccount`, `Deployment`, and `Service` — exactly the three kinds allowed
by the org-level constraint above.

```cue
// Project-level deployment template for go-httpbin.
// Produces: ServiceAccount, Deployment, Service.
// Allowed by the org constraint: Deployment, Service, ServiceAccount.

input: #ProjectInput & {
    name:  =~"^[a-z][a-z0-9-]*$"
    image: string | *"ghcr.io/mccutchen/go-httpbin"
    tag:   string | *"2.21.0"
    port:  >0 & <=65535 | *8080
}
platform: #PlatformInput

_labels: {
    "app.kubernetes.io/name":       input.name
    "app.kubernetes.io/managed-by": "console.holos.run"
}

_annotations: {
    "console.holos.run/deployer-email": platform.claims.email
}

projectResources: {
    namespacedResources: (platform.namespace): {
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

        // Deployment runs the go-httpbin container.
        // go-httpbin listens on port 8080 by default and needs no special
        // command or args — the image's default entrypoint works.
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
                            ports: [{containerPort: input.port, name: "http"}]
                        }]
                    }
                }
            }
        }

        // Service exposes port 80 → container port input.port.
        // The HTTPRoute in the org platform template routes gateway traffic here.
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
    }

    clusterResources: {}
}
```

Key points:
- The `ReferenceGrant` present in the default template is omitted here — the
  org-level constraint only allows `Deployment`, `Service`, `ServiceAccount`.
  The gateway team is expected to manage cross-namespace traffic permissions at
  the org level. If your org template needs a `ReferenceGrant`, add
  `ReferenceGrant?: _` to the `close()` call and produce it from the system
  template's `platformResources`.
- `go-httpbin` needs no command override — the image's default entrypoint
  listens on `$PORT` (default `8080`) and the template's `input.port` default
  matches. A `GET /get` request returns 200 and is a simple health-check.

#### What Happens When a Project Template Violates the Constraint

When the org-level platform template closes `projectResources.namespacedResources`
to `Deployment`, `Service`, and `ServiceAccount`, any project template that
tries to add a disallowed Kind gets a CUE evaluation error immediately — before
any Kubernetes API call.

**Project template with a disallowed Kind:**

```cue
// This template tries to create a RoleBinding — not in the org's allowed list.
projectResources: namespacedResources: (platform.namespace): {
    RoleBinding: "my-binding": {
        apiVersion: "rbac.authorization.k8s.io/v1"
        kind:       "RoleBinding"
        metadata: {
            name:      "my-binding"
            namespace: platform.namespace
            labels: {"app.kubernetes.io/managed-by": "console.holos.run"}
        }
        roleRef: {
            apiGroup: "rbac.authorization.k8s.io"
            kind:     "ClusterRole"
            name:     "view"
        }
        subjects: [{
            kind:      "ServiceAccount"
            name:      "my-app"
            namespace: platform.namespace
        }]
    }
}
```

**CUE evaluation error:**

```
projectResources.namespacedResources.<ns>.RoleBinding: field not allowed
```

The error names the exact field path. Because it is a CUE evaluation error, it
is reported by the `RenderDeploymentTemplate` preview RPC and by the deployment
create/update RPC before any Kubernetes call is attempted.

#### Enforcement Layers

The system uses three complementary enforcement layers, applied in order:

| Layer | Mechanism | When it runs |
|-------|-----------|--------------|
| **Layer 1 — CUE (early)** | Org template closes `projectResources.namespacedResources` struct → project template gets a CUE evaluation error for any unlisted Kind | At CUE evaluation time, before any Go or Kubernetes call |
| **Layer 2 — Go safety net** | `allowedKindSet` in `render.go` and `allowedKinds` in `apply.go` validate every Kind after evaluation | After CUE evaluation, before Kubernetes apply |
| **Layer 3 — Go hard boundary** | The project-level renderer (`Render()`) does not read `platformResources` from project templates (ADR 016 Decision 8) | At render time, unconditionally |

Layer 1 provides the earliest and most actionable feedback — the template
preview RPC surfaces the error before any deployment is created. Layer 2 is a
hardcoded safety net that catches Kinds that slip past Layer 1 (for example,
when no org-level constraint is defined). Layer 3 is structural: `platformResources`
is simply not read from project-level templates, so a project template cannot
contribute platform resources regardless of what it defines.

### Linking Platform Templates

Platform templates are opt-in: a non-mandatory enabled platform template only
participates in a deployment render when the deployment template explicitly links
it. Mandatory platform templates always participate regardless of linking.

**Why explicit linking?**

An organization may have multiple platform templates targeting different
archetypes — one that routes traffic through a public gateway, another for
internal-only services, a third for batch jobs. Without explicit linking, every
deployment would unify with all enabled templates simultaneously, causing
conflicts when templates close disjoint resource structs. Explicit linking lets
the deployment template declare exactly which platform templates apply, enabling
multiple non-overlapping archetypes to coexist in the same organization.

**How to link templates in the editor**

When creating or editing a deployment template, the form shows a
"Linked Platform Templates" section listing all enabled platform templates for
the organization. Each non-mandatory template has a checkbox — check it to
include that template in every render of this deployment template. Mandatory
templates appear pre-checked with a lock icon; they are always included and
cannot be deselected.

**Render set formula**

The set of platform templates that participate in a render is:

```
render_set = (mandatory AND enabled) UNION (enabled AND name IN linked_list)
```

The `linked_list` is the `linked_org_templates` annotation on the deployment
template ConfigMap (JSON string array, annotation key
`console.holos.run/linked-org-templates`). Disabled templates are never
included, even if they appear in the linked list.

**Authoring implications**

Because explicit linking is scoped per deployment template, different deployment
templates in the same project can link different platform templates. This means:

- A public-facing service links the "HTTPRoute gateway" platform template.
- An internal worker links the "internal network" platform template.
- Both templates share the same org and project without conflicting.

A platform template that should apply universally without any action from
product engineers must set `mandatory = true`. The platform engineer controls
the `mandatory` flag via the platform template editor.

### Previewing Your Template

Use the `RenderDeploymentTemplate` RPC to preview a template without creating a
deployment. This accepts a `cue_template` (raw CUE source) and a `cue_input`
(valid CUE source that supplies concrete values for template parameters),
returning the rendered resources as multi-document YAML (`rendered_yaml`) and as
a pretty-printed JSON array (`rendered_json`). Useful for validating templates
during authoring.

The RPC accepts two separate CUE input fields:

- `cue_platform_input` — trusted platform context (project, namespace, claims); populated by the backend from authenticated context when provided by the caller
- `cue_input` — user-provided deployment parameters (name, image, tag, env, etc.)

Both are valid CUE source. The backend combines them into a single document before unifying with the template, so both `platform` and `input` top-level fields are available.

Example `cue_platform_input` (trusted, set from authenticated context):

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

## Template Defaults

Templates can declare default values for `#ProjectInput` fields using a `defaults` block. The
backend reads this block to pre-fill the Create Deployment form, so users see sensible starting
values without having to know which image or port the template expects.

### The `defaults` + `input` pattern

Declare defaults as a concrete `#ProjectInput` value at the top level of your template:

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
```

Then wire each default field into `input` using CUE's `*preferred | alternative` syntax:

```cue
// input wires defaults as overridable. User-supplied values from the form
// override these defaults at render time via CUE unification.
input: #ProjectInput & {
    name:        *defaults.name        | _
    image:       *defaults.image       | _
    tag:         *defaults.tag         | _
    description: *defaults.description | _
    port:        *defaults.port        | _
    env:         [...#EnvVar] | *[]
}
```

The `*value | _` syntax makes `value` the CUE default while `_` (top) allows any override. At
render time, the backend calls `FillPath("input", userInput)` to unify the form values with
`input`. If a field is left at its zero value in the form, the CUE default wins. If the user
fills in a value, that concrete value wins.

### How defaults are extracted

When the backend loads a template (in `GetDeploymentTemplate` or `ListDeploymentTemplates`),
it evaluates the CUE source and reads the `defaults` path. It maps the concrete field values
to `DeploymentDefaults` in the proto response:

```
defaults.name        → DeploymentDefaults.name
defaults.image       → DeploymentDefaults.image
defaults.tag         → DeploymentDefaults.tag
defaults.description → DeploymentDefaults.description
defaults.port        → DeploymentDefaults.port
```

The frontend receives these fields and uses them to pre-fill the Create Deployment form.

Templates that do not have a `defaults` block continue to work unchanged. If a template was
authored before this pattern existed and stored defaults in a ConfigMap annotation (the legacy
approach), those annotation values are still read as a fallback.

### The `description` field

`description` is an optional field on `#ProjectInput` that holds a short human-readable
description of the deployment. It appears in the Create Deployment form as a pre-filled
description that users can change.

```cue
defaults: #ProjectInput & {
    // description is displayed in the Create Deployment form.
    description: "A simple HTTP Request & Response Service"
    // ... other fields
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
    name:         string             // deployment name
    image:        string             // container image repository (no tag)
    tag:          string             // image tag
    command?:     [...string]        // container ENTRYPOINT override
    args?:        [...string]        // container CMD override
    env?:         [...#EnvVar]       // environment variables
    port:         int                // container port (default applied by template)
    description?: string             // short human-readable description (optional)
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

| Field         | Type           | Required | Description |
|---------------|----------------|----------|-------------|
| `name`        | `string`       | Yes      | Deployment name. Must be a valid DNS label (`^[a-z][a-z0-9-]*$`). |
| `image`       | `string`       | Yes      | Container image repository (e.g. `ghcr.io/holos-run/holos-console`). |
| `tag`         | `string`       | Yes      | Image tag (e.g. `v1.2.3`, `latest`). |
| `command`     | `[...string]`  | No       | Overrides the container `ENTRYPOINT`. Omitted when not set. |
| `args`        | `[...string]`  | No       | Overrides the container `CMD`. Omitted when not set. |
| `env`         | `[...#EnvVar]` | No       | Container environment variables. Defaults to `[]`. |
| `port`        | `int`          | No       | Container port the application listens on. Must be between 1 and 65535. Defaults to `8080`. The default template names this port `"http"` and creates a Service that maps port 80 to this target. |
| `description` | `string`       | No       | Short human-readable description of the deployment. Used in the `defaults` block to pre-fill the Create Deployment form description field. |

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
respectively, but are reserved for platform template resources managed by the platform
operator. User-authored deployment templates should NOT define these fields.

**Platform Template Unification**

At deploy time, the console unifies the applicable platform templates with the
deployment template before CUE compilation. The render set is:

  `(mandatory AND enabled)` UNION `(enabled AND name IN linked list)`

Mandatory templates apply regardless of linking. Non-mandatory enabled templates
unify only when the deployment template explicitly links them (see
[Linking Platform Templates](#linking-platform-templates)).

Because all templates share the same generated schema (prepended by the renderer),
they have full access to all `input.*` and `platform.*` fields — including
`input.name`, `input.port`, `platform.namespace`, and `platform.gatewayNamespace`.

Platform templates may define resources under `platformResources` and/or `projectResources`
(ADR 016 Decision 8). The built-in example and operator-managed resources conventionally
use `platformResources` to signal their intent, but there is no rigid separation — all
template output is unified by CUE before the renderer reads either collection.

**Operator Guarantees**

Resources placed in `platformResources.namespacedResources` and
`platformResources.clusterResources` by platform templates are:

- **Not produced by project-level templates** — the renderer enforces a hard boundary
  (ADR 016 Decision 8) in Go code: when rendering a project-level template (`Render()`),
  it does not read `platformResources` from the evaluated CUE value. A project template
  that defines `platformResources` fields is valid CUE but the values are silently
  ignored. Only the organization/folder-level path (`RenderWithOrgTemplates()`) reads
  both collections.
- **Always applied alongside user resources** — the render engine collects resources
  from all four output fields at the organization/folder level and applies them in a
  single server-side apply pass. This guarantees that operator-required resources
  (e.g., `HTTPRoute`, network policies) are always present whenever the deployment
  is applied.

**Operator Constraints via Platform Templates**

Operators can use platform templates to constrain user-controlled resources. Because
platform templates are unified with the deployment template before compilation, a
platform template can add CUE constraints on `projectResources.namespacedResources` (the project
output fields). For example, a platform template could enforce a required label or
annotation on all namespaced resources — any user template that violates the
constraint will fail at CUE evaluation time before any Kubernetes call.

For a full walkthrough of this pattern including complete CUE examples and an
explanation of the three enforcement layers, see
["Platform and Project Templates Working Together"](#platform-and-project-templates-working-together)
in the "Writing a Custom Template" section.

**Example: HTTPRoute Platform Template**

The built-in `default_referencegrant.cue` platform template seeds a disabled HTTPRoute
example. When enabled, it adds an `HTTPRoute` to `platformResources.namespacedResources`
that routes all gateway traffic to the deployment's `Service`:

```cue
// platformResources contributes platform-managed Kubernetes resources.
// Any template at any level can define values for both platformResources and
// projectResources. The renderer reads platformResources from organization and
// folder templates (not project templates). See ADR 016 Decision 8.
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

This platform template references `input.name` (project-supplied deployment name),
`platform.namespace` (resolved project namespace), and `platform.gatewayNamespace`
(operator-configured gateway namespace) — all available because platform templates
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
| `console/org_templates/default_referencegrant.cue` | Built-in example HTTPRoute platform template (code: `OrgTemplate`). References `input.name` and `platform.gatewayNamespace` — designed to be unified with the deployment template at deploy time. Contributes resources to `platformResources.namespacedResources`. Seeded as disabled (not mandatory) on first `ListOrgTemplates` access. Embedded via `console/org_templates/embed.go`. No `package` declaration. |
| `api/v1alpha1/` | Go type definitions for `PlatformInput`, `ProjectInput`, `Claims`, `EnvVar`, `KeyRef`, `PlatformResources`, `ProjectResources`. CUE schemas (`#PlatformInput`, `#ProjectInput`, etc.) are generated from these types via `cue get go` and embedded into the binary. The renderer prepends the generated schema before compiling any template. |

### Go Rendering Pipeline

Two render paths exist — one for the deployment service and one for the template preview RPC:

| File | Purpose |
|------|---------|
| `console/deployments/render.go` | `CueRenderer.Render()` — project-level render path: prepends generated schema, compiles CUE source, fills `"input"` and `"platform"`, then calls `evaluateStructured(..., false)` which reads only `projectResources` (ADR 016 Decision 8 hard boundary — `platformResources` is intentionally skipped). |
| `console/deployments/render.go` | `CueRenderer.RenderWithOrgTemplates()` — organization/folder-level render path: unifies platform template sources with the deployment template before compilation, then calls `evaluateStructured(..., true)` which reads both `projectResources` and `platformResources`. |
| `console/deployments/render.go` | `CueRenderer.RenderWithCueInput()` — template preview path: concatenates generated schema, CUE source, and a raw CUE input string before compilation so cross-references (e.g. `input.name` used in platform templates) resolve correctly. Extracts `platform.namespace` from the compiled value. Calls `evaluateStructured(..., true)` to read both collections. |
| `console/deployments/render.go` | `PlatformInput`, `ProjectInput` structs in `api/v1alpha1` — split Go representation of template inputs. `PlatformInput` (project, namespace, gatewayNamespace, organization, claims) is trusted backend context; `ProjectInput` (name, image, tag, etc.) is user-supplied. |
| `console/deployments/render.go` | `validateResource()` — enforces kind allowlist and managed-by label on a single resource. `evaluateStructured(unified, ns, readPlatformResources)` reads `projectResources.*` always and `platformResources.*` only when `readPlatformResources` is true; dispatches to `walkNamespacedResources()` and `walkClusterResources()` which add namespace-match and struct-key consistency checks. |
| `console/deployments/apply.go` | `Applier.Apply()` — injects ownership label, performs server-side apply with field manager `console.holos.run`. |
| `console/deployments/apply.go` | `Applier.Reconcile()` — calls `Apply()` then deletes owned resources whose (kind, name) is not in the desired set (orphan cleanup). Used by `UpdateDeployment`. Orphan cleanup is skipped if apply fails so the previously working state is preserved. |
| `console/deployments/apply.go` | `Applier.Cleanup()` — deletes all resources matching the ownership label selector. Used by `DeleteDeployment` (unconditional removal) and `CreateDeployment` rollback. |

### Template Service

| File | Purpose |
|------|---------|
| `console/templates/handler.go` | `DeploymentTemplateService` handler — CRUD for templates stored as ConfigMaps. |
| `console/templates/k8s.go` | ConfigMap storage: templates stored with `template.cue` data key, `deployment-template` resource-type label. |
| `console/templates/render_adapter.go` | `CueRendererAdapter` — wraps `deployments.CueRenderer` to produce YAML and structured object data for the template preview RPC. |

### Platform Template Service (code: `OrgTemplateService`)

| File | Purpose |
|------|---------|
| `console/org_templates/handler.go` | `OrgTemplateService` handler — CRUD and render for org-scoped platform templates stored as ConfigMaps. Edit access requires `PERMISSION_ORG_TEMPLATES_WRITE`. |
| `console/org_templates/k8s.go` | ConfigMap storage: templates stored with `template.cue` data key, `org-template` resource-type label, `mandatory` and `enabled` annotations. Seeds `default_referencegrant.cue` (HTTPRoute example) on first `ListOrgTemplates`. `ListOrgTemplateSourcesForRender(ctx, org, linkedNames)` implements the explicit linking formula `(mandatory AND enabled) UNION (enabled AND name IN linkedNames)` (satisfies `deployments.OrgTemplateProvider`). |
| `console/org_templates/apply.go` | `MandatoryTemplateApplier.ApplyMandatoryOrgTemplates()` — called by the projects service after project namespace creation to apply platform templates that are both `mandatory=true` AND `enabled=true`. |

### Deployment Service

| File | Purpose |
|------|---------|
| `console/deployments/handler.go` | Create flow — builds `PlatformInput` and `ProjectInput`, calls `renderResources()`, then `Apply()`. All-or-nothing: if render or apply fails, `rollbackCreate()` calls `Cleanup()` then `DeleteDeployment()` to remove partial state. Update flow uses `Reconcile()` instead of `Apply()` so orphaned resources are cleaned up after a successful apply. |
| `console/deployments/handler.go:607-656` | `protoToEnvVarInput()` / `envVarInputToProto()` — converts between protobuf `EnvVar` and `EnvVarInput` for CUE. |
| `console/deployments/k8s.go` | ConfigMap storage for deployment state: image, tag, template, command, args, env stored as data keys. |

### Protobuf Definitions

| File | Purpose |
|------|---------|
| `proto/holos/console/v1/deployments.proto` | `Deployment`, `EnvVar`, `SecretKeyRef`, `ConfigMapKeyRef` messages; `DeploymentService` RPCs. |
| `proto/holos/console/v1/deployment_templates.proto` | `DeploymentTemplate` message; `DeploymentTemplateService` RPCs including `RenderDeploymentTemplate`. |
| `proto/holos/console/v1/org_templates.proto` | `OrgTemplate` message (platform template); `OrgTemplateService` RPCs including `RenderOrgTemplate`. |

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

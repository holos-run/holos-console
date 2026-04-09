# Deployment Guide

This guide explains the holos-console deployment feature to engineers who are
new to it. It covers the template hierarchy, the resource model, and how CUE
unification merges templates from every level of the hierarchy into two resource
collections that the renderer applies to Kubernetes.

For canonical definitions of every term used here, see the
[Glossary](glossary.md). For step-by-step instructions on writing a template,
see the [CUE Template Guide](cue-template-guide.md). For architectural
rationale, see [ADR 016](adrs/016-config-management-resource-schema.md) and
[ADR 017](adrs/017-config-management-rbac-levels.md).

---

## Overview

A **deployment** is a live instance of a containerized application running in a
Kubernetes namespace. Creating a deployment requires a **deployment template** —
a CUE file that describes the Kubernetes resources the application needs
(Deployment, Service, ServiceAccount, etc.).

Three roles interact with the deployment feature:

| Role | What they do |
|------|-------------|
| **Product engineer** | Writes deployment templates in their project. Defines image, ports, env vars, and other application config. |
| **Site reliability engineer (SRE)** | Writes templates at a folder level (planned `v1alpha2`) to enforce operational standards — resource limits, health checks, monitoring sidecars — across a group of projects. |
| **Platform engineer** | Writes platform templates at the organization level to enforce platform-wide policy — network routing, security standards, namespace quotas. |

These roles are not mutually exclusive. A single person may operate at multiple
levels depending on the task.

---

## Template Hierarchy

Templates live at specific levels in the organizational hierarchy. The renderer
collects templates from every level and unifies them into a single evaluated
value using CUE unification.

```
Organization  (platform engineer writes platform templates here)
    │
    ├── Folder  (SRE writes templates here — planned v1alpha2)
    │     │
    │     └── Folder  (up to 3 folder levels supported)
    │           │
    └─────────── Project  (product engineer writes deployment template here)
```

The diagram below shows how templates, inputs, and resource collections fit
together:

![Resource Model](adrs/014-resource-model.svg)

In `v1alpha1`, the hierarchy is Organization → Project (two levels). Folder
support is planned for `v1alpha2`. The schema is designed to accommodate folders
from the start so that adding them in `v1alpha2` does not require breaking
changes.

### Who writes templates at each level

**Organization level** — Platform engineers write **platform templates** here.
A platform template applies to every project in the organization. It can:
- Add Kubernetes resources outside the project namespace (HTTPRoutes in the
  gateway namespace, NetworkPolicies, ReferenceGrants) to `platformResources`.
- Define CUE values in `projectResources` that every project template must
  satisfy — for example, requiring a specific label on every Deployment, setting
  a minimum replica count, or closing the resource struct to restrict which
  Kubernetes Kinds a project template may produce.

Platform templates are stored as Kubernetes ConfigMaps in the organization
namespace. Edit access requires `PERMISSION_ORG_TEMPLATES_WRITE`, granted
only to org-level Owners. In code, platform templates use the identifier
`OrgTemplate` and the service `OrgTemplateService` (see the
[Glossary — Naming History Note](glossary.md#naming-history-note)).

**Folder level** — SREs write templates here to enforce standards for a group
of projects. The renderer reads both `platformResources` and `projectResources`
from folder templates. Folder support is planned for `v1alpha2`.

**Project level** — Product engineers write **deployment templates** here. A
deployment template defines the application resources for a single deployment.
The renderer reads `projectResources` from project-level templates and
intentionally ignores any `platformResources` fields — project templates cannot
affect platform-controlled resources. Deployment templates are stored as
Kubernetes ConfigMaps in the project namespace.

---

## Resource Model

Every deployment produces two resource collections:

### platformResources

The `platformResources` collection holds Kubernetes resources managed by
platform and SRE engineers. These resources typically live outside the project
namespace.

**Typical contents:**
- `HTTPRoute` in the gateway namespace (external traffic routing)
- `NetworkPolicy` for the project namespace
- `ReferenceGrant` (cross-namespace traffic access)
- `ResourceQuota`, `LimitRange`

**Key rule:** The renderer reads `platformResources` only from templates at the
organization and folder levels. A project-level deployment template that defines
`platformResources` fields has no effect — those values are silently ignored by
the renderer. This boundary is enforced in Go code, not in CUE.

### projectResources

The `projectResources` collection holds Kubernetes resources managed by product
engineers, scoped to the project namespace.

**Typical contents:**
- `Deployment` (your application container)
- `Service` (internal traffic routing)
- `ServiceAccount` (pod identity)
- `ConfigMap`, `Secret` (application configuration)

**Key rule:** The renderer reads `projectResources` from templates at all
levels — organization, folder, and project. All levels can define values for
this collection; CUE unification merges them.

### Renderer scope table

| Template level | Renderer reads `projectResources` | Renderer reads `platformResources` |
|---------------|-----------------------------------|-------------------------------------|
| Organization  | Yes | Yes |
| Folder        | Yes | Yes |
| Project       | Yes | No  |

---

## How Template Unification Works

At render time, the console performs these steps:

1. Fills **platform input** (`platform.*`) from the authenticated server
   context: project name, namespace, gateway namespace, organization, and OIDC
   claims.
2. Fills **project input** (`input.*`) from the deployment creation form or API
   request: image, tag, name, port, env vars, etc.
3. Collects the applicable platform templates using the explicit linking formula: mandatory AND enabled templates always participate; additionally, enabled templates that are explicitly linked to the deployment template (stored in the `console.holos.run/linked-org-templates` annotation) are included. See [Linking Platform Templates](cue-template-guide.md#linking-platform-templates).
4. Prepends the generated CUE schema (from `api/v1alpha1` Go types via
   `cue get go`) so type definitions like `#ProjectInput` and `#PlatformInput`
   are available without explicit imports.
5. Unifies all template sources — platform templates and the deployment template
   — into a single CUE value.
6. Reads `platformResources` from org/folder template contributions and
   `projectResources` from all template contributions.
7. Validates each resource against safety constraints and applies the resources
   to Kubernetes via server-side apply.

### CUE unification is not a pipeline

A common misconception is that higher-level templates "override" lower-level
ones, or that lower-level templates "fill in" values that higher-level templates
leave blank. Neither is correct.

**CUE unification is commutative, associative, and idempotent.** The order of
template collection does not affect the result. Every template at every level
defines values — concrete values, constraints, types, or top (`_`). CUE unifies
all of them. A conflict (`_|_`) at any field causes evaluation to fail with a
CUE error before any Kubernetes API call.

**Example.** A platform template defines a constraint on Deployment replicas:

```cue
// Platform template (org level)
projectResources: [_]: {
    Deployment: [_]: spec: replicas: >=2
}
```

A deployment template defines a concrete value:

```cue
// Deployment template (project level)
projectResources: (platform.namespace): {
    Deployment: (input.name): spec: replicas: 3
}
```

CUE unifies these two values. The result is `replicas: 3` because `3` satisfies
the constraint `>=2`. Both templates defined a value for the same field — one
defined a constraint, the other a concrete value. CUE treats them identically as
plain old values to be unified.

If the deployment template had instead set `replicas: 1`, CUE unification would
fail at evaluation time:

```
projectResources.<ns>.Deployment.<name>.spec.replicas: 2 (not 1): constraint failed:
    ./platform_template.cue:3:30
```

The error is reported before any Kubernetes API call.

### Values flow downward, not upward

Organization and folder templates can define values in `projectResources` that
get unified with the project template's values. Values cannot flow upward: a
project template cannot affect `platformResources` because the renderer does not
read that field from project-level templates.

This is not a CUE-level restriction — CUE itself does not distinguish "up" from
"down." It is a Go-level decision: the renderer simply does not read
`platformResources` from the project template's evaluated contribution.

### There is no distinction between "writing" and "constraining"

ADR 016 corrects a misconception from earlier design documents: CUE has no
separate "constrain" operation. Every value in CUE — `replicas: 3`, `replicas:
>=2`, `replicas: int`, `replicas: _` — is a plain value that gets unified with
other values. The distinction between "writing a concrete value" and "defining a
constraint" is a human mental model, not a CUE language feature.

Platform engineers who want to restrict what project templates can produce use
regular CUE unification — the same mechanism used everywhere else in the
language.

---

## Inputs

Every template receives two sources of input:

### Platform input (`platform.*`)

Platform input is set by the backend from the authenticated server context.
Template authors can rely on these values being correct and verified. Fields
include:

| Field | Description |
|-------|-------------|
| `platform.project` | The project name |
| `platform.namespace` | The Kubernetes namespace for the project |
| `platform.gatewayNamespace` | The gateway namespace (default: `istio-ingress`) |
| `platform.organization` | The organization name |
| `platform.claims.email` | The authenticated user's email (from OIDC ID token) |

Go type: `api/v1alpha1.PlatformInput`

### Project input (`input.*`)

Project input is provided by the product engineer via the deployment creation
form or the API request. Template authors should treat these as user-supplied
values and apply appropriate CUE constraints. Fields include:

| Field | Description |
|-------|-------------|
| `input.name` | The deployment name (must be a valid DNS label) |
| `input.image` | The container image repository |
| `input.tag` | The image tag |
| `input.port` | The container port (default: `8080`) |
| `input.command` | Container entrypoint override (optional) |
| `input.args` | Container command override (optional) |
| `input.env` | Container environment variables (optional) |

Go type: `api/v1alpha1.ProjectInput`

---

## Platform Templates: A Closer Look

Platform templates (code identifier: `OrgTemplate`) are organization-level
CUE templates that platform engineers use to define policy across all projects.

### Mandatory and enabled flags

Each platform template has two flags:

- **mandatory** — Applied to the project namespace at project creation time.
  For example, a mandatory platform template might create a `NetworkPolicy` that
  allows only intra-namespace traffic by default. Mandatory AND enabled templates
  are also always unified at render time, regardless of whether the deployment
  template links them.
- **enabled** — Makes the template available for linking and render-time
  unification. A disabled template is never unified, even if it appears in a
  deployment template's linked list. A mandatory template that is not enabled is
  still applied at project-namespace creation time but not at render time.

### Explicit linking

Non-mandatory enabled platform templates are **opt-in**: they unify at render
time only when the deployment template explicitly links them. The deployment
template stores its linked list as the annotation
`console.holos.run/linked-org-templates` (a JSON string array of template names)
on the deployment template ConfigMap.

**Render set formula:**
```
render_set = (mandatory AND enabled) UNION (enabled AND name IN linked_list)
```

This explicit linking model allows multiple non-overlapping platform template
archetypes to coexist in the same organization. A public-facing service can link
the HTTPRoute gateway template; an internal worker can link an internal-network
template — both without conflict.

### End-to-end linking workflow

**Platform engineer** — create and enable a platform template:

1. Navigate to the organization → **Platform Templates** tab.
2. Click **Create Platform Template** and author the CUE source.
3. Set **enabled = true** so the template is available for linking.
4. Optionally set **mandatory = true** if the template should apply to all
   deployments without opt-in.

**Product engineer** — link the platform template from a deployment template:

1. Navigate to the project → **Templates** tab.
2. Open the deployment template (or create a new one).
3. In the **Linked Platform Templates** section, check the platform template(s)
   you want to unify. Mandatory templates appear pre-checked with a lock icon
   and cannot be deselected.
4. Save the deployment template.

**Deploy** — the next time a deployment is created or updated using this
template, the linked platform templates are automatically unified with the
deployment template at render time.

### Closing the projectResources struct

One of the most powerful uses of a platform template is closing the
`projectResources` struct to restrict which Kubernetes Kinds a project template
may produce:

```cue
// Platform template: restrict project templates to safe Kinds only.
import "list"

_allowedKinds: ["ConfigMap", "Deployment", "Service", "ServiceAccount"]

projectResources: [_]: {
    for kind in _allowedKinds {
        (kind): _
    }
}
```

With this in place, any project template that tries to produce a
`ClusterRoleBinding` fails at CUE evaluation time with a clear error — before
any Kubernetes API call.

The Go renderer's allowed-kinds list in `apply.go` is a second line of defense,
but CUE-level enforcement is earlier, more informative, and configurable per
organization.

### The httpbin example

The built-in `example_httpbin_platform.cue` platform template is a working
example that:
- Produces an `HTTPRoute` in `platformResources` to expose the project publicly.
- Closes `projectResources.namespacedResources` to `Deployment`, `Service`, and
  `ServiceAccount` — the three Kinds the companion project-level
  `example_httpbin.cue` template produces.

This example is stored in the organization namespace as a disabled (not
mandatory) platform template. Enable it and pair it with the project-level
httpbin template to see the full two-level unification in action.

---

## Getting Started

The fastest way to create a deployment is to use the built-in default template:

1. **Create an organization** and a **project** in the console UI.
2. Navigate to the project and open the **Templates** tab.
3. The default deployment template is pre-loaded. It produces a
   `ServiceAccount`, `Deployment`, `Service`, and `ReferenceGrant`.
4. Click **Create Deployment** and fill in the image, tag, and port fields.
5. The console renders the template, validates the output, and applies the
   resources to Kubernetes.

To customize the template, click the template name to open the template editor.
The editor shows the CUE source and a live preview panel that renders the
template with the current project input values.

For a complete walkthrough of writing a custom template — including the port
flow from `input.port` to container port to Service `targetPort` to HTTPRoute
— see the [CUE Template Guide](cue-template-guide.md).

---

## Further Reading

| Document | What it covers |
|----------|---------------|
| [Glossary](glossary.md) | Canonical definitions for all deployment feature terms |
| [CUE Template Guide](cue-template-guide.md) | Step-by-step template authoring, complete examples, input/output reference |
| [ADR 016: Configuration Management Resource Schema](adrs/016-config-management-resource-schema.md) | Architectural decisions: Go struct API types, CUE unification model, resource collections, hierarchy |
| [ADR 017: Configuration Management RBAC Levels](adrs/017-config-management-rbac-levels.md) | Architectural decisions: RBAC cascade model, renderer scope table, permission design for template authoring |
| [Permissions Guide](permissions-guide.md) | Cascade table pattern, naming conventions, narrow scoping |
| [Advanced User Guide](advanced-user-guide.md) | Full RPC surface reference and end-to-end scenario walkthrough |

<!--
Copyright 2026 The Holos Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
-->

# ADR 034: Namespace TemplatePolicyBinding for new Projects (HOL-806)

- Status: Accepted
- Date: 2026-04-21
- Binary: `holos-console` (`console/projects/`)
- Follows: [ADR 029 — TemplatePolicyBinding target_refs wildcards](https://github.com/holos-run/holos-console-docs/blob/main/docs/adrs/029-template-policy-binding-split.md)

## Context

When a `Project` is created via the `CreateProject` RPC, the console
provisions a Kubernetes `Namespace` for the project using the project's
slug as the namespace name. Today this namespace is created with a fixed
set of labels and annotations — operators have no way to inject
organization-specific labels, attach custom annotations, or co-create
namespace-scoped resources (e.g. a `ReferenceGrant`) as part of project
provisioning.

A `TemplatePolicyBinding` already supplies the mechanism for attaching a
`TemplatePolicy` (and, transitively, CUE-rendered resources) to a render
target. The existing `TemplatePolicyBindingTargetKind` enum has two values:
`ProjectTemplate` (targets a project-scope Template) and `Deployment`
(targets a single Deployment). Adding `ProjectNamespace` as a third kind
lets an ancestor-namespace binding point at the namespace that will be
created during `CreateProject`.

Three design choices needed explicit decisions before implementation:

1. **Scope**: should this cover only new project namespaces, or also
   folder-backed and organization-backed namespaces?
2. **CUE source-of-truth**: should the feature read `platformResources`
   or `projectResources` from the matched templates?
3. **Apply order**: after rendering, in what order should cluster-scoped
   resources, the namespace itself, and namespace-scoped resources be
   applied, and what retry strategy guards against the namespace-readiness
   race?

### Option A: Scope = Project, Folder, and Organization namespaces

Extend the mechanism at once to cover every hierarchy level that
provisions a namespace on creation.

Pros:
- Single feature request; no need for a follow-up.
- Uniform semantics for all hierarchy-object creation events.

Cons:
- `CreateFolder` and `CreateOrganization` have different RBAC callers,
  different namespace labels, and different blast-radius ceilings than
  `CreateProject`. Shipping them together couples three distinct code paths
  for a feature that is currently scoped to projects only.
- Higher risk in the first phase; harder to reason about correctness.

### Option B: Scope = new Project creation only (deferred follow-up for Folders/Orgs)

Restrict this ADR and implementation to `CreateProject`. Folders and
Organizations are noted as an open question tracked by a placeholder issue.

Pros:
- Implementation surface is tightly bounded.
- `CreateProject` is the code path with the most operator demand (the
  original issue was filed against project creation specifically).
- Folder/Organization namespace customization can be revisited once the
  project case is proven in production.

Cons:
- Follow-up work required for the folder/organization case.

### Option C: platformResources vs. projectResources

ADR 016 Decision 8 establishes a two-level render model: organization- and
folder-level renders read both `platformResources` and `projectResources`;
project-level renders must not emit `platformResources`. The namespace being
provisioned does not yet exist at render time — the project is being created
right now — and the binding lives in the ancestor (org/folder) namespace,
making this an inherently platform-level operation.

Using `platformResources` preserves the ADR 016 Decision 8 invariant that
platform-level resources flow from platform-level scopes. Using
`projectResources` would let a project-level template accidentally influence
namespace provisioning that belongs to the platform scope, violating the
trust boundary.

## Decision

**Scope (Decision 1):** Adopt **Option B**. Scope is new `Project` creation
only. Folders and Organizations are out of scope for this phase; the open
question is tracked in [HOL-817](https://linear.app/holos-run/issue/HOL-817).

**CUE source-of-truth (Decision 2):** Read from `platformResources` only.
The binding lives in the ancestor (org/folder) namespace; the project does
not yet exist at render time. ADR-016 Decision 8 keeps project-level renders
from emitting platform-level resources and vice-versa — `platformResources`
is the correct half of that boundary.

**New target kind (Decision 3):** Add `ProjectNamespace` as a third value to
`TemplatePolicyBindingTargetKind` (current values: `ProjectTemplate`,
`Deployment`). The existing two values are unchanged. `projectName` on the
`TemplatePolicyBindingTargetRef` accepts the HOL-767 `"*"` wildcard so a
single binding can match every new project under the ancestor.

**Apply order (Decision 4):** The `CreateProject` RPC executes the following
steps after resolving and rendering matched templates:

1. Resolve `ProjectNamespace` bindings from the ancestor chain above the
   project's parent namespace.
2. Render each matched Template with platform inputs; collect
   `platformResources` outputs (cluster-scoped and namespace-scoped).
3. Unify the template-produced `Namespace` object (if any) with the
   RPC-constructed `Namespace` (labels, annotations, finalizers from both
   sides merged). Conflicting field values are a hard error — the operation
   fails closed rather than silently dropping operator intent.
4. Apply cluster-scoped resources via Server-Side Apply (SSA).
5. Apply the unified `Namespace` via SSA.
6. Wait for `Namespace.status.phase == Active`. This is the
   upstream-documented readiness signal emitted by the Kubernetes namespace
   controller
   ([`pkg/controller/namespace`](https://github.com/kubernetes/kubernetes/tree/master/pkg/controller/namespace)).
7. Apply namespace-scoped resources via SSA with exponential-backoff retry
   on `IsNotFound`, `IsForbidden`, `IsServerTimeout`, and `IsInternalError`.
   The retry window is bounded by the request context plus a 30-second
   ceiling. The implementation mirrors the Argo CD applier's
   `applyResource` retry loop
   ([`util/app/applyresource.go`](https://github.com/argoproj/argo-cd/blob/master/util/app/applyresource.go))
   and the Flux kustomize-controller's `applySet` retry pattern
   ([`internal/reconcile/kustomization.go`](https://github.com/fluxcd/kustomize-controller/blob/main/internal/reconcile/kustomization.go)).

The namespace-ready race arises because the Kubernetes RBAC admission plugin
propagates namespace labels asynchronously: even after `Namespace` is `Active`,
a brief window exists before admission controllers and namespace-scoped RBAC
fully propagate. Polling `.status.phase == Active` eliminates the most common
failure mode; exponential-backoff SSA retry covers the remaining
RBAC-propagation window.

### Conventions specific to `ProjectNamespace`

- **Binding namespace**: the ancestor namespace (organization or folder)
  that owns the binding — same as all other `TemplatePolicyBinding` objects.
- **Scope**: `projectName` is the project being created. Accepts `"*"` per
  HOL-767 wildcard semantics so one binding can match all new projects under
  the ancestor.
- **`name` field**: not meaningful for `ProjectNamespace` targets (there is
  exactly one namespace per project). The field must be set to `"*"` when
  creating a `ProjectNamespace` binding; the resolver ignores its value.
- **Render level**: always org/folder-level (i.e. `ReadPlatformResources =
  true`). Platform inputs are supplied; `platformResources` is the only
  output collection read (matching ADR 016 Decision 8).
- **Conflict handling**: if two matched templates both produce a
  `Namespace` object with conflicting field values, the entire `CreateProject`
  operation fails with a descriptive error. Partial application is never
  performed.
- **Admission policy**: the existing
  `templatepolicybinding-folder-or-org-only` `ValidatingAdmissionPolicy`
  already enforces that `TemplatePolicyBinding` objects cannot be created in
  project-labeled namespaces. No new admission policy is required for the new
  kind value.
- **Idempotency**: `CreateProject` is idempotent (existing project returns
  `AlreadyExists`). The namespace-provisioning path will not re-apply
  templates if the namespace already exists and is `Active`.

### Files introduced or modified

| Path | Change |
|------|--------|
| `api/templates/v1alpha1/template_policy_binding_types.go` | Add `TemplatePolicyBindingTargetKindProjectNamespace` constant |
| `config/holos-console/crd/templates.holos.run_templatepolicybindings.yaml` | Regenerate via `make manifests` |
| `console/policyresolver/` | Resolver changes to match `ProjectNamespace` bindings |
| `console/projects/` | New namespace applier with retry; wire into `CreateProject` |
| `console/templates/examples/` | Two embedded example templates |
| `frontend/src/components/template-policy-bindings/` | `TargetRefEditor` and `BindingForm` support for new kind |
| `docs/adrs/034-namespace-template-policy-binding-for-new-projects.md` | This ADR |

Code generation (`make manifests`) must be run after modifying
`template_policy_binding_types.go` to regenerate the CRD YAML.

## Consequences

- Operators can attach a `TemplatePolicyBinding` (pointing at a
  `TemplatePolicy` / `Template` in the ancestor namespace) to every new
  project creation event via `projectName: "*"` wildcards, injecting custom
  annotations, labels, and co-located namespace-scoped resources
  (e.g. `ReferenceGrant`) without patching the project namespace after the
  fact.
- The `TemplatePolicyBindingTargetKind` enum gains a third value:
  `ProjectNamespace`. Existing `ProjectTemplate` and `Deployment` values
  are unchanged; no migration of existing bindings is required.
- The `CreateProject` RPC acquires a dependency on the policy resolver and
  a new namespace-aware applier. A resolver or applier error during project
  creation fails the RPC; the operator retries the `CreateProject` call.
- Template authors must place namespace customization output under
  `platformResources`, not `projectResources`. Templates that put namespace
  overrides under `projectResources` will have no effect for
  `ProjectNamespace` bindings; this is intentional and consistent with
  ADR 016 Decision 8.

## Open Questions

**Folders and Organizations** — Should the `ProjectNamespace` mechanism
extend to Folder-backed and Organization-backed namespaces? This is
deliberately out of scope for this phase. Tracked in
[HOL-817](https://linear.app/holos-run/issue/HOL-817).
Per `CONTRIBUTING.md` §ADR Open Questions, this question will be closed when
HOL-817 ships or is explicitly abandoned.

## Why colocate this ADR

Per the criteria in `docs/adrs/README.md`: the binary (`holos-console`)
and the affected packages (`console/projects/`, `console/policyresolver/`,
`api/templates/v1alpha1/`) live in this repository, and the review boundary
matches the `CODEOWNERS` boundary for those paths. The TemplatePolicyBinding
lineage (ADR 029 / HOL-767 in `holos-console-docs`, ADR 034 here) follows
the same split as ADR 032 / ADR 033: cross-binary storage contracts in
`holos-console-docs`; binary-specific implementation decisions colocated here.

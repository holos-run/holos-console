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

# ADR 033: RenderState as a sibling CRD (HOL-694)

- Status: Accepted
- Date: 2026-04-19
- Binary: `holos-console` (`console/policyresolver/`)
- Follows: [ADR 032 — TemplateRelease as a sibling CRD](032-template-release-crd.md)
- Supersedes: Render-state ConfigMap storage introduced in HOL-557 / HOL-567

## Context

`AppliedRenderStateClient` (HOL-557 / HOL-567) persists the effective
set of `LinkedTemplateRef` values last applied to a render target so
the policy resolver can detect drift between the stored "applied" set
and the freshly aggregated set from the current TemplatePolicy /
TemplatePolicyBinding graph. Drift is surfaced on the list views
(`DeploymentStatusSummary.policy_drift`) and on the per-target reads
(`GetDeploymentPolicyState`, `GetProjectTemplatePolicyState`).

Storage was a folder-namespace ConfigMap whose name was
`render-state-{kind}-{project}-{target}`, with custom labels
(`console.holos.run/resource-type=render-state`,
`console.holos.run/render-target-kind={kind}`,
`console.holos.run/render-target-project={project}`,
`console.holos.run/render-target-name={target}`) and a single JSON
data key (`console.holos.run/applied-render-set`) carrying the
serialised `[]LinkedTemplateRef`.

HOL-615 marked `AppliedRenderStateClient` as a non-goal of the initial
CRD migration (Template, TemplatePolicy, TemplatePolicyBinding,
TemplateRelease). HOL-694 picks up the remaining
`api/v1alpha2`-encoded ConfigMap path and aligns it with the rest of
the templates group.

The HOL-554 storage-isolation invariant constrains where these
records can live: render-state for a folder-namespace-owned project
**must** live in the folder namespace (or the organization namespace
when the project's immediate parent is the organization), **never**
in a project namespace. Project owners have namespace-scoped write
access and could otherwise forge drift evidence to mask a policy
violation.

Two shapes were considered for the migration target.

### Option A: `.status.appliedRenderSet` on the render target

A field on the render target's own `.status` (Deployment for
deployment renders, Template for project-scope template renders)
carrying the inline list of applied refs.

Pros:

- Single object. List/Get of the target gives the applied set
  alongside the rest of the target's state.
- No new CRD to maintain.

Cons:

- **HOL-554 violation**. A Deployment's status lives in the project
  namespace (the same namespace as the Deployment object). Project
  owners can patch `.status` on objects they own under namespace-scoped
  RBAC, so shipping the applied set on `.status` would let a project
  owner forge "no drift" by patching their own Deployment. The whole
  point of the original folder-namespace ConfigMap layout was to
  isolate drift evidence behind a namespace the project cannot write
  to. Putting it on `.status` of the project-namespace target gives
  that guardrail away.
- **Render targets in two namespaces**. Project-scope Template renders
  produce drift evidence keyed against the *project*, but Templates
  themselves can be created at any of three scopes (organization,
  folder, project). Putting drift state on the Template's `.status`
  forces a per-scope branch every time the resolver wants to read it,
  and the storage namespace flips with the scope.
- **Spec/Status misuse risk** (lesser concern). Drift evidence is
  controller-observed state — that part fits `.status`. But it is
  also load-bearing for security decisions, which the Gateway-API
  status convention does not contemplate.

### Option B: `RenderState` sibling CRD

A new CRD in the same `templates.holos.run/v1alpha1` group. The
object name is a deterministic function of `(targetKind, project,
targetName)`. Spec carries the snapshot (typed target reference plus
the applied ref list). Status follows the Gateway-API pattern
(observedGeneration + Conditions).

Pros:

- **HOL-554 isolation enforced at admission**. The object's namespace
  is independent of the render target's namespace. A
  `ValidatingAdmissionPolicy` (`renderstate-folder-or-org-only`,
  mirroring `templatepolicy-folder-or-org-only`) rejects any
  RenderState CREATE/UPDATE in a project-labeled namespace, so
  storage-isolation is a kube-apiserver guarantee, not a
  resolver-honor-system convention.
- **Per-target object**. Each `(targetKind, targetName)` pair gets
  its own object. A misbehaving renderer cannot trample another
  target's drift record by accident; admission validation can scope
  per-record without consulting every project's full target list.
- **Label-selector lookups remain efficient**. Listing by
  `(targetKind, targetName)` within a single namespace is a
  `client.List` with structured fields; the deterministic name lets
  callers skip the list entirely and Get directly.
- **Spec/Status separation**. The snapshot lives in `.spec` (it's
  controller-authored truth-of-record at the moment of write).
  Conditions and `observedGeneration` follow ADR 030.
- **Pattern parity with HOL-693**. The TemplateRelease migration
  already established the sibling-CRD pattern in this group (ADR
  032). Reusing it keeps the resolver's two storage paths
  (TemplatePolicy / TemplatePolicyBinding for the desired set;
  RenderState for the applied set) symmetric.

Cons:

- One more CRD in the group. Reconciler footprint grows slightly.
- Callers that want both render target state and drift evidence take
  two reads.

## Decision

Adopt **Option B**: `RenderState` as a sibling CRD in
`templates.holos.run/v1alpha1`.

The deterministic name helper is `renderStateObjectName(targetKind,
project, targetName)` and returns
`render-state-{kind-segment}-{project}-{target}` where
`kind-segment` is `deployment` or `project-template`. The HOL-554
storage-isolation invariant is enforced at admission by
`config/admission/renderstate-folder-or-org-only.yaml` (CEL rule
mirroring the TemplatePolicy admission policy).

The seven `api/v1alpha2` constants encoding the ConfigMap layout
retire as part of this change:

| Retired constant                 | Replacement on the CRD                          |
|----------------------------------|-------------------------------------------------|
| `ResourceTypeRenderState`        | implicit in the object's GVK                    |
| `LabelRenderTargetKind`          | `spec.targetKind` (typed `RenderTargetKind` enum) |
| `LabelRenderTargetProject`       | `spec.project` + `console.holos.run/render-target-project` label (kept for selector convenience) |
| `LabelRenderTargetName`          | `spec.targetName`                               |
| `AnnotationAppliedRenderSet`     | `spec.appliedRefs` (typed list)                 |
| `RenderTargetKindDeployment`     | `templatesv1alpha1.RenderTargetKindDeployment`  |
| `RenderTargetKindProjectTemplate`| `templatesv1alpha1.RenderTargetKindProjectTemplate` |

The `RenderTargetKind` values move from snake-case label values
(`deployment`, `project-template`) to PascalCase enum values
(`Deployment`, `ProjectTemplate`) on the CRD, matching Kubernetes API
conventions for enum fields. The deterministic object name retains
the lower-kebab segment for kubectl readability.

`AppliedRenderStateClient` drops its `kubernetes.Interface` dependency
and accepts a `controller-runtime` `client.Client`. The embedded
controller manager primes the RenderState informer eagerly at startup
(same pattern HOL-693 introduced for TemplateRelease) so cache-sync
readiness covers the kind and an absent CRD or RBAC gap surfaces at
`/readyz` rather than on first read.

### Conventions specific to `RenderState`

- **Namespace**: the folder namespace owning the project, or the
  organization namespace when the project's immediate parent is the
  organization. Never a project namespace (admission-enforced).
- **Name**: `renderStateObjectName(targetKind, project, targetName) =
  render-state-{kind-segment}-{project}-{target}`.
- **Lookup**: deterministic Get on `(namespace, name)`.
- **Idempotency**: `RecordAppliedRenderSet` does Create-then-Update
  on `AlreadyExists` so re-applying an unchanged set is a no-op and
  shrinking the set overwrites the prior record.
- **Status**: follows Gateway-API status pattern from ADR 030.

### Read consistency model

Reads route through the cache-backed `client.Client` returned by
`mgr.GetClient()`, matching the pattern adopted by Template (HOL-661),
TemplatePolicy / TemplatePolicyBinding (HOL-622), and TemplateRelease
(HOL-693 / ADR 032). Writes (`Create`, `Update`) fall through to the
API server directly; the informer's watch updates the cache on the
next event.

This means `ReadAppliedRenderSet` returns the cache view, which lags
the API server by one watch round-trip — typically sub-millisecond on
a healthy informer, observable only on a cold cache or under
controller-manager pressure. The window matters for a render-then-read
flow inside a single RPC chain (operator does Create, server runs
render and writes RenderState, operator's next Get arrives before the
informer observes the write). The accepted UX consequence is:
`GetDeploymentPolicyState` / `policy_drift` may report
`has_applied_state=false` for the duration of the watch lag on the
first read after a brand-new render-target's first render.

This consistency tradeoff is intentional and matches the rest of the
templates-group storage clients. It is preferred over an
`APIReader`-served read path because:

- The drift surface is informational UX, not a security or data-loss
  decision. The HOL-554 isolation guarantee that this storage layout
  enforces lives at admission, not at read time.
- Drift checks land on every list-view page render. Routing each
  through an uncached Get would add a synchronous API hop per row
  shown, regressing the very list-view performance the cache-backed
  resolver path (HOL-622) was built to deliver.
- Symmetry with the TemplatePolicy / TemplatePolicyBinding cache path
  used to compute the *desired* set keeps the comparison trivial and
  testable: a single watch round-trip lag on either side resolves the
  same way.

If a future caller needs strict read-your-writes for an externally
observable correctness property (not a drift UX nicety), the
recommended pattern is to construct an `APIReader`-backed reader at
the call site rather than to switch the entire client.

## Consequences

- Existing operators carrying render-state ConfigMaps will need a
  fresh aggregation. Per HOL-615 the pre-release migration policy is
  "no backwards compatibility"; the next render of each target
  rewrites the applied set into the new CRD shape.
- New CRD manifest ships in `config/crd/`; new VAP manifest ships in
  `config/admission/`. Both regenerate via `make manifests`.
- `console/policyresolver/applied_state_test.go` moves from
  fake-clientset ConfigMap fixtures to the controller-runtime fake
  client builder seeded with `RenderState` CRs. The fake client backs
  the same `client.Client` interface the production wiring exposes,
  so the resolver code path under test is the path that runs in
  production.

## Why colocate this ADR

Per the criteria in `docs/adrs/README.md`: the binary
(`holos-console`) and the CRD types (`api/templates/v1alpha1/`) live
in this repository, and the review boundary matches the CODEOWNERS
boundary for `api/templates/` and `console/policyresolver/`. The
sibling ADR for TemplateRelease (032) is colocated for the same
reason; keeping the two together preserves the Template /
TemplateRelease / RenderState lineage in a single index.

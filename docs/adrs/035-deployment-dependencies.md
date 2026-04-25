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

# ADR 035: Deployment Dependencies via TemplateGrant, TemplateDependency, TemplateRequirement (HOL-954)

- Status: Accepted
- Date: 2026-04-25
- Binary: `holos-console` (multiple packages)
- Follows: [ADR 034 — Namespace TemplatePolicyBinding for new Projects](034-namespace-template-policy-binding-for-new-projects.md)

## Context

Platform owners need to express that certain templates depend on other
templates being deployed alongside them (e.g., every `mcp-server` deployment
requires a shared `waypoint` sidecar). Project-scope declarations
(`TemplateDependency`) let Service Owners declare this within a single project.
Org- or folder-scope mandates (`TemplateRequirement`) let Platform Owners
declare this for all projects under an ancestor without any per-project
configuration.

Cross-namespace template references also require an authorization mechanism
(`TemplateGrant`) to prevent project owners from referencing templates they
do not own.

## Decisions

### Decision 1 — Three new CRDs

Three tightly scoped CRDs are introduced:

| CRD | Scope | Purpose |
|-----|-------|---------|
| `TemplateGrant` | org or folder namespace | Authorizes cross-namespace template references from listed project namespaces |
| `TemplateDependency` | project namespace | Declares that Deployments from template A require a singleton of template B |
| `TemplateRequirement` | org or folder namespace | Mandates that all Deployments matching `targetRefs[]` require a singleton of template B |

### Decision 2 — Singleton Deployment with refcount owner-refs

The first Deployment that triggers a dependency edge creates a singleton
Deployment in the same project namespace. Subsequent Deployments that match
the same edge add a non-controller `ownerReference` (Controller=false,
BlockOwnerDeletion=true). Native Kubernetes GC reaps the singleton when the
last owner is deleted.

### Decision 3 — Singleton naming

The singleton Deployment name is deterministic:

```
<requires.Name>-<sanitized-versionConstraint>-shared
```

When `VersionConstraint` is empty: `<requires.Name>-shared`.

This ensures idempotent applies and allows Phase 8 (PreflightCheck) to detect
name collisions before apply.

### Decision 4 — cascadeDelete controls owner-ref edge

`cascadeDelete: true` (default) adds the non-controller ownerReference.
`cascadeDelete: false` creates the singleton but skips the ownerReference,
decoupling the singleton's lifecycle from the dependent.

### Decision 5 — TemplateGrant: ReferenceGrant-style authorization

A `TemplateGrant` in namespace `N` grants listed project namespaces access to
templates in `N`. Same-namespace references are always allowed without a grant.
Supports literal namespace, `"*"` wildcard, and `namespaceSelector`.

### Decision 6 — Hard-revoke on grant deletion

When a TemplateGrant is deleted, the TemplateGrantController immediately
removes it from the cache. New cross-namespace materializations are rejected
from that point. Existing materialised singletons are NOT removed — callers
must manage orphan cleanup manually.

### Decision 7 — HOL-554 storage-isolation for TemplateRequirement

`TemplateRequirement` objects stored in project namespaces are ignored by the
ancestor walker (mirrors the existing storage-isolation rule for
`TemplatePolicyBinding`). The CEL ValidatingAdmissionPolicy enforces this at
admission time; the ancestor walker enforces it as belt-and-suspenders.

### Decision 8 — TemplateRequirement targeting

`TemplateRequirement.targetRefs[]` uses the same Kind/Name/ProjectName shape
as `TemplatePolicyBindingTargetRef`. The wildcard `"*"` semantics from ADR 029
apply: `projectName: "*"` matches all projects reachable via the ancestor walk;
`name: "*"` matches all Deployments of that kind within the matched projects.

### Decision 9 — Render order (Open Question 2, resolved)

`TemplatePolicy.Require` runs at render time (unchanged). `TemplateRequirement`
materialises sibling Deployments **after** the dependent's render succeeds.

The ordering is enforced by the controller: it watches Deployment objects and
only calls `EnsureSingletonDependencyDeployment` for Deployments that already
exist as CRs (i.e., whose render has produced a Deployment CR). This avoids
races where the sibling singleton references rendered output that does not yet
exist.

### Decision 10 — TemplateRequirement overlap policy (Open Question 1, resolved)

When two `TemplateRequirement` objects in the same ancestor chain match the
same Deployment:

- **Different `requires` templates**: each requirement is processed
  independently, producing distinct singleton Deployments (one per
  `(requires.namespace, requires.name, requires.versionConstraint)` tuple).
  The union of the required set emerges naturally from this decomposition.

- **Same `(namespace, name)` template with different `versionConstraint`**:
  the sanitized version suffix in the singleton name (`waypoint-v1-shared` vs
  `waypoint-v2-shared`) keeps the two singletons distinct. Phase 8
  (PreflightCheck) surfaces these as potential version conflicts before apply.

- **Same `(namespace, name, versionConstraint)`**: the `EnsureSingleton` call
  is idempotent — it finds the existing singleton and adds the ownerReference
  if missing. The second reconcile is a no-op.

Incompatible `versionConstraint`s on the same `(namespace, name)` pair fail
hard via the Phase 8 `PreflightCheck` RPC (HOL-962) before any apply. The
unit tests in `console/deployments/dependency_reconciler_test.go` cover the
union case; the conflict case is surfaced by PreflightCheck.

### Decision 11 — Grant validation in TemplateRequirementReconciler

Grant validation for `TemplateRequirement` is performed per-Deployment by
`EnsureSingletonDependencyDeployment`, not at the TemplateRequirement level.
This is because the impacted project namespaces are discovered dynamically
(via the wildcard match) and the meaningful authorization boundary is "can
*this Deployment's namespace* use the required template", not "can the org
namespace use the required template".

When a grant is missing for a specific project namespace, the ResolvedRefs
condition reflects GrantNotFound for that reconcile and the reconciler
requeues. When a TemplateGrant is subsequently created, the
TemplateGrantController updates the cache and the reconciler will succeed on
the next reconcile triggered by the Deployment watch.

## Consequences

- Three new CRDs with kubebuilder status subresources and conditions following
  the Gateway-API ADR 030 pattern (Accepted, ResolvedRefs, Ready).
- `EnsureSingletonDependencyDeployment` in `console/deployments` is the shared
  helper for both TemplateDependency and TemplateRequirement reconcilers.
- Deployment CRD promotion (D1, HOL-957) is prerequisite: owner-refs require
  both ends of the edge to be CRs at reconcile time.
- Phase 8 (PreflightCheck, HOL-962) surfaces naming collisions and version
  conflicts before apply.
- Phase 9 (UI, HOL-963) surfaces shared dependency Deployments with a "shared
  dependency" indicator and per-edge cascade-delete toggle.

# Changelog

All notable changes to holos-console are documented here.

## [Unreleased]

### Added — TemplateDependency, TemplateRequirement, TemplateGrant new/edit/detail pages and ScopePicker (HOL-1017)

Full CRUD UI for the three template-dependency resource families introduced in HOL-954.
All creation forms use the new `ScopePicker` component so users can choose between
organization and project scope without leaving the page.

#### ScopePicker component (HOL-1018)

`frontend/src/components/scope-picker/ScopePicker.tsx` — a controlled dropdown that
lets "new resource" forms toggle between Organization and Project scope. Disables the
Project option (with tooltip) when no project is selected. Used on all five creation
forms below.

#### Query hooks (HOL-1019)

Added `useGetTemplateDependency`, `useCreateTemplateDependency`,
`useUpdateTemplateDependency`, `useGetTemplateRequirement`,
`useCreateTemplateRequirement`, `useUpdateTemplateRequirement`,
`useGetTemplateGrant`, `useCreateTemplateGrant`, and `useUpdateTemplateGrant`
hooks to the existing query modules in `frontend/src/queries/`.

#### TemplateDependency pages (HOL-1020)

- `/organizations/$orgName/template-dependencies/` — ResourceGrid index
- `/organizations/$orgName/template-dependencies/new` — create form with ScopePicker
- `/organizations/$orgName/template-dependencies/$dependencyName` — detail / edit / delete
- `frontend/src/components/template-dependencies/DependencyForm.tsx` — shared form component

#### TemplateRequirement pages (HOL-1021)

- `/organizations/$orgName/template-requirements/` — ResourceGrid index
- `/organizations/$orgName/template-requirements/new` — create form with ScopePicker
- `/organizations/$orgName/template-requirements/$requirementName` — detail / edit / delete
- `frontend/src/components/template-requirements/RequirementForm.tsx` — shared form component

#### TemplateGrant pages (HOL-1022)

- `/organizations/$orgName/template-grants/` — ResourceGrid index
- `/organizations/$orgName/template-grants/new` — create form with ScopePicker
- `/organizations/$orgName/template-grants/$grantName` — detail / edit / delete
- `frontend/src/components/template-grants/GrantForm.tsx` — shared form component

#### New header actions on project-scoped grid pages (HOL-1023)

Added canCreate-gated "New" header actions to the three project-scoped template
index pages (`/projects/$projectName/templates/dependencies/`,
`/projects/$projectName/templates/requirements/`,
`/projects/$projectName/templates/grants/`). The New button navigates to the
corresponding org-scoped creation route above.

#### ScopePicker adopted on existing new-resource pages (HOL-1024)

`TemplatePolicy`, `TemplatePolicyBinding`, and Template creation pages now render
`ScopePicker` so the namespace is always visible and selectable at creation time.

### Chore — Purge cascade/hierarchical-apply TemplatePolicy terminology (HOL-992 / HOL-993)

TemplatePolicy enforcement is now binding-only: a policy in an ancestor namespace
has no effect unless a `TemplatePolicyBinding` explicitly selects the render target.
The cascade/hierarchical-apply model and all associated terminology have been removed.

- Renamed wildcard cascade tests to clarify binding-based scope semantics (HOL-995).
- Purged cascade and hierarchical-policy prose from proto comments (HOL-996).
- Rewrote `TemplatesHelpPane` to explain binding-only enforcement (HOL-997).
- Updated ADR 034 and ADR 035 to reflect binding-only enforcement (HOL-998).
- Added `TestFolderResolver_PolicyWithoutBindingDoesNotApply` regression test as a
  permanent guardrail: asserts that a `TemplatePolicy` in an org namespace with no
  matching `TemplatePolicyBinding` contributes zero rules to `Resolve()` for any
  descendant project (HOL-999).

### Added — Deployment Dependencies: TemplateGrant, TemplateDependency, TemplateRequirement, Deployment CRD (HOL-954)

Implements [ADR 035](docs/adrs/035-deployment-dependencies.md): three new tightly-scoped
CRDs plus a Deployment CRD promotion that together enable platform owners and
service owners to express mandatory co-deployment relationships between templates.

#### New CRDs

| CRD | Scope | Purpose |
|-----|-------|---------|
| `TemplateGrant` (`templates.holos.run/v1alpha1`) | org or folder namespace | Authorizes cross-namespace template references from listed project namespaces (ReferenceGrant-style). Hard-revoke on deletion; existing singletons are preserved, new materializations blocked. |
| `TemplateDependency` (`templates.holos.run/v1alpha1`) | project namespace | Declares that all Deployments of template A in this project require a singleton of template B. Same-namespace references need no grant; cross-namespace requires a matching `TemplateGrant`. |
| `TemplateRequirement` (`templates.holos.run/v1alpha1`) | org or folder namespace | Mandates that all Deployments matching `targetRefs[]` across every project under the ancestor require a singleton of template B — no per-project action needed. Mirrors the storage-isolation rule from `TemplatePolicyBinding`. |

#### Deployment CRD (D1 promotion — HOL-957)

`Deployment` is now a Custom Resource (`deployments.holos.run/v1alpha1`) backed
by kubebuilder status subresources. The server dual-writes via Server-Side Apply
so the proto store and the CR are kept in sync. Owner-references between
`Deployment` CRs are the mechanism for GC: a non-controller ownerReference
(`controller=false`, `blockOwnerDeletion=true`) ties each dependent Deployment
to the shared singleton it triggered.

#### Singleton lifecycle

The first Deployment that triggers a dependency edge creates a singleton
Deployment in the same project namespace with the deterministic name
`<requires.Name>-<sanitized-versionConstraint>-shared` (e.g. `waypoint-v1-shared`).
Subsequent Deployments add a second non-controller ownerReference. Native
Kubernetes GC reaps the singleton when the last owner is deleted.
`cascadeDelete: false` creates the singleton but skips the owner-reference edge,
decoupling the singleton's lifecycle from the dependent.

#### PreflightCheck RPC (HOL-962)

`DeploymentService.PreflightCheck` in `proto/holos/console/v1/deployments.proto`
surfaces sibling-Deployment name collisions and `versionConstraint` conflicts
before any apply. The `versionConstraint` conflict case (same
`(namespace, name)` template, different version strings) fails hard via
PreflightCheck rather than silently creating two singletons with overlapping
purposes.

#### UI (HOL-963)

- Deployments index page: shared singleton Deployments display a "shared
  dependency" badge. Badge tooltip now lists each originating
  `TemplateDependency` / `TemplateRequirement` (kind, namespace, name) sourced
  from the new `Deployment.dependencies` field exposed over gRPC. Detection
  switched from the `-shared` suffix heuristic to the resolved edges; the
  suffix remains as a fallback during the brief gap between singleton creation
  and the first RenderState write.
- Per-dependency cascade-delete toggle on the Create/Edit Deployment form;
  defaults to on. (Per-edge persistence to the originating CRD is tracked in
  HOL-991 and is not yet wired.)
- PreflightCheck conflict banner inline on the deployment form before apply.

#### `RenderState.spec.dependencies[]` (HOL-961)

`RenderState` snapshots the resolved `(template, version)` dependency edges
produced by TemplateDependency and TemplateRequirement reconcilers. The existing
drift checker covers the edges.

#### ValidatingAdmissionPolicy (HOL-956)

Three new CEL-backed `ValidatingAdmissionPolicy` objects enforce namespace
contracts:
- `TemplateGrant` must be in an org or folder namespace (not a project namespace).
- `TemplateDependency` must be in a project namespace.
- `TemplateRequirement` must be in an org or folder namespace.

#### Example templates (HOL-983 / PR #1193)

Four new built-in template examples added to the registry picker to illustrate
all three dependency scopes:

| Example | Scope | Description |
|---------|-------|-------------|
| `valkey-v1` | A (instance) | Valkey cache — same-namespace TemplateDependency |
| `shared-configmap-v1` | B (project) | Shared ConfigMap mandated by TemplateRequirement |
| `httproute-with-grant-v1` | C (remote-project) | Cross-namespace HTTPRoute with TemplateGrant |
| `all-scopes-v1` | A + B + C | Composite example exercising all three scopes |

#### ADR 035 open questions resolved

Three questions deferred to the implementation plan are now closed:

1. **Overlap policy** (OQ 1, resolved in PR #1189): union the `requires` set;
   incompatible `versionConstraint`s on the same `(namespace, name)` pair are
   rejected by PreflightCheck.
2. **Render order** (OQ 2, resolved in PR #1189): `TemplatePolicy.Require` runs
   at render time (unchanged); `TemplateRequirement` materialises singletons
   after the dependent's render succeeds.
3. **PreflightCheck RPC shape** (OQ 3, resolved in PR #1194): pinned in
   `proto/holos/console/v1/deployments.proto`.

#### PRs

| Phase | Issue | PR |
|-------|-------|-----|
| 1 — CRD types | HOL-955 | [#1183](https://github.com/holos-run/holos-console/pull/1183) |
| 2 — Admission policies | HOL-956 | [#1185](https://github.com/holos-run/holos-console/pull/1185) |
| 3 — Deployment CRD (D1) | HOL-957 | [#1186](https://github.com/holos-run/holos-console/pull/1186) |
| 4 — TemplateGrant validator | HOL-958 | [#1187](https://github.com/holos-run/holos-console/pull/1187) |
| 5 — TemplateDependency reconciler | HOL-959 | [#1188](https://github.com/holos-run/holos-console/pull/1188) |
| 6 — TemplateRequirement reconciler | HOL-960 | [#1189](https://github.com/holos-run/holos-console/pull/1189) |
| 7 — RenderState dependencies | HOL-961 | [#1191](https://github.com/holos-run/holos-console/pull/1191) |
| 8 — PreflightCheck RPC | HOL-962 | [#1194](https://github.com/holos-run/holos-console/pull/1194) |
| 9 — UI dependency indicator | HOL-963 | [#1197](https://github.com/holos-run/holos-console/pull/1197) |
| 10 — Example templates | HOL-983 | [#1193](https://github.com/holos-run/holos-console/pull/1193) |

### Removed — `/organizations/$orgName/resources` route and ResourceGrid consumer (HOL-938)

- Deleted the org-scoped Resources page at
  `/organizations/$orgName/resources` along with its only-consumer query
  (`useListResources`) and the `ResourceTypeIcon` component (also unused after
  the page was removed). Clicking an organization on the `/organizations`
  index now lands on the org-scoped Projects listing
  (`/organizations/$orgName/projects`).
- All breadcrumbs that previously linked back to the org Resources page
  (folder index, folder settings, folder templates, folder template policies,
  folder template-policy bindings, folder projects index, folder
  template-policies/new, folder template-policy-bindings/new, folder
  templates/new, folder template-policies/$policyName, folder
  template-policy-bindings/$bindingName) now link to
  `/organizations/$orgName/projects` and label the link "Projects".
- Removed the `selectOrg` Playwright helper's wait on the `/resources` URL —
  the helper now waits for `/projects` instead.
- Removed e2e assertions that exercised the deleted Resources listing
  (`folders.spec.ts`, `folder-rbac.spec.ts`); the underlying behavior is now
  exercised by direct navigation to the folder detail pages.
- Updated the AGENTS.md "Deferred" entry to drop the reference to the removed
  route.

### Changed — Migrate `/orgs/$orgName` routes to `/organizations/$orgName` (HOL-939)

- All organization-scoped UI routes now use the canonical `/organizations/...`
  prefix. The legacy `/orgs/...` tree (`resources`, `settings`, `templates`,
  `template-bindings`, `template-policies`) was renamed in place; layout files
  for both prefixes were identical, so the moved subdirectories now sit under
  the existing `_authenticated/organizations/$orgName/` layout.
- Updated every link, redirect, and helper in `frontend/` (including the
  scope-aware `template-row-link` resolver) plus all unit tests, e2e test
  navigation, and PR-capture scripts to emit `/organizations/...` paths.
- AGENTS.md and `docs/ui/resource-routing.md` now state the path-name
  convention: use the full plural words `organizations` and `projects`; do
  not use `/orgs/...` for new routes, links, or tests.

### Chore — Remove stale lineage, scope, and Resource Manager references (HOL-910 / HOL-919)

The HOL-910 series (HOL-912 through HOL-919) removed the legacy Resource
Manager tree view, All-Scopes selector, lineage filter, and related
infrastructure. This final cleanup pass (HOL-919) removed the remaining
doc/comment references:

- **`docs/ui/resource-grid-v1.md`**: Removed the stale lineage-filter entry
  from the "What ResourceGrid v1 does" list; removed `lineage` and `recursive`
  fields from the `ResourceGridSearch` interface example; removed the stale
  "Resource Manager tree" comparison table row; removed the reference to the
  deleted `resource-manager/` component directory.
- **`docs/ui/resource-routing.md`**: Updated the worked example to use
  `/projects` as the default `returnTo` fallback instead of the deleted
  `/resource-manager` route.
- **`docs/testing.md`**: Removed two catalog entries for test files that were
  deleted with the Resource Manager component
  (`resource-manager/-resource-tree.test.tsx` and
  `resource-manager/-index.test.tsx`); updated the ResourceGrid v1 test-catalog
  entry to remove the mention of the removed lineage filter controls.
- **`frontend/src/routes/.../templates/index.tsx`** and its test: Updated
  stale JSDoc comment that still referenced `lineage=descendants` and the
  removed lineage select control.

### Fixed — Remove All Scopes selector and fix BindingForm policy picker (HOL-917)

- **`BindingForm`**: Removed the unused All-Scopes combobox entry; the policy
  picker now lists policies from the current scope only, matching the
  "no policies reachable from this scope" → "no policies exist in this org
  yet" copy change (HOL-917).

### Added — Template Bindings listing page (`/organizations/$orgName/template-bindings`) (HOL-918)

- New route at `/organizations/$orgName/template-bindings` listing all
  `TemplatePolicyBinding` objects in the org. Powered by `ResourceGrid v1`
  with sortable Created At column (HOL-918).

### Fixed — holos-secret-injector HOL-752 review follow-ups (HOL-839)

- **`holos-secret-injector` CLI now exposes `--mesh-trust-domain`**
  (`HOLOS_SECRETINJECTOR_MESH_TRUST_DOMAIN`) plumbed into
  `controller.Options.MeshTrustDomain`. Operators running a re-pegged
  mesh (anything other than `cluster.local`) can now override the trust
  domain stamped into emitted `AuthorizationPolicy.source.principals`
  without rebuilding the controller. Default remains `cluster.local` so
  upstream Istio installations are unaffected.
- **`ruleEqual` drift detection tightened** in the
  `SecretInjectionPolicyBindingReconciler`. The previous
  `len(a.When) == len(b.When)` compare masked in-place mutations of
  fixed-length `When` slices; the helper now treats any non-empty `When`
  on either side as drift, matching the inline contract that the
  reconciler never populates `Rule.When` today. Guarded by a new table
  test in `binding_controller_test.go` so a future M3 decision to emit
  `When` predicates will force an explicit element-wise compare.

### Added — Ancestor-aware TemplatePolicyBinding policy picker (HOL-833)

`BindingForm` now calls `ListLinkableTemplatePolicies` (scope + ancestor walk)
instead of the single-scope `ListTemplatePolicies` RPC, so a folder-scoped
binding can select policies stored at any ancestor folder or org scope.
`AncestorChainResolver` validation on `CreateTemplatePolicyBinding` /
`UpdateTemplatePolicyBinding` ensures the referenced policy is reachable from
the binding's storage scope at authoring time (HOL-836).

**References**: PRs #1119 (backend `ListLinkableTemplatePolicies`), #1120
(frontend hook + BindingForm wiring), #1121 (ancestor-chain authoring
validation).

### Added — ProjectNamespace TemplatePolicyBinding for new Projects (HOL-806)

Operators can now attach a `TemplatePolicyBinding` with
`targetRefs.kind: ProjectNamespace` to an org- or folder-scoped ancestor
namespace. When `CreateProject` is called, the console:

1. Resolves all `ProjectNamespace` bindings that match the new project's
   name (wildcards supported via `projectName: "*"`).
2. Renders each referenced `Template` with platform inputs and collects
   `platformResources` (cluster-scoped resources, the namespace itself,
   and namespace-scoped resources).
3. Merges any template-produced `Namespace` object with the
   RPC-constructed `Namespace`. Conflicting field values are a hard error.
4. Applies cluster-scoped resources, then the unified `Namespace`, then
   namespace-scoped resources — in that order — using Server-Side Apply.
5. Waits for `Namespace.status.phase == Active` before applying
   namespace-scoped resources, then retries with exponential back-off on
   transient API server errors (mirrors the ADR 034 §4 retry strategy).

If no bindings match, `CreateProject` falls through to the existing typed
namespace-create path unchanged.

**New `TemplatePolicyBindingTargetKind` value**: `ProjectNamespace` joins
the existing `ProjectTemplate` and `Deployment` values. No migration of
existing bindings is required.

**Frontend**: the `BindingForm` and `TargetRefEditor` components now
surface `ProjectNamespace` as a selectable kind. Selecting it renders a
project-name input with wildcard (`*`) support.

**Two built-in example templates** are available in the UI picker:

- `project-namespace-description-annotation-v1` — adds a `description`
  annotation to the new namespace. Minimal starting point.
- `project-namespace-reference-grant-v1` — emits a Gateway API
  `ReferenceGrant` in the project namespace so HTTPRoutes in the org
  gateway namespace can reference Services in the project namespace.

**References**: ADR 034
(`docs/adrs/034-namespace-template-policy-binding-for-new-projects.md`),
PRs #1091 (ADR), #1093 (API types), #1096 (resolver), #1098 (render),
#1100 (applier), #1107 (RPC wire-up), #1109 (examples), #1112 (frontend).

### Fixed — `crypto.Params` JSON shape pins required uint fields (HOL-838)

Removed `,omitempty` from the `Time`, `Memory`, `Parallelism`, `KeyLength`,
and `Iterations` JSON tags on `internal/secretinjector/crypto.Params`. A
zero value — which `validateArgon2idParams` rejects on both Hash and
Verify — is now serialized as an explicit `0` instead of being silently
dropped. The envelope JSON shape now faithfully reflects the required
fields, so a future KDF that forgets a zero-rejection check cannot
silently round-trip a degenerate envelope. No on-wire break: envelopes
produced by earlier code either contain the fields already (non-zero
values pass `omitempty`) or fail `Verify` at the validator as before.

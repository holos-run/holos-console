# AGENTS.md

Agent context map for the `holos-console` code repo. For architecture,
UI conventions, and the full catalog of cross-cutting guardrails, see the
companion
[AGENTS.md in holos-console-docs](https://github.com/holos-run/holos-console-docs/blob/main/AGENTS.md).

This code is not yet released. Do not preserve backwards compatibility when
making changes. Review `CONTRIBUTING.md` for commit message requirements before
opening a PR.

## Testing

All testing guidance lives in this repo. Read the entries below in order the first time; after that, jump straight to the doc you need.

1. [Test Strategy](docs/agents/test-strategy.md) — Decision rule: prefer unit tests; reserve E2E for the OIDC login flow (requires a real Dex server) and full-stack CRUD round-trips (require a real Kubernetes cluster).
2. [Testing Patterns](docs/agents/testing-patterns.md) — Frameworks and conventions per layer: Go table-driven tests with `k8s.io/client-go/kubernetes/fake`, Vitest + React Testing Library + jsdom for UI with `vi.mock('@/queries/*')` for ConnectRPC hooks, Playwright for E2E, plus the `loginAsPersona()` multi-persona helper.
3. [Testing Guide](docs/testing.md) — Full decision-rule table (what belongs in a unit vs. E2E test), the ConnectRPC mock worked example, route-directory test-file naming rules (`-<name>.test.tsx`), and the catalog of existing unit-test files.
4. [E2E Testing](docs/e2e-testing.md) — Tight iteration loop against running servers, port overrides, multi-persona helpers, and the per-spec table of which E2E tests require Kubernetes.
5. [E2E Refactor Audit](docs/agents/e2e-refactor-audit.md) — Historical per-spec verdict (Keep / Refactor-to-unit / Split / Delete) consumed by HOL-653 through HOL-658, and the post-refactor CI timing results (Results section: `E2E Tests` job dropped from ~11m 23s to ~7m 43s).

**Make targets**: `make test-go` (Go tests), `make test-ui` (Vitest unit tests), `make test-e2e` (Playwright, needs `make certs` and a k3d cluster), `make test` (all three). Run `make generate` before committing if proto or generated code is affected.

## Access Control

Authorization is enforced by Kubernetes RBAC with OIDC impersonation per [ADR 036](docs/adrs/036-rbac-and-oidc-impersonation.md). Every ConnectRPC handler that calls Kubernetes routes the request through an impersonated clientset so the API server — not in-process Go code — is the single arbiter of access. The legacy `console/rbac` package (in-process role checks) is being incrementally removed; see [ADR 036](docs/adrs/036-rbac-and-oidc-impersonation.md) and the migration runbook in `cmd/holos-console-migrate-rbac/` for context. New handlers must use `rpc.ImpersonatedClientsetFromContext(ctx)` and must not call `rbac.CheckAccessGrants` or `rbac.CheckCascadeAccess`.

## Frontend stack and constraints

The frontend under `frontend/` is a Vite single-page React app. Treat these as
the locked stack dependencies for new UI work:

| Layer | Dependency |
|---|---|
| Build/dev server | `vite@7.3.1`, `@vitejs/plugin-react@5.1.4` |
| UI runtime | `react@19.2.4`, `react-dom@19.2.4` |
| Routing | `@tanstack/react-router@1.163.3`, `@tanstack/router-plugin@1.163.3` |
| Server state | `@tanstack/react-query@5.90.21` |
| Tables | `@tanstack/react-table@8.21.3` |
| RPC transport | `@connectrpc/connect@2.1.1`, `@connectrpc/connect-web@2.1.1`, `@connectrpc/connect-query@2.2.0` |
| Styling | `tailwindcss@4.2.1`, `@tailwindcss/vite@4.2.1` |
| UI primitives | `shadcn@3.8.5`, `radix-ui@1.4.3`, `lucide-react@0.575.0` |

Do not introduce Next.js or a parallel framework. New frontend code must fit
the existing Vite + React + TanStack Router model unless an accepted architecture
issue explicitly changes this stack.

Agent-facing frontend docs:

- [Frontend Architecture](docs/agents/frontend-architecture.md) — routing,
  `returnTo`, selected-entity sync, ConnectRPC transport, and build/test
  commands.
- [TanStack Query Conventions](docs/agents/tanstack-query-conventions.md) —
  query-key factories, transport/hook split, stale time defaults, enabled
  guards, mutation invalidation matrix, and prefetch policy.
- [Data Grid Architecture](docs/agents/data-grid-architecture.md) —
  source of record for `ResourceGrid` architecture conventions.
- [Data Grid Conventions](docs/agents/data-grid-conventions.md) — quick
  pointer for the clickable resource ID and fully-clickable row rule.
- [Standard Page Layout](docs/agents/standard-page-layout.md) — when to use
  `StandardPageLayout`, its slots, prop contract, and canonical examples.
- [Frontend Audit Baseline](docs/agents/frontend-audit-2026-04.md) — Phase 1
  inventory and target conventions.

### StandardPageLayout

Use `StandardPageLayout` from `@/components/page-layout` for top-level
resource list pages that render a `ResourceGrid` and need the standard title,
breadcrumb, header action, URL-search, and contextual-content slots. It exposes
`title` or `titleParts`, `breadcrumbs`, `headerActions`, `children`, and a
typed `grid` prop bag. Canonical examples are the project-scoped
[Secrets](frontend/src/routes/_authenticated/projects/$projectName/secrets/index.tsx),
[Deployments](frontend/src/routes/_authenticated/projects/$projectName/deployments/index.tsx),
and [Templates](frontend/src/routes/_authenticated/projects/$projectName/templates/index.tsx)
routes; see [Standard Page Layout](docs/agents/standard-page-layout.md) for
the prop table and minimal usage example.

### Security: secrets in the UI

Never display raw secret values by default. Secret list pages and grid columns
must be metadata-only: name, scope, type, sharing/usage summaries, timestamps,
and other non-sensitive descriptors. Any reveal of secret material requires an
explicit user action in a detail/editor flow, and the default view after
navigation or refresh must return to a non-revealed state.

Do not place secret material in CR specs or status. Template CRs carry metadata
and `v1.Secret` refs only, and `secrets.holos.run/v1alpha1` CRs are control
objects, not vaults. The Holos invariant forbids plaintext credentials, hash
bytes, salt bytes, pepper bytes, API key prefixes, last-4 digits, and any other
entropy-revealing truncation in CR fields; see
[No-sensitive-values invariant](#no-sensitive-values-invariant-must-read-before-editing-any-cr-field).

## MVP UI — ResourceGrid v1 and sidebar nav (HOL-854 + HOL-911)

The HOL-854 plan shipped seven phases (HOL-855 – HOL-861) that replace the old
two-tree sidebar with a flat nav and a shared table component. HOL-911 (phase
HOL-914) subsequently removed the Resource Manager and added a Projects nav
entry and WorkspaceMenu improvements. Key files:

| File / dir | Phase | Purpose |
|---|---|---|
| `frontend/src/components/resource-grid/` | HOL-855 | `ResourceGrid` v1 table, `types.ts`, `url-state.ts` |
| `frontend/src/components/ui/confirm-delete-dialog.tsx` | HOL-855 | Shared delete confirmation dialog |
| `frontend/src/components/app-sidebar.tsx` | HOL-856 / HOL-914 | Flat nav (Projects, Secrets, Deployments, Templates) |
| `frontend/src/routes/_authenticated/projects/$projectName/secrets/index.tsx` | HOL-857 | Secrets page on ResourceGrid v1 |
| `frontend/src/routes/_authenticated/projects/$projectName/deployments/index.tsx` | HOL-858 | Deployments page on ResourceGrid v1 |
| `frontend/src/routes/_authenticated/projects/$projectName/templates/index.tsx` | HOL-859 | Unified Templates index on ResourceGrid v1 |
| `frontend/src/components/templates/TemplatesHelpPane.tsx` | HOL-860 | Templates help pane (? icon toggle) |
| `frontend/src/routes/_authenticated/organizations/$orgName.tsx` | HOL-928 | Layout — syncs `$orgName` URL param → `useOrg()` store (one-way) |
| `docs/ui/selected-entity-state.md` | HOL-931 | Selected-entity state contract; read-precedence rules; creation-page invariants |
| `docs/agents/data-grid-conventions.md` | HOL-940 | Data grid conventions: clickable resource IDs and fully-clickable rows |
| `docs/agents/frontend-audit-2026-04.md` | HOL-943 | Phase 2 audit baseline — current frontend architecture, gaps, and target conventions |
| `frontend/src/queries/templateDependencies.ts` | HOL-986 | `useListTemplateDependents` / `useListDeploymentDependents` reverse-dep query hooks |
| `frontend/src/components/templates/ReverseDependents.tsx` | HOL-987 | "Who depends on me" section with ADR-032 scope badges (instance / project / remote-project) |
| `console/templatepolicies/` | HOL-1009 | ConnectRPC handler + K8s adapter for TemplatePolicy CRUD |
| `console/templatepolicybindings/` | HOL-1009 | ConnectRPC handler + K8s adapter for TemplatePolicyBinding CRUD |
| `proto/holos/console/v1/template_policies.proto` | HOL-1009 | TemplatePolicy service proto (List, Get, Create, Update, Delete) |
| `proto/holos/console/v1/template_policy_bindings.proto` | HOL-1009 | TemplatePolicyBinding service proto |
| `frontend/src/routes/_authenticated/projects/$projectName/templates/policies/index.tsx` | HOL-1009 | Template Policies ResourceGrid page |
| `frontend/src/routes/_authenticated/projects/$projectName/templates/policy-bindings/index.tsx` | HOL-1009 | Template Policy Bindings ResourceGrid page |
| `console/templatedependencies/` | HOL-1010 | ConnectRPC handler for TemplateDependency CRUD |
| `proto/holos/console/v1/template_dependencies.proto` | HOL-1010 | TemplateDependency service proto |
| `console/templaterequirements/` | HOL-1011 | ConnectRPC handler for TemplateRequirement CRUD |
| `proto/holos/console/v1/template_requirements.proto` | HOL-1011 | TemplateRequirement service proto |
| `console/templategrants/` | HOL-1012 | ConnectRPC handler for TemplateGrant CRUD |
| `proto/holos/console/v1/template_grants.proto` | HOL-1012 | TemplateGrant service proto |
| `frontend/src/queries/templatePolicies.ts` | HOL-1013 | TanStack Query hooks for TemplatePolicy (list/get/create/update/delete) |
| `frontend/src/queries/templatePolicyBindings.ts` | HOL-1013 | TanStack Query hooks for TemplatePolicyBinding |
| `frontend/src/queries/templateDependencies.ts` (CRUD hooks) | HOL-1013 | TanStack Query CRUD hooks for TemplateDependency |
| `frontend/src/queries/templateRequirements.ts` | HOL-1013 | TanStack Query hooks for TemplateRequirement |
| `frontend/src/queries/templateGrants.ts` | HOL-1013 | TanStack Query hooks for TemplateGrant |
| `frontend/src/routes/_authenticated/projects/$projectName/templates/dependencies/index.tsx` | HOL-1013, HOL-1023 | Template Dependencies ResourceGrid page; New action navigates to org-scoped create route |
| `frontend/src/routes/_authenticated/projects/$projectName/templates/requirements/index.tsx` | HOL-1013, HOL-1023 | Template Requirements ResourceGrid page; New action navigates to org-scoped create route |
| `frontend/src/routes/_authenticated/projects/$projectName/templates/grants/index.tsx` | HOL-1013, HOL-1023 | Template Grants ResourceGrid page; New action navigates to org-scoped create route |
| `frontend/src/components/app-sidebar.tsx` (nested Templates nav) | HOL-1014 | Collapsible Templates sidebar with Policy / Dependencies / Grants sub-groups |
| `frontend/src/components/scope-picker/ScopePicker.tsx` | HOL-1018 | Controlled dropdown for Organization / Project scope selection on "new resource" forms |
| `frontend/src/routes/_authenticated/organizations/$orgName/template-dependencies/index.tsx` | HOL-1020 | Org-scoped TemplateDependency ResourceGrid page |
| `frontend/src/routes/_authenticated/organizations/$orgName/template-dependencies/new.tsx` | HOL-1020 | Create TemplateDependency form with ScopePicker |
| `frontend/src/routes/_authenticated/organizations/$orgName/template-dependencies/$dependencyName.tsx` | HOL-1020 | TemplateDependency detail / edit / delete page |
| `frontend/src/components/template-dependencies/DependencyForm.tsx` | HOL-1020 | Reusable create/edit form for TemplateDependency |
| `frontend/src/routes/_authenticated/organizations/$orgName/template-requirements/index.tsx` | HOL-1021 | Org-scoped TemplateRequirement ResourceGrid page |
| `frontend/src/routes/_authenticated/organizations/$orgName/template-requirements/new.tsx` | HOL-1021 | Create TemplateRequirement form with ScopePicker |
| `frontend/src/routes/_authenticated/organizations/$orgName/template-requirements/$requirementName.tsx` | HOL-1021 | TemplateRequirement detail / edit / delete page |
| `frontend/src/components/template-requirements/RequirementForm.tsx` | HOL-1021 | Reusable create/edit form for TemplateRequirement |
| `frontend/src/routes/_authenticated/organizations/$orgName/template-grants/index.tsx` | HOL-1022 | Org-scoped TemplateGrant ResourceGrid page |
| `frontend/src/routes/_authenticated/organizations/$orgName/template-grants/new.tsx` | HOL-1022 | Create TemplateGrant form with ScopePicker |
| `frontend/src/routes/_authenticated/organizations/$orgName/template-grants/$grantName.tsx` | HOL-1022 | TemplateGrant detail / edit / delete page |
| `frontend/src/components/template-grants/GrantForm.tsx` | HOL-1022 | Reusable create/edit form for TemplateGrant |

**Deleted (HOL-914)**: `frontend/src/components/resource-manager/` and
`frontend/src/routes/_authenticated/resource-manager/` were removed when the
Resource Manager tree view was retired in favour of the Projects listing page.

**Design note**: `docs/ui/resource-grid-v1.md` — columns, filter contract, URL
state format, extension points (`extraColumns`, `onDelete`).

**URL convention**: `docs/ui/resource-routing.md` — singular prefix for
creation pages (`/organization/new`, `/folder/new`, `/project/new`); plural +
identifier for scoped operations (`/organizations/$name/settings`). Includes
the `returnTo` search-param contract.

**Deferred**: Legacy sidebar destinations (Project tree, Organization tree,
folder-scoped index pages) are still present in the codebase but no longer
reachable via the sidebar. Their removal is tracked in a sibling cleanup
plan.

**Path-name convention (HOL-939)**: Organization-scoped routes use the literal
`organizations` segment, project-scoped routes use `projects`. The legacy
`/orgs/...` prefix has been removed — do not reintroduce it for new routes,
links, helpers, or tests.

## Guardrails

- [Resource URL Convention](docs/ui/resource-routing.md) — Use singular prefix for creation pages (`/organization/new`, `/folder/new`, `/project/new`) and plural + identifier for scoped operations (`/organizations/$name/settings`). Do NOT place creation pages under plural prefixes (e.g. `/folders/new`) — this creates a namespace collision where `new` is both a keyword and a valid resource name. Use the full plural words `organizations` and `projects` in URL paths; do NOT use `/orgs/...` (HOL-939).
- [Selected-Entity State Contract](docs/ui/selected-entity-state.md) — `useOrg()` / `useProject()` are the canonical stores; URL params are authoritative when present; layouts sync URL → store (never the reverse); creation pages read the store but must never write it.
- [Demo Docs Routing](https://github.com/holos-run/holos-console-docs/tree/main/demo) — Demo setup materials and CUE example snippets belong in `holos-run/holos-console-docs/demo/`, **not** in this repo; demo-related issues must include concrete examples and operator guidance.
- [Smoke Test Contract](https://github.com/holos-run/holos-console-docs/tree/main/demo/smoke-tests) — Smoke-test instructions must use `kubectl` commands for the resources required to observe the feature in the demo environment.
- [Demo README](https://github.com/holos-run/holos-console-docs/blob/main/demo/README.md) — Forward pointer to the demo setup order, prerequisites, and per-template walkthroughs.
- [Data Grid Conventions](docs/agents/data-grid-conventions.md) — Every `ResourceGrid` row must have `detailHref` set so the resource ID cell and the full row are clickable links to the resource detail page. Action buttons in the row must call `e.stopPropagation()` to prevent triggering row navigation.

## Example Template Registry

The UI picker on every "New Template" page is backed by a Go registry of built-in CUE drop-in examples.

### Adding a new example (drop-in workflow)

1. Create `console/templates/examples/<name>-<version>.cue` with four top-level fields:

   ```cue
   displayName: "Human Readable Name (version)"
   name:        "url-safe-slug-v1"
   description: "One sentence describing what the template produces."

   cueTemplate: """
     // CUE template body visible in the editor.
     // Reference #PlatformInput and #ProjectInput freely — they are
     // prepended by the renderer at evaluation time.
     platform: #PlatformInput
     projectResources: {}
     """
   ```

2. Update two test files:

   In `console/templates/examples/examples_test.go`:
   - Increment the `TestExamples` count by 1.
   - Add the new example's `name` to the `wantNames` slice.
   - If the template produces concrete Kubernetes resources (i.e. it is not a policy-only template), add the name to the `exampleResourcesEmitted` switch so `TestExamplePreviewRender` asserts non-empty output.
   - Optionally add a sub-test in `TestExamplePreviewRender_KnownExamples` asserting specific resource kinds and apiVersions for regression coverage (recommended for deployment templates).

   In `console/templates/handler_examples_test.go`:
   - Increment the `wantCount` constant by 1 in `TestListTemplateExamples_HappyPath`.
   - Add the new example's `name` to the `wantNames` slice in `TestListTemplateExamples_KnownNames`.

3. Run `make test-go` to confirm the new example compiles against the `v1alpha2` generated schema and renders correctly through the preview path.

The ConnectRPC `ListTemplateExamples` handler reads the registry at startup; the picker fetches from that RPC. No TypeScript changes are required.

### Docs-sync contract

The `holos-console-docs/demo/` directory hosts **demo walkthrough snippets** tied to the CI demo environment (full cluster configs, hard-coded gateway namespaces, etc.). The `console/templates/examples/` registry hosts **generic drop-in starters** intended for new contributors. The two sets are intentionally different — they serve different audiences.

The sync contract is: **both must compile** against the `v1alpha2` generated schema.

`console/templates/examples/docs_sync_test.go` enforces this contract for the pinned copies of docs snippets stored under `console/templates/examples/testdata/docs-snippets/`. When the docs repo updates a snippet, copy the new version into the corresponding `testdata/` subdirectory and run `make test-go` to confirm it still compiles:

```bash
# Update after a holos-console-docs change:
cp /path/to/holos-console-docs/demo/httpbin-v1/httpbin-v1.cue \
    console/templates/examples/testdata/docs-snippets/httpbin-v1/httpbin-v1.cue
cp /path/to/holos-console-docs/demo/allowed-resources/allowed-resources.cue \
    console/templates/examples/testdata/docs-snippets/allowed-resources/allowed-resources.cue
make test-go
```

## Binary layout

This repo ships two independent binaries from disjoint source trees per [ADR 031](https://github.com/holos-run/holos-console-docs/blob/main/docs/adrs/031-secret-injector-binary-split.md). The `holos-console` web application owns `cmd/holos-console/`, `api/templates/`, `internal/controller/`, `config/holos-console/{crd,rbac,admission}/`, and `Dockerfile.console`; the `holos-secret-injector` controller owns `cmd/secret-injector/`, `api/secrets/`, `internal/secretinjector/`, `config/secret-injector/{crd,rbac}/`, and `Dockerfile.secret-injector`. Shared infrastructure (`console/`, `frontend/`, `proto/`, `pkg/`) is fair game for either binary. The one hard invariant: **no cross-imports** between `internal/controller` and `internal/secretinjector`. `make check-imports` enforces this locally and in CI; if you find yourself reaching across the boundary, lift the shared code into `pkg/` instead. Secret material never travels through templates CRs — CRs carry metadata and `v1.Secret` refs only (ADR 031's no-sensitive-on-CRs rule).

## Secret Injection Service

`holos-secret-injector` is the M2 control-plane binary. Its source tree is
`internal/secretinjector/` (reconcilers), `api/secrets/v1alpha1/` (CRD
types), `internal/secretinjector/crypto/` (KDF + pepper), and
`config/secret-injector/` (manifests, admission policies, RBAC).

### Reconciler package

`internal/secretinjector/controller/` — three reconcilers registered by
`NewManager`:

| Reconciler | Kind | Primary action |
|------------|------|----------------|
| `UpstreamSecretReconciler` | `UpstreamSecret` | Validates upstream v1.Secret exists; publishes `ResolvedRefs` condition |
| `CredentialReconciler` | `Credential` | Mints KSUID + salt, calls KDF.Hash, materialises hash `v1.Secret` |
| `SecretInjectionPolicyBindingReconciler` | `SecretInjectionPolicyBinding` | Resolves policy ref; emits `AuthorizationPolicy` (Istio) |

### Envtest suite

`internal/secretinjector/controller/suite_test.go` — the authoritative
cross-reconciler integration test. Boots a real API server via
`sigs.k8s.io/controller-runtime/pkg/envtest`, installs all four
`secrets.holos.run` CRDs plus the Istio `AuthorizationPolicy` CRD, loads
every `ValidatingAdmissionPolicy` from `config/secret-injector/admission/`,
and runs all three reconcilers simultaneously. The suite skips (not fails)
when the envtest binaries are absent; run `setup-envtest use` to install them.

### Marshal-scan invariant gate

`internal/secretinjector/controller/invariant_test.go` — called after every
reconcile step in the envtest suite. GETs every CR, marshals it to JSON and
YAML, and asserts that the forbidden byte patterns from
`api/secrets/v1alpha1/invariant_patterns.go` produce zero matches in both
representations. A match fails the test without printing the offending bytes.

### No-sensitive-values invariant (MUST READ before editing any CR field)

**CRs in `secrets.holos.run/v1alpha1` are control objects, not vaults.**

They carry references, selectors, lifecycle metadata, phase, and conditions.
They MUST NEVER carry: plaintext credential material, hash bytes, salt bytes,
pepper bytes, API key prefixes, last-4 digits, or any truncation of a
credential that reveals non-trivial entropy. This is the same invariant that
governs `holos-console` template CRs (ADR 031) — it extends to every CR in
this service.

Any agent adding or editing a field in this group must:

1. Verify the field type does not accept sensitive bytes (string fields that
   accept arbitrary values are the most dangerous).
2. Add or update the test in `*_invariant_test.go` that asserts the new
   field cannot be populated with forbidden patterns.
3. Run `make test-go` to confirm the marshal-scan gate passes.

See `api/secrets/v1alpha1/doc.go` for the full rationale and list of
allowed vs. forbidden field categories.

### M2 technical reference

Repo-local docs for the M2 reconcilers (agents need them co-located with
the code):

- [docs/secret-injector/kdf.md](docs/secret-injector/kdf.md) — KDF pluggability seam: `KDF` interface, argon2id defaults, `-fips` swap story, `Envelope` JSON contract.
- [docs/secret-injector/pepper-bootstrap.md](docs/secret-injector/pepper-bootstrap.md) — Pepper bootstrap runbook: Secret shape, versioning contract, Post-MVP rotation notes, RBAC envelope.
- [docs/secret-injector/lifecycle.md](docs/secret-injector/lifecycle.md) — Credential lifecycle contract: single-ownerReference model, delete-cascade semantics, backup/restore ordering, admission vs. reconciler enforcement split.

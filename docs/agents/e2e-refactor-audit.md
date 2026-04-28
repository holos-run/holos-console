# E2E Refactor Audit

This audit classifies every test in `frontend/e2e/` as **Keep**, **Refactor-to-unit**, **Split**, or **Delete** and — for each non-Keep row — names the target Vitest or Go test file and the query hooks or server-side handlers the replacement must cover. Later phases (HOL-653 through HOL-658) consume this document as their work list.

## Decision Rule

From [`test-strategy.md`](test-strategy.md) (imported in HOL-651):

> Prefer unit tests over E2E tests. Rendering, interaction, navigation logic, and ConnectRPC data shaping all belong in unit tests using mocked query hooks. Reserve E2E tests for:
>
> - The OIDC login flow (requires a real Dex server)
> - Full-stack CRUD round-trips that verify server-side behavior (requires a real Kubernetes cluster)

Applied concretely: **Keep** a test in E2E only if removing it would stop exercising either (a) the real Dex-issued token flow or (b) a real Kubernetes round-trip that the Go + Vitest mocks cannot simulate. Everything else moves to unit tests (`frontend/src/**/*.test.tsx` via Vitest + RTL with `vi.mock('@/queries/*')`) or Go tests (`*_test.go` with `k8s.io/client-go/kubernetes/fake`).

## Baseline E2E Wall-Clock Time

Measured from the CI `E2E Tests` job on the last three `main` merges before this audit:

| Run | Started | Completed | Duration |
| --- | --- | --- | --- |
| [24619607567](https://github.com/holos-run/holos-console/actions/runs/24619607567) (PR #1010) | 03:02:41 | 03:14:13 | **11m 32s** |
| [24619233640](https://github.com/holos-run/holos-console/actions/runs/24619233640) (PR #1009) | 02:37:48 | 02:49:01 | **11m 13s** |
| [24618663985](https://github.com/holos-run/holos-console/actions/runs/24618663985) (PR #1008) | 02:02:22 | 02:13:45 | **11m 23s** |

**Baseline: ~11m 23s** (median of three consecutive successful `main` runs). HOL-657 will re-measure after the refactor lands and compare against this number.

## Spec Inventory

`frontend/e2e/` originally contained **11 spec files** totalling **1,576 test-lines** across **58 `test(...)` blocks** (plus `helpers.ts` at 250 lines). After HOL-653 landed, `profile.spec.ts` and `navigation.spec.ts` were deleted and their coverage folded into Vitest unit tests. After HOL-654 landed, `create-dialogs.spec.ts` was also deleted. After HOL-655 landed, `deployments.spec.ts` and `org-settings.spec.ts` were also deleted. After HOL-656 landed, the `Dev Token Endpoint` and `Persona Switching` describes in `multi-persona.spec.ts` were deleted (the `Multi-Persona RBAC` describe remains, keeping 3 tests in the spec); the remaining tables still list all six for historical context.

### Summary Table

| Spec | Tests | Verdict | Status |
| --- | --: | --- | --- |
| `auth.spec.ts` | 14 | **Keep** (OIDC canonical E2E) — 13 Keep + 1 Refactor | pending (HOL-658) |
| `profile.spec.ts` | 5 | **Refactor-to-unit** (all 5) | **done (HOL-653)** — file deleted |
| `navigation.spec.ts` | 2 | **Split** → both refactored to unit | **done (HOL-653)** — file deleted |
| `create-dialogs.spec.ts` | 5 | **Refactor-to-unit** (all 5) | **done (HOL-654)** — file deleted |
| `org-settings.spec.ts` | 2 | **Refactor-to-unit** (all 2) | **done (HOL-655)** — file deleted |
| `deployments.spec.ts` | 3 | **Refactor-to-unit** (all 3) | **done (HOL-655)** — file deleted |
| `folders.spec.ts` | 6 | **Keep** (K8s CRUD) | keep |
| `folder-rbac.spec.ts` | 3 | **Keep** (K8s RBAC cascade) | keep |
| `folder-templates.spec.ts` | 2 | **Keep** (K8s template release) | keep |
| `secrets.spec.ts` | 6 | **Split** (4 Keep, 1 Refactor, 1 Delete) | pending (HOL-658) |
| `multi-persona.spec.ts` | 10 | **Split** (4 → Go tests, 2 → unit, 1 Delete, 3 Keep) | **done (HOL-656)** — 7 tests removed from E2E |
| **Total** | **58** | **Keep: 32, Refactor: 23, Delete: 3** | HOL-653 + HOL-654 + HOL-655 + HOL-656 complete: 24 tests removed from E2E |

Projected reduction: **~45% of E2E test bodies** leave the E2E suite (26 of 58) — 23 move to unit/Go tests, 3 are deleted as redundant with existing coverage. The remaining E2E job is focused on OIDC auth and real K8s round-trips.

**HOL-653 delta**: After this phase, `frontend/e2e/` holds **9 spec files** and **51 `test(...)` blocks** (5 profile + 2 navigation deleted).

**HOL-654 delta**: After this phase, `frontend/e2e/` holds **8 spec files** and **46 `test(...)` blocks** (5 create-dialogs deleted). The new Vitest coverage lives in `frontend/src/components/create-project-dialog.test.tsx` (3 new tests: auto-derived-slug submit + navigate, manual override + reset affordance, pending submit), `frontend/src/components/create-org-dialog.test.tsx` (1 new test: pending submit), and `frontend/src/components/app-sidebar.test.tsx` (4 new tests across two describes: org-picker with existing orgs surfaces a "New Organization" item below the listed orgs, and the symmetric project-picker assertion for "New Project").

**HOL-655 delta**: After this phase, `frontend/e2e/` holds **6 spec files** and **41 `test(...)` blocks** (3 deployments + 2 org-settings deleted). The audit plan was satisfied almost entirely by coverage that already existed in `-new.test.tsx`, `-index.test.tsx`, `-settings.test.tsx`, and `app-sidebar.test.tsx` (the no-templates affordance, Create Deployment link wiring, RBAC-driven button visibility, `Org Settings` sidebar link, and display-name / description / sharing form state were all already asserted at the component level). Two small anti-regression tests were added to preserve invariants the E2E suite uniquely exercised: `renders as a standalone page (not inside a dialog)` in `frontend/src/routes/_authenticated/projects/$projectName/deployments/-new.test.tsx` (guards against the Create Deployment modal regression from issue #396), and `renders the {orgName} / Settings breadcrumb on the page header` in `frontend/src/routes/_authenticated/organizations/$orgName/settings/-settings.test.tsx` (guards the `"{orgName} / Settings"` header string the E2E sidebar-click test asserted). No `deployments.spec.ts` case required a K8s round-trip to observe persistence — the Deployment creation round-trip lives entirely under `folder-templates.spec.ts` — so the spec was fully removed rather than split.

**HOL-656 delta**: After this phase, `frontend/e2e/` holds **6 spec files** and **34 `test(...)` blocks** (4 Dev Token Endpoint cases + 3 Persona Switching cases removed from `multi-persona.spec.ts`; the 3 Multi-Persona RBAC cases remain). The new Go coverage lives in `console/oidc/token_exchange_test.go`: `TestHandleTokenExchange_Personas` (4 sub-tests — admin/platform/product/SRE response-shape table), `TestHandleTokenExchange_SignatureVerification` (4 sub-tests — JWS parse + public-key verify per persona, covering the invariant the browser tests implicitly relied on when they injected the returned id_token into sessionStorage), `TestHandleTokenExchange_ClaimContents` (3 sub-tests — iss/aud/sub/email/email_verified/groups/iat/exp claims plus the 1-hour expiry window), and an extended `TestHandleTokenExchange_UnknownEmail` (2 sub-tests that assert the `"unknown test user email"` body fragment the E2E case checked, including the exact `unknown@example.com` literal). The new Vitest coverage lives in `frontend/src/routes/_authenticated/-profile.test.tsx` under `ProfilePage persona email rendering`: an `it.each` over all four persona emails plus a rerender test that swaps useAuth's return value and asserts the displayed email updates. `getPersonaToken`, `switchPersona`, and `loginAsPersona` stay in `frontend/e2e/helpers.ts` — the remaining three RBAC tests still use them.

---

## Per-Test Audit

### `auth.spec.ts` — Keep all (OIDC)

OIDC login against a real Dex server is the canonical E2E use case. Unit tests cannot replace tests that drive the Dex authorize endpoint, discovery document, and PKCE redirect chain.

| Test | Verdict | Notes |
| --- | --- | --- |
| `Authentication > should auto-login unauthenticated users via OIDC` | **Keep** | Exercises the full `/` → `/dex/auth` → `/pkce/verify` → `/profile` redirect chain with a real Dex session. |
| `Authentication > should have about page accessible after login` | **Keep** | Verifies RPC proxy wiring after a real login (`GET /.well-known/` + server version). |
| `Authentication > should have OIDC discovery endpoint accessible` | **Keep** | Checks the real Dex discovery document. Cannot be mocked meaningfully. |
| `Authentication > should display Dex login page when accessing authorize endpoint` | **Keep** | Hits `/dex/auth` directly. |
| `Login Flow > should show login form with username and password fields` | **Keep** | Requires a live Dex login page render. |
| `Login Flow > should reject invalid credentials` | **Keep** | Exercises Dex auth rejection. |
| `Login Flow > should complete login with valid credentials` | **Keep** | End-to-end credential flow. |
| `Profile Page > should auto-login unauthenticated users navigating to profile` | **Keep** | OIDC redirect from `/profile`. |
| `Profile Page > should navigate to profile page from sidebar` | **Keep** | Verifies the sidebar-to-profile click round-trip after a real login. Note: this is the one auth-spec test that is nominally UI, but the login prerequisite keeps it cheap to leave in E2E alongside the other OIDC tests. |
| `Profile Page > should complete full login flow via profile page` | **Keep** | Covers post-login claim rendering after real OIDC. |
| `Profile Page > should display token claims after login` | **Keep (minimize)** | Could be trimmed: the claim-label rendering is covered by `-profile.test.tsx`. The E2E value is that the claims come from a **real** Dex-issued ID token, not a fixture. Retain the smoke assertion, drop the per-claim enumeration. |
| `Profile Page > should include roles / groups in claims view` | **Keep (minimize)** | Same reasoning — keep the real-token smoke, drop the label-enumeration portion (unit-tested). |
| `Profile Page > should display iss claim from embedded Dex` | **Keep** | Specifically asserts the **real** embedded-Dex issuer — cannot be mocked. |
| `Profile Page > should switch to raw JSON view and show complete claims` | **Refactor-to-unit (merge with smoke)** | Raw-view toggle is UI-only; already covered by `-profile.test.tsx` *"switches to raw view and shows JSON"*. Safe to delete from `auth.spec.ts`. |

**Net:** 13 tests kept (two marked "minimize"), 1 deletable. HOL-658 cleanup ticket should drop the raw-view and redundant per-label assertions once the unit migration lands.

### `profile.spec.ts` — Refactor-to-unit (all 5) — **DONE (HOL-653)**

These tests exercised the API Access card's copy snippet and shell-history tabs. They are pure UI state — no Dex, no K8s, no server round-trip — and the `useAuth` hook is mocked directly.

**Target:** Extended `frontend/src/routes/_authenticated/-profile.test.tsx` (already covered the API Access card and shell-history tabs for the zsh-default case).

**Mocks used:** `vi.mock('@/lib/auth', () => ({ useAuth: vi.fn() }))` with `id_token: 'id.token.value'` (pattern already in place in `-profile.test.tsx`). No query-hook mocks required.

| Test | Verdict | Target | Outcome |
| --- | --- | --- | --- |
| `pre block shows single-line export without history wrapper` | **Refactor** | `-profile.test.tsx` | Already covered by `"copies a clean export line with the id_token on copy"` — deleted. |
| `shell history tabs are visible with zsh and bash triggers` | **Refactor** | `-profile.test.tsx` | Already covered by `"renders shell history tabs with zsh and bash triggers"` — deleted. |
| `zsh tab is selected by default and shows setopt instructions` | **Refactor** | `-profile.test.tsx` | Already covered by `"shows zsh tab content by default"` — deleted. |
| `clicking bash tab reveals bash-specific instructions` | **Refactor** | `-profile.test.tsx` | Already covered by `"switches to bash tab and shows bash-specific instructions"` — deleted. |
| `clicking zsh tab after bash returns to zsh content` | **Refactor** | `-profile.test.tsx` | Added `"clicking zsh tab after bash restores zsh content"` — userEvent click bash, click zsh, asserts active tab data-state plus panel content. |

**`profile.spec.ts` deleted** in HOL-653. All coverage lives in `-profile.test.tsx`.

### `navigation.spec.ts` — Split → both refactored to unit (HOL-653) — **DONE**

| Test | Verdict | Target | Outcome |
| --- | --- | --- | --- |
| `Sidebar Project Picker navigation > selecting a project from the picker navigates directly to secrets page` | **Refactor-to-unit** | `frontend/src/components/app-sidebar.test.tsx` | Added `"selecting a project in the picker navigates directly to its secrets page"` and a symmetric `"selecting All Projects…navigates to the org projects page and clears selection"` test. The existing `mockNavigate` wired through `useRouter` lets us assert the router-navigate call directly. |
| `Phase 4: Navigation friction removal > full flow via sidebar pickers reaches secrets grid in 2 clicks` | **Delete (redundant)** | — | The audit originally tagged this **Keep** for the K8s round-trip. Re-evaluated at implementation time: the secret-creation round-trip is already covered by `secrets.spec.ts > should show sharing summary in secrets list` (creates a secret, asserts it appears in the list), so this test duplicated server-side coverage. The picker-navigation portion is covered by the new unit test. **Deleted.** |

`navigation.spec.ts` is gone. This phase lived in **HOL-653** alongside the profile migration.

### `create-dialogs.spec.ts` — Refactor-to-unit (all 5) — **DONE (HOL-654)**

The dialog validation, auto-slug, and reset-affordance behaviours are pure form state. The "picker-menu-item-renders-at-bottom" assertions are also pure UI. The create → navigate-to-secrets case looks like a full-stack round-trip at first glance, but the only thing E2E verifies that a unit test cannot is that the **slug URL the router produces matches the slug the server created** — and that invariant is already covered by `secrets.spec.ts` (which creates a project and then a secret under it). With mocked `useCreateProject` and `useNavigate`, all five cases collapsed into pure UI assertions.

**Unit targets:** `frontend/src/components/create-project-dialog.test.tsx`, `frontend/src/components/create-org-dialog.test.tsx`, and `frontend/src/components/app-sidebar.test.tsx` — all three already existed; extended in place.

**Mocks used (HOL-654):**
- `vi.mock('@/queries/projects', () => ({ useCreateProject: vi.fn(), useListProjects: vi.fn() }))` — already present in `create-project-dialog.test.tsx` and `app-sidebar.test.tsx`.
- `vi.mock('@/queries/organizations', () => ({ useCreateOrganization: vi.fn(), useListOrganizations: vi.fn(), useGetOrganization: vi.fn() }))` — already present.
- Router mock: `vi.mock('@tanstack/react-router', ...)` was updated in `create-project-dialog.test.tsx` to expose a hoisted `mockNavigate` spy so the post-create `navigate({ to: '/projects/$projectName/secrets', params: { projectName } })` call can be asserted. `useAuth` was not needed because the dialog does not depend on it.

| Test | Verdict | Target | Outcome |
| --- | --- | --- | --- |
| `Create Organization dialog > existing user sees New Organization item at bottom of org picker dropdown` | **Refactor** | `frontend/src/components/app-sidebar.test.tsx` | Added describe `"AppSidebar — OrgPicker menu with existing orgs"` with two tests that render with two orgs, confirm `getByTestId('org-picker')` is present, and assert the "New Organization" text node appears *after* the last org label via `compareDocumentPosition`. |
| `Create Project dialog > org with no projects shows New Project CTA` | **Refactor** | `frontend/src/components/app-sidebar.test.tsx` | Already covered by the existing describe `"AppSidebar — ProjectPicker empty state"` which asserts the "New Project" button is visible and `queryByTestId('project-picker')` is null. No new test needed. |
| `Create Project dialog > create project dialog opens, submits via display name auto-slug, and navigates to secrets page` | **Refactor** | `create-project-dialog.test.tsx` | Added `"submits with auto-derived slug and navigates to the new project secrets page"` — fills displayName, asserts auto-derived slug in the Name field, submits, asserts `mockMutateAsync` called with `{ name, displayName, organization }` and `mockNavigate` called with `{ to: '/projects/$projectName/secrets', params: { projectName: expectedSlug } }` using the server-returned name (not the local slug). Also added `"disables submit and shows Creating… label while the mutation is pending"`. |
| `Create Project dialog > create project dialog: manually overriding name stops auto-derivation and shows reset affordance` | **Refactor** | `create-project-dialog.test.tsx` | Added `"manually overriding name stops auto-derivation and the reset link restores it"` — a single test that exercises the full E2E flow (type displayName → override name → change displayName (no effect) → click reset → displayName change re-derives). Consolidates what the existing test file split across four separate its. |
| `Create Project dialog > existing user sees New Project item at bottom of project picker dropdown` | **Refactor** | `frontend/src/components/app-sidebar.test.tsx` | Added describe `"AppSidebar — ProjectPicker menu with existing projects"` with two tests that confirm the picker trigger renders and the "New Project" item is after the last project via `compareDocumentPosition`. |

Also added `"disables submit and shows Creating… label while the mutation is pending"` in `create-org-dialog.test.tsx` to cover the pending-state branch symmetrically (the E2E test could not observe this reliably).

`create-dialogs.spec.ts` deleted in HOL-654. All coverage lives in the three extended unit-test files.

### `org-settings.spec.ts` — Refactor-to-unit (all 2) — **DONE (HOL-655)**

Both tests are pure sidebar-link-visibility and route-renders-with-org-name. No server round-trip beyond what the standard app-sidebar unit test already covers.

**Targets:**
- Test 1 → `frontend/src/components/app-sidebar.test.tsx`
- Test 2 → `frontend/src/routes/_authenticated/organizations/$orgName/settings/-settings.test.tsx` (already 537 lines covering the settings form itself)

**Mocks needed:**
- `vi.mock('@/queries/organizations', () => ({ useListOrganizations: vi.fn(), useGetOrganization: vi.fn() }))`
- `vi.mock('@/lib/org-context', () => ({ useOrg: vi.fn() }))`

| Test | Verdict | Target | Outcome |
| --- | --- | --- | --- |
| `Org Settings page > settings link appears in sidebar when org is selected` | **Refactor** | `app-sidebar.test.tsx` | Already covered by the existing `"renders org Settings link labeled 'Org Settings' with correct href"` test (plus the sibling `"shows 'Org Settings' label instead of 'Settings' in org nav"`). Both mock `useOrg` with a selected org and assert the link renders with the correct href — exactly the E2E invariant. No new test required. |
| `Org Settings page > clicking Settings in sidebar navigates to settings page` | **Refactor** | `-settings.test.tsx` | The page-render portion is covered by existing tests (`"renders display name and description from org data"`, `"renders name (slug) as read-only"`, etc.). Added `"renders the {orgName} / Settings breadcrumb on the page header"` to cover the exact `"{orgName} / Settings"` header string the E2E test asserted after navigating. |

**`org-settings.spec.ts` deleted** in HOL-655.

### `deployments.spec.ts` — Refactor-to-unit (all 3) — **DONE (HOL-655)**

The no-templates affordance is a pure UI branch on `useListTemplates({ scope: 'PROJECT' })` returning empty. The "clicking Create Deployment navigates to new page" test is pure router behaviour. The "has templates → shows submit button" test is the mirror of the empty case.

**Target:** `frontend/src/routes/_authenticated/projects/$projectName/deployments/-new.test.tsx` (already exists and covers form fields). Extend it with the three affordance cases.

**Mocks needed:**
- `vi.mock('@/queries/templates', () => ({ useListTemplates: vi.fn(), useCreateDeployment: vi.fn() }))`
- `vi.mock('@/queries/deployments', () => ({ useListDeployments: vi.fn(), useCreateDeployment: vi.fn() }))`
- `vi.mock('@/lib/auth', () => ({ useAuth: vi.fn() }))`

| Test | Verdict | Target | Outcome |
| --- | --- | --- | --- |
| `Create Deployment page — no-templates affordance > shows "No templates available..." when no templates exist` | **Refactor** | `-new.test.tsx` | Already covered by the existing `"shows 'no templates' link to create templates page when no templates exist"` and the field-ordering variant `"renders 'No templates available' fallback as the first field when templates list is empty"`. Both mock `useListTemplates` with `data: []` and assert the affordance plus the `create a template` link. |
| `... > does not show no-templates affordance when templates exist` | **Refactor** | `-new.test.tsx` | Already covered by `"does not show 'no templates' message when templates exist"` (negation of the above). The submit button is rendered by every other test in the file already. |
| `... > clicking "Create Deployment" link on list page navigates to new page` | **Refactor** | `-index.test.tsx` | Already covered by `"renders Create Deployment link for owners"`, `"renders Create Deployment link for editors"`, `"does not render Create Deployment link for viewers"`, and `"Create Deployment link in empty state navigates to new page"`. All four assert the link's `href` resolves to `deployments/new`. Added `"renders as a standalone page (not inside a dialog)"` in `-new.test.tsx` to preserve the E2E anti-regression against the pre-#396 modal. |

**`deployments.spec.ts` deleted** in HOL-655.

### `folders.spec.ts` — Keep (all 5, require K8s)

Every test in this spec creates real Kubernetes namespaces (folder-backed orgs), exercises the hierarchy-list API, and asserts the DOM reflects what the cluster returned. Deletion would lose coverage of the full-stack folder CRUD flow.

| Test | Verdict | Notes |
| --- | --- | --- |
| `Folder list page > shows folders under an org and navigates to folder detail` | **Keep** | K8s namespace create + list. |
| `Folder list page > new org appears without an implicit folder` | **Keep** | Verifies organization creation without an auto-created folder in Kubernetes. |
| `Folder detail page > shows folder name and organization` | **Keep** | GET-on-folder CRUD path. |
| `Nested folder workflow > creates org → parent folder → child folder, both visible in list` | **Keep** | Parent-child K8s hierarchy. |
| `Nested folder workflow > project under folder shows in folder breadcrumb context` | **Keep** | Cross-resource K8s relationship. |
| `Sidebar Folders navigation > org nav section includes Folders link` | **Refactor candidate (low priority)** | This one *could* move to `app-sidebar.test.tsx`, but the API-create-org prerequisite makes the unit version non-trivial; leave in E2E unless a future cleanup phase targets it. |

### `folder-rbac.spec.ts` — Keep (all 3, require K8s RBAC cascade)

Folder RBAC is enforced by Kubernetes RBAC per ADR 036. These tests exercise the end-to-end wiring (HTTP → handler → K8s RBAC → UI delete button visibility) which only exists in E2E.

| Test | Verdict | Notes |
| --- | --- | --- |
| `Folder RBAC - owner can manage folder > org owner can create and delete a folder` | **Keep** | K8s + UI delete affordance. |
| `Folder RBAC - owner can manage folder > org owner can see folder sharing panel` | **Keep** | K8s + sharing UI. |
| `Folder RBAC - metadata persisted in Kubernetes > folder raw JSON includes correct organization label` | **Keep** | Direct assertion on K8s namespace annotations via `GetFolderRaw` RPC. |

### `folder-templates.spec.ts` — Keep (all 2, require K8s template release)

Template-release round-trip (create template → render → list in UI) requires the real cluster.

| Test | Verdict | Notes |
| --- | --- | --- |
| `Folder-scoped templates > folder template appears in folder templates list page` | **Keep** | K8s + render unification. |
| `Folder-scoped templates > folder without templates shows empty state` | **Keep** | K8s list-empty assertion; could theoretically move to unit, but the fixture-setup cost of the other test already pays for the spec so leave both together. |

### `secrets.spec.ts` — Split (4 Keep, 2 mobile → delete/consolidate)

CRUD tests exercise the real Kubernetes secrets API, which unit tests cannot replace.

| Test | Verdict | Target | Notes |
| --- | --- | --- | --- |
| `Secrets Page > should create secret with sharing and show sharing panel` | **Keep** | — | Full CRUD round-trip against K8s. |
| `Secrets Page > should update sharing grants on secret page` | **Keep** | — | Sharing update → K8s annotations → UI read-back. |
| `Secrets Page > should show sharing summary in secrets list` | **Keep** | — | List API + summary derivation. |
| `Secrets Page > should allow adding a key to an empty secret on the detail page` | **Keep** | — | Edit → save → reload round-trip. |
| `Mobile Responsive Layout > should show hamburger menu and hide sidebar on mobile` | **Delete (redundant)** | — | Already covered by `mobile-chrome` project running every other spec; the standalone assertion adds no K8s coverage. After deletion, keep the mobile viewport running against `auth.spec.ts` and `folders.spec.ts` to retain mobile layout coverage. |
| `Mobile Responsive Layout > should open drawer and show sidebar navigation on mobile` | **Refactor-to-unit** | `frontend/src/components/app-sidebar.test.tsx` | The drawer-open + visible-profile-link assertion is pure UI at a mobile breakpoint. Add a Vitest test that renders the sidebar with `matchMedia('(max-width: ...)')` mocked to match, clicks the toggle, asserts Profile link is visible. This phase is a stretch goal for **HOL-658** cleanup — no explicit phase ticket owns it. |

The two mobile tests are the only refactor candidates in this spec; the CRUD tests stay. This phase (if scoped) would live in **HOL-658** cleanup.

### `multi-persona.spec.ts` — Split (4 → Go tests, 3 → unit, 3 Keep) — **DONE (HOL-656)**

The first four tests call `POST /api/dev/token` and assert the response shape — they do **not** need a browser. Move them to Go tests against the `HandleTokenExchange` handler in `console/oidc/token_exchange_test.go` (which already contains similar unit tests via `httptest`). The three persona-switching tests exercise UI email display; the three RBAC grant tests require K8s.

**Go target:** `console/oidc/token_exchange_test.go` (already has `TestHandleTokenExchange_Success`, etc.). Extend it.

**Unit target for persona-switch display:** `frontend/src/routes/_authenticated/-profile.test.tsx` — add per-email rendering tests that mock `useAuth` with the persona's email.

| Test | Verdict | Target | Notes |
| --- | --- | --- | --- |
| `Dev Token Endpoint > should return a valid token for the platform engineer persona` | **Refactored to Go** | `console/oidc/token_exchange_test.go` | Added as the `platform engineer persona returns owner group` row in `TestHandleTokenExchange_Personas` (table-driven); spot-checks `id_token`, `email`, `groups: ["owner"]`, `expires_in > 0`. Signature verification and claim contents are asserted by `TestHandleTokenExchange_SignatureVerification` / `TestHandleTokenExchange_ClaimContents`. |
| `Dev Token Endpoint > should return a valid token for the product engineer persona` | **Refactored to Go** | `console/oidc/token_exchange_test.go` | Added as the `product engineer persona returns editor group` row in `TestHandleTokenExchange_Personas` (table-driven). |
| `Dev Token Endpoint > should return a valid token for the SRE persona` | **Refactored to Go** | `console/oidc/token_exchange_test.go` | Added as the `SRE persona returns viewer group` row in `TestHandleTokenExchange_Personas` (table-driven). |
| `Dev Token Endpoint > should reject unknown email addresses` | **Refactored to Go** | `console/oidc/token_exchange_test.go` | Extended `TestHandleTokenExchange_UnknownEmail` to a two-row table that covers both `nobody@localhost` and the E2E literal `unknown@example.com`, and added the body-fragment assertion (`strings.Contains(body, "unknown test user email")`). |
| `Persona Switching > should login as platform engineer and show correct email` | **Refactored to unit** | `-profile.test.tsx` | Added `ProfilePage persona email rendering` describe with an `it.each` over all four persona emails (platform, product, SRE, admin); each row mocks `useAuth` with the persona's email and asserts it renders on the profile page. |
| `Persona Switching > should switch from admin to SRE persona` | **Refactored to unit** | `-profile.test.tsx` | Added `updates the displayed email when useAuth rerenders with a new persona` — renders as admin, rerenders with SRE's email, asserts the new email appears and the old one is gone. |
| `Persona Switching > should switch between all three non-admin personas` | **Deleted (redundant)** | — | The `it.each` over all four personas plus the rerender test together cover the same logic as the three-step browser test. Deleted rather than split. |
| `Multi-Persona RBAC > platform engineer can create an org and grant SRE viewer access` | **Keep** | — | K8s org create + RBAC grant. |
| `Multi-Persona RBAC > SRE can list the org after being granted viewer access` | **Keep** | — | K8s list with persona-scoped RBAC. |
| `Multi-Persona RBAC > product engineer can access the org with editor privileges` | **Keep** | — | K8s list with editor-scoped RBAC. |

After the refactor (landed in HOL-656):
- The `Dev Token Endpoint` describe was deleted from `multi-persona.spec.ts`; the 4 response-shape cases plus their signature-verification and claim-content invariants moved to `console/oidc/token_exchange_test.go`.
- The `Persona Switching` describe was deleted from `multi-persona.spec.ts`; 2 cases moved to `-profile.test.tsx` (per-persona email render + rerender-swap), the three-persona cycle was dropped as redundant.
- The `Multi-Persona RBAC` describe is unchanged; it still covers org create + RBAC grants against a real K8s cluster.

**Result: `multi-persona.spec.ts` shrank from 10 tests to 3.** This phase lived in **HOL-656**.

---

## Phase Assignments

| Phase | Tickets | Scope |
| --- | --- | --- |
| HOL-653 | profile.spec.ts (5), navigation.spec.ts (1) | 6 tests → Vitest |
| HOL-654 | create-dialogs.spec.ts (5) | 5 tests → Vitest |
| HOL-655 | deployments.spec.ts (3), org-settings.spec.ts (2) | 5 tests → Vitest (DONE) |
| HOL-656 | multi-persona.spec.ts Dev Token (4), Persona Switching (3) | 4 → Go, 3 → Vitest (DONE) |
| HOL-657 | — | Measure E2E CI wall-clock after the four refactor phases; compare against the 11m 23s baseline. |
| HOL-658 | auth.spec.ts trims, helpers.ts cleanup, mobile consolidation | Remove dead helpers, trim auth-spec overlaps, decide on mobile responsive tests. |

## Notes for Implementers

- **Always extend existing route-directory test files** (`-profile.test.tsx`, `-settings.test.tsx`, `-new.test.tsx`, etc.) rather than creating new ones. The existing files already set up the necessary mocks and router stubs; adding a test body is a ~20-line change, creating a new file is a ~100-line change.
- **Delete the refactored E2E tests in the same PR that adds the unit coverage.** Dead E2E code continues to run in CI and contributes to the 11-minute runtime; leaving "just in case" defeats the purpose.
- **Preserve the E2E mobile-chrome project** even after deleting the two mobile-only tests — it runs every remaining spec at a phone viewport and catches responsive regressions for free.
- **Do not add E2E tests in the replacement PRs.** If a behaviour needs verification and doesn't fit the Keep criteria (OIDC or K8s round-trip), it belongs in Vitest. The whole point of this refactor is to reverse the creep that pushed E2E from 4 minutes to 11 minutes.
- **Verify with `make test` before each phase lands.** E2E is not required for the refactor phases (HOL-653 through HOL-656) because they delete E2E tests and add unit tests; `make test-ui` + `make test-go` are the relevant gates.

---

## Results (HOL-657)

This section records the post-refactor CI wall-clock measurement and compares it against the pre-refactor baseline captured at the top of this document. The goal was to reduce the `E2E Tests` job runtime by pushing UI-only tests into Vitest and response-shape tests into Go.

### Summary

| Metric | Pre-refactor (median) | Post-refactor (HOL-656) | Delta |
| --- | --- | --- | --- |
| `E2E Tests` job wall-clock | **11m 23s** | **7m 43s** | **-3m 40s (-32%)** |
| Playwright per-test seconds (sum of all test durations) | 495.1s | 275.5s | -219.6s (-44%) |
| Playwright tests executed (chromium + mobile-chrome) | 110 | 62 | -48 (-44%) |
| `test(...)` blocks in `frontend/e2e/` | 58 (11 specs) | 34 (6 specs) | -24 blocks / -5 specs |

**Outcome:** the refactor met its goal. The `E2E Tests` CI job now finishes in ~7m 43s on `main`, down from a ~11m 23s median over the three pre-refactor runs. The reduction is entirely explained by the -44% drop in Playwright per-test seconds (-219.6s of pure test work removed); the ~140-second fixed overhead of the job (setup-go, setup-node, mkcert, k3s boot, Playwright browser install, Go binary build) stays roughly constant and now dominates the wall clock.

The 7m 43s number is **still over the ~6-minute target** flagged in the HOL-657 acceptance criteria, so a "Long-pole analysis" section follows below with the three longest specs and a per-spec recommendation.

### Runs Compared

**Pre-refactor baseline** (same data as the [Baseline E2E Wall-Clock Time](#baseline-e2e-wall-clock-time) table above, restated here for side-by-side reading):

| Run | PR | E2E job start | E2E job end | Duration |
| --- | --- | --- | --- | --- |
| [24619607567](https://github.com/holos-run/holos-console/actions/runs/24619607567) | #1010 (HOL-647) | 03:02:41 | 03:14:13 | **11m 32s** |
| [24619233640](https://github.com/holos-run/holos-console/actions/runs/24619233640) | #1009 (HOL-646) | 02:37:48 | 02:49:01 | **11m 13s** |
| [24618663985](https://github.com/holos-run/holos-console/actions/runs/24618663985) | #1008 (HOL-645) | 02:02:22 | 02:13:45 | **11m 23s** |

**Post-refactor, per phase** (measured from the merge-to-`main` CI run for each phase -- so each row captures the job runtime against `main` after the phase's PR landed):

| Run | PR | Phase | E2E job start | E2E job end | Duration | Delta vs. baseline |
| --- | --- | --- | --- | --- | --- | --- |
| [24632615504](https://github.com/holos-run/holos-console/actions/runs/24632615504) | #1011 | HOL-651 (docs import) | 15:31:15 | 15:42:35 | **11m 20s** | -3s |
| [24632972429](https://github.com/holos-run/holos-console/actions/runs/24632972429) | #1012 | HOL-652 (audit doc) | 15:49:03 | 16:00:24 | **11m 21s** | -2s |
| [24633333160](https://github.com/holos-run/holos-console/actions/runs/24633333160) | #1014 | HOL-653 (profile + nav to Vitest) | 16:07:20 | 16:17:49 | **10m 29s** | -54s |
| [24633917834](https://github.com/holos-run/holos-console/actions/runs/24633917834) | #1015 | HOL-654 (create-dialogs to Vitest) | 16:37:23 | 16:46:56 | **9m 33s** | -1m 50s |
| [24634317391](https://github.com/holos-run/holos-console/actions/runs/24634317391) | #1016 | HOL-655 (deployments + org-settings to Vitest) | 16:57:59 | 17:06:34 | **8m 35s** | -2m 48s |
| [24634599394](https://github.com/holos-run/holos-console/actions/runs/24634599394) | #1017 | HOL-656 (multi-persona split -- PR validation) | 17:12:36 | 17:20:19 | **7m 43s** | **-3m 40s** |

**Post-refactor data point used for the summary table:** the PR validation run for HOL-656 (24634599394, E2E = 7m 43s). The HOL-651 and HOL-652 rows are included to show that docs-only phases do not move the needle; the wall-clock reduction begins in HOL-653 when the first E2E tests are actually deleted. The per-phase reduction maps cleanly to the per-phase test-count reduction, which is the strongest confirmation that the savings come from the refactor and not from runner-pool variance.

### Per-Spec Breakdown

Playwright's `--reporter=list` output (pulled from the GitHub Actions log of each job) gives per-test durations. The table below sums the durations across both browser projects (`chromium` + `mobile-chrome`) per spec. "Per-test seconds" is the sum of every `test(...)` block's reported duration in a spec; the `E2E Tests` job wall-clock is larger because it also includes the WebServer boot, Dex startup, k3s install, and fixture teardown per test.

**Pre-refactor (run 24619607567, HOL-647 merge):** 110 Playwright tests, 495.1s of test work across 11 spec files.

| Spec | Tests (chromium + mobile) | Sum per-test seconds | Status after refactor |
| --- | --: | --: | --- |
| `secrets.spec.ts` | 10 | 98.3s | **Keep** (K8s CRUD) |
| `folders.spec.ts` | 12 | 71.6s | **Keep** (K8s hierarchy) |
| `multi-persona.spec.ts` | 20 | 57.8s | **Split** -- 7 removed in HOL-656, 3 remain |
| `navigation.spec.ts` | 4 | 55.0s | **Deleted** in HOL-653 (long pole -- 20.4s chromium + 18.4s mobile for one nav-flow test) |
| `deployments.spec.ts` | 6 | 51.0s | **Deleted** in HOL-655 |
| `create-dialogs.spec.ts` | 10 | 46.4s | **Deleted** in HOL-654 |
| `folder-rbac.spec.ts` | 6 | 35.4s | **Keep** (K8s RBAC cascade) |
| `auth.spec.ts` | 24 | 27.4s | **Keep** (OIDC) -- trims pending in HOL-658 |
| `folder-templates.spec.ts` | 4 | 24.4s | **Keep** (K8s template release) |
| `org-settings.spec.ts` | 4 | 16.1s | **Deleted** in HOL-655 |
| `profile.spec.ts` | 10 | 11.7s | **Deleted** in HOL-653 |
| **Total** | **110** | **495.1s** | -- |

**Post-refactor (run 24634599394, HOL-656 PR validation):** 62 Playwright tests, 275.5s of test work across 6 spec files.

| Spec | Tests (chromium + mobile) | Sum per-test seconds | Delta vs. pre |
| --- | --: | --: | --- |
| `secrets.spec.ts` | 12 (10 CRUD + 2 mobile) | 98.2s | +/-0s |
| `folders.spec.ts` | 12 | 73.5s | +1.9s |
| `folder-rbac.spec.ts` | 6 | 33.1s | -2.3s |
| `auth.spec.ts` | 24 | 26.3s | -1.1s |
| `folder-templates.spec.ts` | 4 | 24.4s | +/-0s |
| `multi-persona.spec.ts` | 6 (3 RBAC x 2 browsers) | 20.0s | -37.8s |
| **Total** | **62** | **275.5s** | -219.6s |

The -219.6s drop in per-test seconds matches the -3m 40s drop in job wall-clock within ~20 seconds (attributable to parallelization overhead -- Playwright runs multiple tests concurrently, so a spec that halves its test count does not halve its wall-clock contribution). The four Keep specs (`secrets`, `folders`, `folder-rbac`, `folder-templates`) held steady +/-3s across the two runs, which is strong evidence the measurement is stable and the savings are entirely from deleted tests, not runner variance.

### Long-Pole Analysis (Over the ~6-minute Target)

The current 7m 43s E2E job breaks down (approximately) as:

- **~2m 20s fixed overhead** -- checkout, setup-go, setup-node, npm install, buf + mkcert install, cert generation, `go build` of the server binary, k3s install, Playwright browser install (all happen before any test runs; steps 1-11 from the job log totaled ~2m in the HOL-656 run).
- **~4m 52s Playwright run step** -- start WebServer, Dex, drive tests across two browser projects, tear down per-test K8s fixtures. The sum of per-test durations is 275.5s, but the job serializes fixture teardown and Dex login across the two projects, so the wall-clock of the test step is higher than the sum.
- **~31s finalize** -- tear down Playwright, post-run hooks, upload-skipped, complete job.

With ~2m 20s of fixed overhead that no amount of test-deletion can remove, hitting a 6-minute total means the Playwright step has to finish in ~3m 40s (-72s from where it is today). The three specs below are the longest poles in the post-refactor suite and are where further trimming would yield the biggest wins.

#### Longest specs, post-refactor

| Rank | Spec | Tests | Per-test seconds | Recommendation |
| --- | --- | --: | --: | --- |
| 1 | `secrets.spec.ts` | 12 | 98.2s | **Accept** for now -- 4 of the 10 `Secrets Page` tests are the longest individual tests in the whole suite (10.2s-15.2s each) because each one creates a real K8s Secret resource with sharing, re-reads it, and tears it down. These are the canonical K8s round-trip tests the audit flagged as **Keep**; refactoring them to unit tests would lose the K8s coverage that is exactly why E2E exists. **Follow-up candidate for HOL-658:** delete the two `Mobile Responsive Layout` tests (67 and 68 in the current run) -- they add only 2.3s but the audit already lists them as "Delete (redundant)" and "Refactor-to-unit"; cleanup will remove them. |
| 2 | `folders.spec.ts` | 12 | 73.5s | **Accept** -- every test creates K8s namespaces and asserts against them. The 9.8s "creates org -> parent folder -> child folder" test and the 8.2s "project under folder shows in folder breadcrumb context" test are irreducible because they exercise the parent-child K8s hierarchy. The only candidate for removal is `Sidebar Folders navigation > org nav section includes Folders link` (5.3s mobile + 3.8s chromium = 9.1s total), which the audit already flagged as a "Refactor candidate (low priority)" -- the API-create-org prerequisite makes the unit version expensive, so leave in E2E. |
| 3 | `folder-rbac.spec.ts` | 6 | 33.1s | **Accept** -- all three tests write RBAC metadata to real K8s namespace annotations and assert the cascade. The per-test duration (4.6s-6.4s) is dominated by namespace creation latency, not by avoidable work. |

**Recommendation for HOL-658 cleanup:** the remaining ~72-second gap between 7m 43s and 6m 00s is mostly unreachable without sharding (running chromium and mobile-chrome on separate runners) because:

1. The three longest specs are all genuine K8s round-trip tests -- they are the canonical use case for E2E.
2. The fixed-overhead portion (~2m 20s) is already minimal; further compression requires caching the Playwright browser install or the k3s image, both of which are CI-infrastructure work outside the scope of this refactor.
3. The remaining minor candidates (2 mobile-layout tests in `secrets.spec.ts`, 1 Folders-sidebar test, a few `auth.spec.ts` trims) together would save ~10-15s, not 72s.

**If sub-6-minute E2E becomes a hard requirement**, the highest-leverage follow-up is to shard the Playwright run: split `chromium` and `mobile-chrome` onto two CI jobs that run in parallel. The current run executes them as two sequential `projects` inside one `npx playwright test` invocation, so each contributes ~137s of pure test work to the wall clock. Sharding would bring the Playwright step to ~2m 20s (the larger of the two projects) and the total job to approximately 4m 40s. This is a future optimisation and is explicitly **not** part of HOL-658; record it as a follow-up ticket if the operator decides to pursue it.

### Acceptance Criteria Status

- [x] A timing comparison is recorded in `docs/agents/e2e-refactor-audit.md`: pre-refactor baseline, post-refactor actual, delta, plus the per-spec breakdown from Playwright's reporter output. *(This section.)*
- [x] The result is posted as a comment on HOL-650 so operators can see the payoff without opening the repo. *(Posted by the agent that ran HOL-657.)*
- [x] If total E2E time is still over ~6 minutes, a follow-up section lists the top three longest-running specs with a recommendation. *(See "Long-Pole Analysis" above -- all three are accepted as canonical K8s round-trips; sharding is identified as the only remaining ~3-minute lever.)*
- [x] No code changes in this phase other than the results doc update.
- [x] Tests pass: `make test` (verified locally before the PR).

---

## Release Notes (HOL-658)

HOL-658 finalizes the E2E refactor that ran across HOL-651 through HOL-657. The repo now owns the complete testing strategy end to end (`docs/agents/test-strategy.md`, `docs/agents/testing-patterns.md`, `docs/testing.md`, `docs/e2e-testing.md`, and this audit); every testing-guidance forward-pointer to the external docs repo has been removed. `AGENTS.md` Testing section enumerates the five docs in reader order (Strategy -> Patterns -> Guide -> E2E -> Audit) and calls out the make targets.

### What landed in this phase

- **`frontend/e2e/helpers.ts` slimmed to its public surface.** `TokenExchangeResponse`, `getPersonaToken()`, and `switchPersona()` are now file-internal. The public helpers are `loginAsPersona()`, `apiGrantOrgAccess()`, the four persona email constants, plus the org / project / folder API helpers and `loginViaProfilePage()` / `selectOrg()` used by the K8s round-trip specs.
- **`frontend/e2e/fixtures/` deleted.** The three YAML fixtures (`project-default-sharing.yaml`, `project-namespace.yaml`, `secret-no-time-bounds.yaml`) were only referenced by one-off PR screenshot scripts (`scripts/pr-200/`, `scripts/pr-268/`, `scripts/browser-verify-time-bounds`) for already-merged PRs; those scripts were removed alongside the fixtures.
- **`auth.spec.ts` trimmed** per the audit's per-test notes: the raw-JSON-view test was deleted (covered by `frontend/src/routes/_authenticated/-profile.test.tsx`), and the two token-claim-enumeration tests were minimized to real-Dex smoke assertions (per-claim label coverage lives in the same unit test).
- **`secrets.spec.ts` mobile cleanup.** The redundant `should show hamburger menu and hide sidebar on mobile` test was deleted; the mobile-chrome Playwright project runs every remaining spec at a phone viewport and already exercises responsive layout.
- **`docs/e2e-testing.md` updated** to reflect the narrower public helper API, the revised multi-persona example, and the post-refactor "Which Tests Need Kubernetes" table (`multi-persona.spec.ts` is now K8s-only — the token-endpoint tests moved to Go in HOL-656).
- **`docs/agents/testing-patterns.md` updated** to list only the public helpers (`loginAsPersona`, `apiGrantOrgAccess`).

### Net effect of HOL-650

`E2E Tests` CI job: **~11m 23s -> ~7m 43s (-32%)**. Playwright per-test seconds: **495.1s -> 275.5s (-44%)**. Spec file count: **11 -> 6**. Remaining E2E coverage is focused on OIDC auth (`auth.spec.ts`) and real Kubernetes round-trips (`folders.spec.ts`, `folder-rbac.spec.ts`, `folder-templates.spec.ts`, `secrets.spec.ts`, `multi-persona.spec.ts`).

This repo does not maintain a `CHANGELOG.md`; this Release Notes section in the audit doc is the canonical record of the in-sourced test strategy and E2E refactor per the HOL-658 acceptance criteria.

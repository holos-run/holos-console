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

`frontend/e2e/` originally contained **11 spec files** totalling **1,576 test-lines** across **58 `test(...)` blocks** (plus `helpers.ts` at 250 lines). After HOL-653 landed, `profile.spec.ts` and `navigation.spec.ts` were deleted and their coverage folded into Vitest unit tests; the remaining tables still list them for historical context.

### Summary Table

| Spec | Tests | Verdict | Status |
| --- | --: | --- | --- |
| `auth.spec.ts` | 14 | **Keep** (OIDC canonical E2E) — 13 Keep + 1 Refactor | pending (HOL-658) |
| `profile.spec.ts` | 5 | **Refactor-to-unit** (all 5) | **done (HOL-653)** — file deleted |
| `navigation.spec.ts` | 2 | **Split** → both refactored to unit | **done (HOL-653)** — file deleted |
| `create-dialogs.spec.ts` | 5 | **Refactor-to-unit** (all 5) | pending (HOL-654) |
| `org-settings.spec.ts` | 2 | **Refactor-to-unit** (all 2) | pending (HOL-655) |
| `deployments.spec.ts` | 3 | **Refactor-to-unit** (all 3) | pending (HOL-655) |
| `folders.spec.ts` | 6 | **Keep** (K8s CRUD) | keep |
| `folder-rbac.spec.ts` | 3 | **Keep** (K8s RBAC cascade) | keep |
| `folder-templates.spec.ts` | 2 | **Keep** (K8s template release) | keep |
| `secrets.spec.ts` | 6 | **Split** (4 Keep, 1 Refactor, 1 Delete) | pending (HOL-658) |
| `multi-persona.spec.ts` | 10 | **Split** (4 → Go tests, 2 → unit, 1 Delete, 3 Keep) | pending (HOL-656) |
| **Total** | **58** | **Keep: 32, Refactor: 23, Delete: 3** | HOL-653 complete: 7 tests removed from E2E |

Projected reduction: **~45% of E2E test bodies** leave the E2E suite (26 of 58) — 23 move to unit/Go tests, 3 are deleted as redundant with existing coverage. The remaining E2E job is focused on OIDC auth and real K8s round-trips.

**HOL-653 delta**: After this phase, `frontend/e2e/` holds **9 spec files** and **51 `test(...)` blocks** (5 profile + 2 navigation deleted).

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

### `create-dialogs.spec.ts` — Refactor-to-unit (all 5)

The dialog validation, auto-slug, and reset-affordance behaviours are pure form state. The "picker-menu-item-renders-at-bottom" assertions are also pure UI. The create → navigate-to-secrets case looks like a full-stack round-trip at first glance, but the only thing E2E verifies that a unit test cannot is that the **slug URL the router produces matches the slug the server created** — and that invariant is already covered by `secrets.spec.ts` (which creates a project and then a secret under it). With mocked `useCreateProject` and `useNavigate`, all five cases collapse into pure UI assertions.

**Existing unit targets:** `frontend/src/components/create-project-dialog.test.tsx` (278 lines) and `frontend/src/components/create-org-dialog.test.tsx` (238 lines) — **extend these**, do not create new files.

**Mocks needed:**
- `vi.mock('@/queries/projects', () => ({ useCreateProject: vi.fn(), useListProjects: vi.fn() }))`
- `vi.mock('@/queries/organizations', () => ({ useCreateOrganization: vi.fn(), useListOrganizations: vi.fn() }))`
- `vi.mock('@/lib/auth', () => ({ useAuth: vi.fn() }))`
- Router mock exporting `useNavigate: vi.fn()` so navigation can be asserted.

| Test | Verdict | Target | New unit test(s) to add |
| --- | --- | --- | --- |
| `Create Organization dialog > existing user sees New Organization item at bottom of org picker dropdown` | **Refactor** | `frontend/src/components/app-sidebar.test.tsx` | New: `"org picker menu includes New Organization at bottom when orgs exist"` — mock `useListOrganizations` with one org, open picker, assert `getByRole('menuitem', { name: /new organization/i })` is the last item. |
| `Create Project dialog > org with no projects shows New Project CTA` | **Refactor** | `frontend/src/components/app-sidebar.test.tsx` | New: `"sidebar shows New Project CTA when selected org has zero projects"` — mock `useListProjects` with empty array, assert `queryByTestId('project-picker')` is null and `getByRole('button', { name: /new project/i })` is visible. |
| `Create Project dialog > create project dialog opens, submits via display name auto-slug, and navigates to secrets page` | **Refactor** | `create-project-dialog.test.tsx` | New: `"submits with auto-derived slug and navigates to secrets page"` — fill displayName, click Create, assert `mutateAsync` called with the expected slug, assert `navigate({ to: '/projects/$projectName/secrets', params: { projectName: expectedSlug } })`. |
| `Create Project dialog > create project dialog: manually overriding name stops auto-derivation and shows reset affordance` | **Refactor** | `create-project-dialog.test.tsx` | Already partially covered. Verify the existing test in `create-project-dialog.test.tsx` covers the reset-affordance round-trip; if not, add `"name override disables auto-derivation until reset link is clicked"`. |
| `Create Project dialog > existing user sees New Project item at bottom of project picker dropdown` | **Refactor** | `frontend/src/components/app-sidebar.test.tsx` | New: `"project picker menu includes New Project at bottom when projects exist"` — symmetric to the org-picker test above. |

After the above refactor lands, **delete `create-dialogs.spec.ts` entirely**. This phase lives in **HOL-654**.

### `org-settings.spec.ts` — Refactor-to-unit (all 2)

Both tests are pure sidebar-link-visibility and route-renders-with-org-name. No server round-trip beyond what the standard app-sidebar unit test already covers.

**Targets:**
- Test 1 → `frontend/src/components/app-sidebar.test.tsx`
- Test 2 → `frontend/src/routes/_authenticated/orgs/$orgName/settings/-settings.test.tsx` (already 537 lines covering the settings form itself)

**Mocks needed:**
- `vi.mock('@/queries/organizations', () => ({ useListOrganizations: vi.fn(), useGetOrganization: vi.fn() }))`
- `vi.mock('@/lib/org-context', () => ({ useOrg: vi.fn() }))`

| Test | Verdict | Target | New unit test(s) |
| --- | --- | --- | --- |
| `Org Settings page > settings link appears in sidebar when org is selected` | **Refactor** | `app-sidebar.test.tsx` | New: `"sidebar shows Org Settings link when an org is selected"` — mock `useOrg` with `{ name: 'test-org' }`, render sidebar, assert link visible. |
| `Org Settings page > clicking Settings in sidebar navigates to settings page` | **Refactor** | `-settings.test.tsx` | Not a new unit test — the existing `-settings.test.tsx` already covers the page rendering. The navigation portion is redundant with router mocks; **drop** the click-assertion and rely on the existing test's render proof. |

**Delete entire `org-settings.spec.ts`.** This phase lives in **HOL-655**.

### `deployments.spec.ts` — Refactor-to-unit (all 3)

The no-templates affordance is a pure UI branch on `useListTemplates({ scope: 'PROJECT' })` returning empty. The "clicking Create Deployment navigates to new page" test is pure router behaviour. The "has templates → shows submit button" test is the mirror of the empty case.

**Target:** `frontend/src/routes/_authenticated/projects/$projectName/deployments/-new.test.tsx` (already exists and covers form fields). Extend it with the three affordance cases.

**Mocks needed:**
- `vi.mock('@/queries/templates', () => ({ useListTemplates: vi.fn(), useCreateDeployment: vi.fn() }))`
- `vi.mock('@/queries/deployments', () => ({ useListDeployments: vi.fn(), useCreateDeployment: vi.fn() }))`
- `vi.mock('@/lib/auth', () => ({ useAuth: vi.fn() }))`

| Test | Verdict | Target | New unit test |
| --- | --- | --- | --- |
| `Create Deployment page — no-templates affordance > shows "No templates available..." when no templates exist` | **Refactor** | `-new.test.tsx` | New: `"shows no-templates affordance and create-a-template link when template list is empty"` — mock `useListTemplates` returning `data: []`, assert `getByText(/no templates available/i)` and `getByRole('link', { name: /create a template/i })`. |
| `... > does not show no-templates affordance when templates exist` | **Refactor** | `-new.test.tsx` | New: `"hides no-templates affordance when templates are available"` — mock `useListTemplates` with one template, assert `queryByText(/no templates available/i)` is null and the Create Deployment submit button is enabled. |
| `... > clicking "Create Deployment" link on list page navigates to new page` | **Refactor** | `frontend/src/routes/_authenticated/projects/$projectName/deployments/-index.test.tsx` | New: `"Create Deployment link points to the /new route"` — assert the `<Link>` component's `to` prop resolves to `deployments/new` (no router round-trip needed). |

**Delete entire `deployments.spec.ts`.** This phase lives in **HOL-655**.

### `folders.spec.ts` — Keep (all 5, require K8s)

Every test in this spec creates real Kubernetes namespaces (folder-backed orgs), exercises the hierarchy-list API, and asserts the DOM reflects what the cluster returned. Deletion would lose coverage of the full-stack folder CRUD flow.

| Test | Verdict | Notes |
| --- | --- | --- |
| `Folder list page > shows folders under an org and navigates to folder detail` | **Keep** | K8s namespace create + list. |
| `Folder list page > new org has default folder` | **Keep** | Verifies server-side default-folder creation in Kubernetes. |
| `Folder detail page > shows folder name and organization` | **Keep** | GET-on-folder CRUD path. |
| `Nested folder workflow > creates org → parent folder → child folder, both visible in list` | **Keep** | Parent-child K8s hierarchy. |
| `Nested folder workflow > project under folder shows in folder breadcrumb context` | **Keep** | Cross-resource K8s relationship. |
| `Sidebar Folders navigation > org nav section includes Folders link` | **Refactor candidate (low priority)** | This one *could* move to `app-sidebar.test.tsx`, but the API-create-org prerequisite makes the unit version non-trivial; leave in E2E unless a future cleanup phase targets it. |

### `folder-rbac.spec.ts` — Keep (all 3, require K8s RBAC cascade)

Folder RBAC metadata is written to real Kubernetes namespace annotations. The cascade is already unit-tested in `console/rbac/`, but the end-to-end wiring (HTTP → handler → K8s annotations → UI delete button visibility) only exists here.

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

### `multi-persona.spec.ts` — Split (4 → Go tests, 3 → unit, 3 Keep)

The first four tests call `POST /api/dev/token` and assert the response shape — they do **not** need a browser. Move them to Go tests against the `HandleTokenExchange` handler in `console/oidc/token_exchange_test.go` (which already contains similar unit tests via `httptest`). The three persona-switching tests exercise UI email display; the three RBAC grant tests require K8s.

**Go target:** `console/oidc/token_exchange_test.go` (already has `TestHandleTokenExchange_Success`, etc.). Extend it.

**Unit target for persona-switch display:** `frontend/src/routes/_authenticated/-profile.test.tsx` — add per-email rendering tests that mock `useAuth` with the persona's email.

| Test | Verdict | Target | Notes |
| --- | --- | --- | --- |
| `Dev Token Endpoint > should return a valid token for the platform engineer persona` | **Refactor to Go** | `console/oidc/token_exchange_test.go` | New: `TestHandleTokenExchange_PlatformEngineer` — POST to the handler with `platform@localhost`, assert response includes `id_token`, `email`, `groups: ["owner"]`, `expires_in > 0`. |
| `Dev Token Endpoint > should return a valid token for the product engineer persona` | **Refactor to Go** | `console/oidc/token_exchange_test.go` | New: `TestHandleTokenExchange_ProductEngineer` — symmetric, `groups: ["editor"]`. |
| `Dev Token Endpoint > should return a valid token for the SRE persona` | **Refactor to Go** | `console/oidc/token_exchange_test.go` | New: `TestHandleTokenExchange_SRE` — symmetric, `groups: ["viewer"]`. |
| `Dev Token Endpoint > should reject unknown email addresses` | **Refactor to Go** | `console/oidc/token_exchange_test.go` | New: `TestHandleTokenExchange_UnknownEmail` — POST with `unknown@example.com`, assert status 400 and body contains `"unknown test user email"`. |
| `Persona Switching > should login as platform engineer and show correct email` | **Refactor-to-unit** | `-profile.test.tsx` | New: `"shows platform engineer email when useAuth returns platform profile"` — mock `useAuth` with `email: 'platform@localhost'`, assert visible. |
| `Persona Switching > should switch from admin to SRE persona` | **Refactor-to-unit** | `-profile.test.tsx` | New: combined render test that rerenders with two different `useAuth` return values and asserts the email updates. Note: this is a shallow equivalence to the browser-based test; the sessionStorage/reload mechanics don't need coverage because they're covered by the auth-layout and transport unit tests (`-_authenticated.test.tsx`, `transport.test.ts`). |
| `Persona Switching > should switch between all three non-admin personas` | **Delete (redundant)** | — | A single cycle-through-emails unit test covers the same logic as the three-step browser test. Skip this one. |
| `Multi-Persona RBAC > platform engineer can create an org and grant SRE viewer access` | **Keep** | — | K8s org create + RBAC grant. |
| `Multi-Persona RBAC > SRE can list the org after being granted viewer access` | **Keep** | — | K8s list with persona-scoped RBAC. |
| `Multi-Persona RBAC > product engineer can access the org with editor privileges` | **Keep** | — | K8s list with editor-scoped RBAC. |

After the refactor:
- Delete the `Dev Token Endpoint` describe (4 tests moved to Go).
- Delete the `Persona Switching` describe (2 refactored to unit, 1 deleted).
- Keep the `Multi-Persona RBAC` describe intact.

**Result: `multi-persona.spec.ts` shrinks from 9 tests to 3.** This phase lives in **HOL-656**.

---

## Phase Assignments

| Phase | Tickets | Scope |
| --- | --- | --- |
| HOL-653 | profile.spec.ts (5), navigation.spec.ts (1) | 6 tests → Vitest |
| HOL-654 | create-dialogs.spec.ts (5) | 5 tests → Vitest |
| HOL-655 | deployments.spec.ts (3), org-settings.spec.ts (2) | 5 tests → Vitest |
| HOL-656 | multi-persona.spec.ts Dev Token (4), Persona Switching (3) | 4 → Go, 3 → Vitest |
| HOL-657 | — | Measure E2E CI wall-clock after the four refactor phases; compare against the 11m 23s baseline. |
| HOL-658 | auth.spec.ts trims, helpers.ts cleanup, mobile consolidation | Remove dead helpers, trim auth-spec overlaps, decide on mobile responsive tests. |

## Notes for Implementers

- **Always extend existing route-directory test files** (`-profile.test.tsx`, `-settings.test.tsx`, `-new.test.tsx`, etc.) rather than creating new ones. The existing files already set up the necessary mocks and router stubs; adding a test body is a ~20-line change, creating a new file is a ~100-line change.
- **Delete the refactored E2E tests in the same PR that adds the unit coverage.** Dead E2E code continues to run in CI and contributes to the 11-minute runtime; leaving "just in case" defeats the purpose.
- **Preserve the E2E mobile-chrome project** even after deleting the two mobile-only tests — it runs every remaining spec at a phone viewport and catches responsive regressions for free.
- **Do not add E2E tests in the replacement PRs.** If a behaviour needs verification and doesn't fit the Keep criteria (OIDC or K8s round-trip), it belongs in Vitest. The whole point of this refactor is to reverse the creep that pushed E2E from 4 minutes to 11 minutes.
- **Verify with `make test` before each phase lands.** E2E is not required for the refactor phases (HOL-653 through HOL-656) because they delete E2E tests and add unit tests; `make test-ui` + `make test-go` are the relevant gates.

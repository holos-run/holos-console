# ADR 023: Multi-Persona Test Identities and Dev Tools

## Status

Accepted

## Context

Prior to this change, the embedded Dex OIDC provider configured a single
auto-login connector that authenticated every user as `admin@localhost` with
the `owner` group. This design was sufficient for single-user development but
made it impossible to test RBAC behavior across different roles without an
external identity provider.

Effective RBAC testing requires multiple identities with distinct group
memberships so that Viewer, Editor, and Owner permission boundaries can be
verified in E2E tests and during interactive development. The solution must:

1. Preserve the zero-credential auto-login experience for the common case
   (developer starts `make run` and immediately has full access).
2. Provide additional identities with different RBAC roles.
3. Allow programmatic token acquisition for E2E test helpers and CLI scripts.
4. Allow interactive persona switching in the browser without a full Dex
   redirect round-trip.
5. Keep all dev-only surface area gated behind explicit opt-in flags so it
   cannot leak into production.

## Decisions

### Decision 1: Static test users registered in embedded Dex

Four test users are defined as a Go-level constant table (`TestUsers` in
`console/oidc/config.go`). Each user has a unique email, UserID,
DisplayName, and group membership:

| Persona | Email | Groups | RBAC Role |
|---------|-------|--------|-----------|
| Admin (default) | `admin@localhost` | `["owner"]` | OWNER |
| Platform Engineer | `platform@localhost` | `["owner"]` | OWNER |
| Product Engineer | `product@localhost` | `["editor"]` | EDITOR |
| SRE | `sre@localhost` | `["viewer"]` | VIEWER |

All four users share the password `verysecret` (the `DefaultPassword`
constant). The admin user is the identity produced by the auto-login
connector; the other three represent the three RBAC tiers.

**Why static, in-code registration (not config files or environment
variables)?** Test personas must be deterministic and reproducible.
Externalizing them would add configuration surface area without benefit --
these are development-only identities whose values are fixed by design.

**Why four users when there are only three RBAC roles?** The admin user
is retained for backwards compatibility with the auto-login connector.
The three additional personas (Platform Engineer, Product Engineer, SRE)
map one-to-one to the Owner, Editor, Viewer roles, making test assertions
unambiguous.

### Decision 2: Dev token-exchange endpoint using Dex signing keys (not ROPC)

A `POST /api/dev/token` endpoint accepts `{"email": "<user>"}` and returns
a signed OIDC ID token for any registered test user. The endpoint is gated
behind `--enable-insecure-dex` (returns 404 when Dex is not running).

The token is minted directly using Dex's signing keys retrieved from the
in-memory Dex storage (`DexState.Storage.GetKeys`). This produces tokens
that are structurally identical to those Dex issues through the standard
OIDC authorization code flow -- same issuer, audience, signing algorithm,
and claim structure.

**Why direct signing rather than ROPC (Resource Owner Password Credentials)?**
ROPC would require registering password connectors in Dex for each test user.
Dex shows a connector selection page when multiple connectors are registered,
which breaks the auto-login experience (Decision 1 requires that the default
`make run` flow authenticates without user interaction). Direct signing avoids
this conflict entirely: no additional connectors are registered, the auto-login
connector remains the only connector, and the token endpoint mints tokens
independently.

The `DexState` struct exposes the Dex `Storage`, `Issuer`, and `ClientID`
fields so the token exchange handler can construct valid tokens without
coupling to Dex's internal token issuance pipeline.

### Decision 3: Separate `--enable-dev-tools` flag

A new `--enable-dev-tools` CLI flag (default `false`) controls whether the
frontend Dev Tools UI is available. When enabled, the server injects a
`window.__CONSOLE_CONFIG__` script tag into the HTML with
`{"devToolsEnabled": true}`. The frontend reads this via `getConsoleConfig()`
and conditionally renders the Dev Tools sidebar item and `/dev-tools` route.

**Why a separate flag rather than bundling with `--enable-insecure-dex`?**
The two flags control different security surfaces:

- `--enable-insecure-dex` controls whether the embedded OIDC provider runs
  and the dev token endpoint is available. This is a backend concern.
- `--enable-dev-tools` controls whether the frontend exposes UI that allows
  persona switching. This is a UI surface area concern.

An operator might want the embedded Dex provider (for convenient local
development) without the persona switcher UI (to avoid confusion during
demos). Keeping the flags independent preserves this flexibility.

`make run` passes both flags. E2E tests pass `--enable-insecure-dex` (for
token acquisition) but do not require `--enable-dev-tools` since tests use
programmatic token injection, not the UI switcher.

### Decision 4: Frontend persona switcher via token injection

The Dev Tools page (`/dev-tools`) displays three persona cards (Platform
Engineer, Product Engineer, SRE). Clicking a persona:

1. Calls `POST /api/dev/token` with the persona's email.
2. Constructs an oidc-client-ts `User` object from the response.
3. Writes it to `sessionStorage` under the `oidc.user:{authority}:{client_id}`
   key.
4. Reloads the page so the auth provider picks up the new identity.

**Why token injection into sessionStorage rather than a full Dex redirect?**
A Dex redirect flow would require the user to navigate to Dex, select a
connector (if multiple are registered), enter credentials, and wait for the
callback. Token injection is instant and preserves the current page state
(after reload). It also avoids the connector selection problem described in
Decision 2.

The persona switcher reuses the same token exchange endpoint that E2E test
helpers use, ensuring consistency between interactive and automated workflows.

### Decision 5: Three personas mapping to RBAC tiers

The UI persona switcher exposes three personas (excluding the admin user):

| Persona | Email | Groups | Role Badge |
|---------|-------|--------|------------|
| Platform Engineer | `platform@localhost` | `["owner"]` | Owner |
| Product Engineer | `product@localhost` | `["editor"]` | Editor |
| SRE | `sre@localhost` | `["viewer"]` | Viewer |

**Why exclude admin from the switcher?** The admin user is the auto-login
identity -- the user is already authenticated as admin when they open Dev
Tools. Including admin in the switcher would add a fourth option with the
same permissions as Platform Engineer (both are `owner` group), creating
confusion without testing value.

**Why these persona names?** They represent the three primary user archetypes
of the Holos Console: the platform team that manages infrastructure (owner
access), the product team that deploys applications (editor access), and
the SRE team that monitors and observes (viewer access).

## Consequences

### Positive

- **RBAC testing without external providers.** Developers and CI can verify
  all three permission tiers using only `make run`.

- **E2E test isolation.** Each test can authenticate as a specific persona
  without shared mutable state. The `loginAsPersona()`, `switchPersona()`,
  and `getPersonaToken()` helpers in `frontend/e2e/helpers.ts` provide a
  clean API for multi-persona test scenarios.

- **Interactive debugging.** The Dev Tools persona switcher lets developers
  quickly see how the UI behaves for different roles without restarting the
  server or using browser incognito windows.

- **Auto-login preserved.** The default `make run` experience is unchanged:
  `admin@localhost` is auto-logged in with owner permissions.

### Negative

- **Four hardcoded test users.** Adding a new persona requires a code change
  in `console/oidc/config.go` and corresponding updates to the frontend
  persona definitions in `frontend/src/lib/dev-tools.ts`.

- **Token endpoint bypasses Dex's authorization flow.** The direct-signing
  approach means the dev token endpoint does not exercise Dex's token
  issuance pipeline. This is acceptable because the endpoint is for testing,
  not for production authentication.

- **Separate flag burden.** Operators running local development must pass
  both `--enable-insecure-dex` and `--enable-dev-tools` for the full
  experience. `make run` handles this automatically.

## References

- #695: Parent tracking issue (multi-persona test identities with dev tools)
- #696: Static test users in embedded Dex
- #697: Dev token-exchange endpoint
- #698: `--enable-dev-tools` flag and config injection
- #699: Dev Tools UI with persona switcher
- #700: E2E test helpers for multi-persona
- ADR 008: `--enable-insecure-dex` flag
- ADR 010: `/api/debug/oidc` endpoint risk acceptance

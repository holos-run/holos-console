# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

This code is not yet released. Do not preserve backwards compatibility when making changes.

Before making changes, review `CONTRIBUTING.md` for commit message requirements.

## Before Committing

**IMPORTANT:** Always run `make generate` before committing changes. This command:
1. Regenerates protobuf code (Go and TypeScript)
2. Rebuilds the UI (runs `npm run build` which includes TypeScript type checking)

If `make generate` fails, fix the errors before committing. Common issues:
- TypeScript type errors in test mocks (cast mock responses with `as unknown as ...`)
- Missing protobuf imports after adding new message types

## Implementing Plans

Plans are recorded as GitHub issues. Implement each plan on a feature branch with regular commits in a single PR that references the issue.

1. **Create a feature branch** from `main` for the plan.
2. **Make regular commits** as you work. Each commit should be a logical unit of change.
3. **Open a PR** when the work is complete. Include `Closes: #NN` (where NN is the issue number) in the PR description so the issue is automatically closed when the PR is merged.
4. **Loop on PR checks**: after pushing, watch CI checks (`gh pr checks <N> --watch`) and fix any failures. Iterate until all checks pass.
5. **Merge via merge commit** once all checks pass: `gh pr merge <N> --merge`. Do not squash or rebase — the project uses merge commits so that commit SHAs referenced in screenshot URLs remain reachable in `main` history.

### Test Strategy

**Prefer unit tests over E2E tests.** Rendering, interaction, navigation logic, and ConnectRPC data shaping all belong in unit tests using mocked query hooks. Reserve E2E tests for:
- The OIDC login flow (requires a real Dex server)
- Full-stack CRUD round-trips that verify server-side behavior (requires a real Kubernetes cluster)

When a behavior can be verified with a unit test, write a unit test. Do not add an E2E test for the same behavior.

### Identifying Your Agent Slot

Agents run in worktrees whose path encodes the agent slot. Identify your slot from your working directory — for example, if `pwd` is `/path/to/worktrees/holos-run/agent-2/holos-console`, your slot is `agent-2`.

**Issue title**: Prepend the slot to issue titles created by the agent so they show up clearly in `gh issue list`, e.g. `[agent-2] feat: add Playwright E2E test infrastructure`.

**PR title**: Use a conventional commit prefix (`feat:`, `fix:`, `docs:`, `build:`, `refactor:`, `test:`). Do **not** include the agent slot — strip it if working from an issue title that contains one.

**PR description**: Include the slot in the footer so reviewers know which agent produced the work.

**PR comment**: After creating the PR, post a comment explaining the rationale and motivation for the implementation approach, including alternatives considered and why they were rejected.

Example workflow:
```bash
git checkout -b feat/add-playwright-config
# ... implement changes, committing as you go ...
git commit -m "Add webServer configuration to playwright.config.ts

Configure Playwright to automatically start Go backend and Vite dev
server before running E2E tests."

# Determine agent slot from working directory
SLOT=$(pwd | grep -oP 'agent-\d+' || echo "agent-0")

# Open a PR that closes the plan issue
# Note: PR title uses conventional commit format — no agent slot prefix
gh pr create --title "feat: add Playwright E2E test infrastructure" --body "$(cat <<'EOF'
## Summary
- Configure Playwright to start Go backend and Vite dev server
- Add E2E test for the login flow

Closes: #42

## Test plan
- [ ] `make test-e2e` passes

🤖 Generated with [Claude Code](https://claude.com/claude-code) · ${SLOT}
EOF
)"

# Add a comment explaining rationale and alternatives
gh pr comment <N> --body "$(cat <<'EOF'
## Rationale

Explain why this approach was chosen over alternatives...

## Alternatives considered

| Option | Verdict |
|--------|---------|
| Alternative A | Rejected because... |
EOF
)"
```

## Build Commands

```bash
make build          # Build executable to bin/holos-console
make debug          # Build with debug symbols to bin/holos-console-debug
make test           # Run all tests (Go + UI unit tests)
make test-go        # Run Go tests with race detector
make test-ui        # Run UI unit tests (one-shot)
make test-e2e       # Run E2E tests (builds binary, starts servers, runs Playwright)
make generate       # Run go generate (regenerates protobuf code + builds UI)
make tools          # Install pinned tool dependencies (buf)
make certs          # Generate TLS certificates with mkcert (one-time setup)
make run            # Build and run server with generated certificates
make dev            # Start Vite dev server with hot reload (use alongside make run)
make dispatch ISSUE=N  # Dispatch a plan issue to a Claude Code agent in a new worktree
make agent-tools    # Install agent-browser for browser automation
make cluster        # Create local k3d cluster (DNS + cluster + CA)
make bump-major     # Bump major version (resets minor and patch to 0)
make bump-minor     # Bump minor version (resets patch to 0)
make bump-patch     # Bump patch version
make tag            # Create annotated git tag from version files (never use git tag directly)
make fmt            # Format code
make vet            # Run go vet
make lint           # Run golangci-lint
make coverage       # Generate HTML coverage report
```

### Running Single Tests

```bash
# Go: single test by name
go test -v -run TestNewHandler_Success ./console/oidc

# UI unit: by file or test name
cd frontend && npm test -- SecretPage
cd frontend && npm test -- -t "displays error message"

# E2E: by test name
cd frontend && npx playwright test --grep "should complete full login flow"
```

## Architecture

This is a Go HTTPS server that serves a web console UI and exposes ConnectRPC services. The built UI is embedded into the Go binary via `//go:embed` for single-binary deployment.

### Package Structure

- `cmd/` - Main entrypoint, calls into cli package
- `cli/` - Cobra CLI setup with Cobra flags for listen addr, TLS, OIDC, RBAC, logging config
- `console/` - Core server package
  - `console.go` - HTTP server setup, TLS, route registration, embedded UI serving
  - `version.go` - Version info with embedded version files and ldflags
  - `rpc/` - ConnectRPC handler implementations and auth interceptor
  - `oidc/` - Embedded Dex OIDC provider
  - `organizations/` - OrganizationService with K8s Namespace backend and annotation-based grants
  - `projects/` - ProjectService with K8s Namespace backend and annotation-based grants
  - `resolver/` - Namespace prefix resolver translating user-facing names to K8s namespace names (`{namespace-prefix}{organization-prefix}{name}` for orgs, `{namespace-prefix}{project-prefix}{name}` for projects)
  - `secrets/` - SecretsService with K8s backend and annotation-based RBAC
  - `settings/` - ProjectSettingsService managing per-project feature flags (e.g. deployments toggle) stored as annotations on the project Namespace; deployments toggle requires org-level OWNER via `PERMISSION_PROJECT_DEPLOYMENTS_ENABLE`
  - `templates/` - DeploymentTemplateService managing CUE-based deployment templates stored as K8s ConfigMaps; embeds `default_template.cue`. Templates use the structured `output.namespacedResources`/`output.clusterResources` output format (see `docs/cue-template-guide.md`). Templates can carry `DeploymentDefaults` (image, tag, command, args, env, port) that pre-fill the Create Deployment form. The `RenderDeploymentTemplate` RPC returns rendered resources as both YAML (`rendered_yaml`) and JSON (`rendered_json`). The default template adds a `console.holos.run/deployer-email` annotation to all resources from `system.claims.email`.
  - `system_templates/` - SystemTemplateService managing org-scoped CUE templates stored as K8s ConfigMaps in the org namespace. System templates differ from deployment templates in that they can be marked mandatory, causing them to be automatically applied to every project namespace at creation time. Includes `MandatoryTemplateApplier` which is called by the projects service after a project namespace is created. Embeds `default_referencegrant.cue` as the built-in mandatory template that creates a `ReferenceGrant` allowing HTTPRoute resources in project namespaces to reference the gateway. Edit access requires `PERMISSION_SYSTEM_DEPLOYMENTS_EDIT` (org-level OWNER only via `OrgCascadeSystemTemplatePerms`).
  - `deployments/` - DeploymentService managing Kubernetes Deployments: CRUD, status polling, log streaming, CUE render and apply (structured `output.namespacedResources`/`output.clusterResources` output), container command/args override, container env vars (literal values, SecretKeyRef, ConfigMapKeyRef), container port configuration, listing project-namespace Secrets/ConfigMaps for env var references, and `GetDeploymentRenderPreview` (returns the CUE template, system input, user input, rendered YAML, and rendered JSON for a live deployment). CUE render uses split `SystemInput` (project, namespace, claims) and `UserInput` (name, image, tag, etc.) — see `docs/cue-template-guide.md`.
  - `dist/` - Embedded static files served at `/` (build output from frontend, not source)
- `proto/` - Protobuf source files
  - `holos/console/v1/organizations.proto` - OrganizationService
  - `holos/console/v1/projects.proto` - ProjectService
  - `holos/console/v1/secrets.proto` - SecretsService
  - `holos/console/v1/project_settings.proto` - ProjectSettingsService
  - `holos/console/v1/deployment_templates.proto` - DeploymentTemplateService
  - `holos/console/v1/deployments.proto` - DeploymentService
  - `holos/console/v1/system_templates.proto` - SystemTemplateService
  - `holos/console/v1/rbac.proto` - Role definitions (VIEWER, EDITOR, OWNER)
  - `holos/console/v1/version.proto` - VersionService
- `gen/` - Generated protobuf Go code (do not edit)
- `frontend/` - React frontend source (see UI Architecture below)

### UI Architecture

React 19 + TypeScript + Vite 7 app in the `frontend/` directory.

- **UI Library**: shadcn/ui with Tailwind CSS v4
- **Routing**: TanStack Router with file-based routing (serves at `/`)
- **Server state**: TanStack Query v5 with ConnectRPC Query integration (`@connectrpc/connect-query`)
- **Auth**: oidc-client-ts library with OIDC PKCE flow
- **Generated types**: `frontend/src/gen/` contains TypeScript protobuf types generated by buf

Key files:
- `frontend/src/main.tsx` - Entry point; mounts RouterProvider
- `frontend/src/routes/__root.tsx` - Root layout with TransportProvider, QueryClientProvider, AuthProvider, and sidebar
- `frontend/src/routes/_authenticated.tsx` - Auth gate with OIDC redirect
- `frontend/src/lib/auth/` - Auth context (AuthProvider, useAuth, userManager) with OIDC PKCE flow
- `frontend/src/lib/transport.ts` - ConnectRPC transport with Bearer token attachment, 401 renewal, and retry
- `frontend/src/lib/query-client.ts` - QueryClient with retry suppression for Unauthenticated errors
- `frontend/src/routes/` - File-based route definitions

### UI Styling Conventions

The application uses a dark-only theme (ADR 011). All new UI elements must follow the conventions in `docs/ui-styling-guide.md`, which covers:

- Color palette usage — always use semantic CSS token classes (e.g., `bg-background`, `text-foreground`, `border-border`); never hardcode colors
- Scrollbar styling — global CSS in `frontend/src/app.css` handles scrollbars; new scrollable elements need only `overflow-auto` or `overflow-y-auto`
- Component selection — prefer shadcn/ui components in `frontend/src/components/ui/`
- Typography — `font-mono` for code/CUE content, default sans-serif for UI text
- Spacing — Tailwind default scale; `gap-*` for flex/grid layouts
- Form patterns — Display Name + slug, `Label` + `Input`/`Textarea` pairs
- Dialog/modal patterns — shadcn `Dialog`, `max-w-2xl` for forms, `DialogHeader` + `DialogFooter`
- Toast notifications — `sonner` with `theme="dark"` (already configured at root)

The guide also includes a "Before adding new UI elements" checklist. Consult it before writing any new component.

### Embedded Services

External dependencies (OIDC, NATS) are embedded in the console binary for dev mode and single-replica deployments. The approach: configure as close to production as possible, optimize for developer experience (no passwords, no sidecars), and support full automation. `make run` starts a complete system with zero external infrastructure.

See `docs/embedded-services.md` for the full pattern, lifecycle conventions, and instructions for adding new embedded services.

Current embedded services:
- **Dex** (`console/oidc/`) — OIDC provider, enabled via `--enable-insecure-dex`
- **NATS JetStream** (`console/nats/`, planned) — event backbone, enabled via `--enable-embedded-nats`

### Authentication

OIDC PKCE flow: Requires `--enable-insecure-dex` flag for embedded Dex at `/dex/`, or an external OIDC provider configured with `--issuer`. Tokens stored in session storage, sent as `Authorization: Bearer` headers. Default credentials: `admin` / `verysecret` (override with `HOLOS_DEX_INITIAL_ADMIN_USERNAME`/`PASSWORD` env vars).

Backend auth: `LazyAuthInterceptor` in `console/rpc/auth.go` verifies JWTs from the `Authorization: Bearer` header and stores `rpc.Claims` in context. Lazy initialization avoids startup race with embedded Dex.

### RBAC

Three-tier access control model evaluated in order (highest role wins):

1. **Organization-level**: Per-org grants stored as JSON annotations on K8s Namespace objects (prefix configurable via `--organization-prefix`, default `org-`)
2. **Project-level**: Per-project grants stored as JSON annotations on K8s Namespace objects (prefix configurable via `--project-prefix`, default `prj-`)
3. **Secret-level**: Per-secret grants stored as JSON annotations on K8s Secret objects

Grant annotations: `console.holos.run/share-users`, `console.holos.run/share-roles`

Metadata annotations on org/project namespaces: `console.holos.run/display-name`, `console.holos.run/creator-email` (email of the user who created the resource, written at creation time from the OIDC email claim)

Namespace prefix scheme (three-part naming: `{namespace-prefix}{type-prefix}{name}`):
- Organizations: `{namespace-prefix}{organization-prefix}{name}` (resource-type label: `organization`)
- Projects: `{namespace-prefix}{project-prefix}{name}` (resource-type label: `project`, optional organization label for IAM inheritance, project label stores project name)

The `--namespace-prefix` flag (default `"holos-"`) prefixes all console-managed namespace names, enabling multi-instance isolation in the same cluster (e.g., `prod-org-acme`, `ci-prj-api`).

Organization creation is controlled by `--disable-org-creation`, `--org-creator-users`, and `--org-creator-roles` CLI flags. By default all authenticated principals can create organizations (implicit grant). Setting `--disable-org-creation` disables this implicit grant; explicit `--org-creator-users` and `--org-creator-roles` lists are still honored.

The `--roles-claim` flag (default `"groups"`) configures which OIDC token claim is used to extract role memberships. This allows integration with identity providers that use non-standard claim names (e.g., `realm_roles`).

Roles: VIEWER (1), EDITOR (2), OWNER (3) defined in `proto/holos/console/v1/rbac.proto`

`PERMISSION_SYSTEM_DEPLOYMENTS_EDIT` is required to create, update, or delete system templates. It is granted only to org-level OWNERs via the `OrgCascadeSystemTemplatePerms` cascade table in `console/rbac/rbac.go`.

### Code Generation

Protobuf code is generated using buf. The `generate.go` file contains the `//go:generate buf generate` directive. After modifying `.proto` files in `proto/`, run:

```bash
make generate   # or: go generate ./...
```

This produces:
- `gen/**/*.pb.go` - Go structs for messages
- `gen/**/consolev1connect/*.connect.go` - ConnectRPC client/server bindings
- `frontend/src/gen/**/*_pb.ts` - TypeScript message classes and service definitions (protobuf-es v2)

### Adding New RPCs

1. Define RPC and messages in `proto/holos/console/v1/*.proto`
2. Run `make generate`
3. Implement handler method in `console/rpc/` (embed `Unimplemented*Handler` for forward compatibility)
4. Handler is auto-wired when service is registered in `console/console.go`

See `docs/rpc-service-definitions.md` for detailed examples. See `docs/permissions-guide.md` for permission design guidelines including narrow scoping, multi-level grantability, and the cascade table pattern.

### Template Field Guardrail

**When adding new fields to `CreateDeploymentRequest`, `DeploymentDefaults`, or related template proto messages**, the field must also be:

1. Added to the CUE `#Input` (user-provided fields) or `#System` (system fields) schema in `console/templates/default_template.cue`
2. Included in the Go `UserInput` or `SystemInput` struct in `console/deployments/render.go`
3. Reflected in the template editor preview's User Input or System Input default values in the frontend (see `frontend/src/routes/`)

This ensures template authors always see new fields in the preview and that the CUE schema stays in sync with the proto interface. See `docs/cue-template-guide.md` for the full template interface.

### Template Doc Completeness Guardrail

**When making any of the following changes**, verify that the "Writing a Custom Template" section of `docs/cue-template-guide.md` remains end-to-end complete for a product engineer trying to deploy a web service:

Triggers:
1. Adding or removing kinds from the allowed kinds list in `console/deployments/render.go` or `apply.go`.
2. Modifying `console/templates/default_template.cue` (the default resource set).
3. Editing any section of `docs/cue-template-guide.md`.

After any of the above, confirm the "Writing a Custom Template" section still includes:
- A complete working template with `ServiceAccount`, `Deployment`, and `Service`.
- An explanation of the port flow: `input.port` → container `containerPort` → Service `targetPort` → HTTPRoute (optional).
- Guidance on `HTTPRoute`: when to add one (external access) versus relying on the Service ClusterIP (cluster-internal), with a minimal CUE example.

If any of the above is missing or stale after your changes, update the doc as part of the same commit.

### Testing Patterns

**Preference: unit tests first, E2E only for full-stack behaviours.**  
Rendering, interaction, navigation logic, and ConnectRPC data shaping belong in unit tests with mocked query hooks. Reserve E2E for the OIDC login flow and real Kubernetes CRUD round-trips.

See `docs/testing.md` for the complete decision rule, the ConnectRPC mock pattern with a worked example, file-naming conventions for route-directory test files, and a table of all existing test files.

**Go tests**: Standard `*_test.go` files with table-driven tests. Uses `k8s.io/client-go/kubernetes/fake` for K8s operations. CLI integration tests use `testscript` in `console/testscript_test.go`.

**UI unit tests**: Vitest + React Testing Library + jsdom. Mock query hooks (`@/queries/*`) with `vi.mock()` and `vi.fn()`. Route-directory test files must be prefixed with `-` (e.g. `-about.test.tsx`) so TanStack Router's generator ignores them. Run with `make test-ui`.

**E2E tests**: Playwright in `frontend/e2e/`. `make test-e2e` orchestrates the full stack (builds Go binary, starts Go backend on :8443 and Vite on :5173). For tight iteration, start servers once and run targeted tests — see `docs/e2e-testing.md` for the full workflow including K8s-backed tests.

See `docs/frontend-patterns.md` for common UI patterns (copy-to-clipboard, toast notifications).

### Version Management

Version is determined by:
1. `console/version/{major,minor,patch}` files (embedded at compile time)
2. `GitDescribe` ldflags override (set by Makefile during build)

Build metadata (commit, tree state, date) injected via ldflags in Makefile.

See `docs/versioning.md` for the complete versioning workflow including bump and tag procedures.

### Container Builds

Trigger container image builds using the `container.yaml` GitHub workflow. The workflow runs from `main` and accepts a `git_ref` input specifying what to check out and build:

```bash
gh workflow run container.yaml --ref main -f git_ref=refs/heads/<branch-name>
gh workflow run container.yaml --ref main -f git_ref=refs/tags/v1.2.3
```

### Tool Dependencies

Tool versions are pinned in `tools.go` using the Go tools pattern. Install with `make tools`. Currently pins: buf.

CUE is used at runtime (not as a pinned tool) by the `console/templates/` package to parse and validate deployment template source. The `cuelang.org/go` module is a regular Go dependency listed in `go.mod`. See `docs/cue-template-guide.md` for the full template interface, including the structured `output.namespacedResources`/`output.clusterResources` output format (ADR 012).

## Planning and Execution

### Feature Planning

Plan features using phases. Record plans as GitHub issues before execution using `gh issue create`.

### Dispatching Plans to Agents

After drafting a plan as a GitHub issue, dispatch it to a Claude Code agent
in a new worktree:

    scripts/dispatch <issue-number>

This creates a git worktree at ../holos-console-<N>, opens a new tmux window
named i<N>, and starts a Claude Code agent that reads the issue and implements
the plan. The script returns immediately so the main agent can continue planning.

Prerequisite: must be run inside a tmux session.

### RED GREEN Implementation

Implement each phase using a RED GREEN approach:

1. **RED** - Write failing tests first that define the expected behavior.
2. **GREEN** - Write the minimum implementation to make the tests pass.

### Final Cleanup Phase

Every plan must include a final phase to scan the entire repository for dead, deprecated, or outdated information introduced or made stale by the commits implementing the plan. This includes removing obsolete comments, unused imports, stale documentation, dead code paths, and outdated references in AGENTS.md, README files, and doc files. Commit cleanup changes separately with a clear message explaining what was removed and why.

### Tracking Progress

When executing plans, record progress by checking off TODO items in the relevant GitHub issue using `gh issue edit` or the API. Keep issues up to date as each phase completes. When the PR is merged, the `Closes: #NN` line in the PR description automatically closes the issue.

## Browser Automation (agent-browser)

Coding agents can interact with the running console UI via `agent-browser`. This enables visual verification of changes, OIDC login automation, and end-to-end workflow testing through the browser.

### Setup

```bash
make agent-tools              # Install agent-browser + Chrome for Testing
scripts/test-agent-browser    # Verify installation
```

### Usage

All browser scripts require the dev stack running (`make run`). For hot reload verification, also run `make dev`. All browser scripts source `scripts/browser-env` and respect `HOLOS_BACKEND_PORT` / `HOLOS_VITE_PORT` environment variables (defaults: 8443 / 5173).

```bash
# Authenticate (OIDC auto-login via embedded Dex, no password prompt)
scripts/browser-login

# Clear session state (triggers fresh OIDC login on next navigation)
scripts/browser-logout

# Verify ID token and refresh token status on the profile page
scripts/browser-verify-change

# Run the full self-service workflow (create org → project → secret → verify → cleanup)
# Requires a Kubernetes cluster (e.g. k3d cluster create holos-dev)
scripts/browser-self-service

# Capture a screenshot of a secret detail page (or any URL)
scripts/browser-capture-secret [URL]

# Capture visual verification screenshots for a PR
# (runs scripts/pr-<N>/capture with agent-dev lifecycle)
scripts/browser-capture-pr <N>

# Test per-key trailing newline affordance in the secret grid
scripts/browser-test-newline
```

Screenshots are saved to `tmp/screenshots/`. After restarting the server, run `scripts/browser-logout && scripts/browser-login` to get a fresh OIDC token (the old Dex signing keys are invalidated).

### Per-Agent Dev Servers

`scripts/agent-dev` builds the frontend into the Go binary (`make generate` + `make build`), then starts the backend on a deterministic port (9000+N, where N is derived from the `agent-N` path segment in the working directory). It uses SIGPIPE-based lifecycle: the script writes the port assignment to stdout, then enters a heartbeat loop. When the pipe reader exits, SIGPIPE terminates the script and an EXIT trap kills the server. No PID files, no stale processes.

Usage (pipe pattern):
```bash
scripts/agent-dev | {
  eval "$(head -1)"                     # sets BACKEND_PORT
  export HOLOS_BACKEND_PORT=$BACKEND_PORT
  scripts/browser-login                 # uses HOLOS_BACKEND_URL
  scripts/browser-capture-secret        # uses HOLOS_BACKEND_URL
  # block exits → pipe breaks → SIGPIPE → server cleaned up
}
```

The Go backend serves the embedded frontend — no Vite dev server is needed for automated screenshot capture. This avoids OIDC port mismatch issues that arise when the Vite dev server runs on a different port than the backend.

`frontend/vite.config.ts` reads `HOLOS_BACKEND_PORT` and `HOLOS_VITE_PORT` from the environment (same defaults) for interactive development with `make dev`.

### Visual Verification for Frontend PRs

**REQUIRED**: Every PR that changes the web UI MUST include:
1. A PR-specific capture script at `scripts/pr-<N>/capture` that takes screenshots of the affected pages.
2. Playwright E2E tests in `frontend/e2e/` that assert the new or changed behavior is visible.
3. Screenshots committed to `docs/screenshots/pr-<N>/` and referenced in the PR.

This is not optional. A frontend PR is not complete until screenshots are captured, committed, and posted to the PR.

The generic launcher `scripts/browser-capture-pr <N>` handles the full agent-dev lifecycle (build, start backend, login, SIGPIPE cleanup) and calls the PR-specific capture script with these environment variables:
- `HOLOS_BACKEND_PORT` — the backend port
- `HOLOS_BACKEND_URL` — `https://localhost:$HOLOS_BACKEND_PORT`
- `PR_SCREENSHOT_DIR` — `docs/screenshots/pr-<N>/` (already created)

For simple cases (single page, no K8s fixtures needed), pass `--url` to skip writing a capture script:
```bash
scripts/browser-capture-pr <N> --url /profile
```
This navigates to the given path and saves `$PR_SCREENSHOT_DIR/screenshot.png` automatically.

For complex cases (multiple pages, K8s fixtures, multiple screenshots), write `scripts/pr-<N>/capture`:
- Apply any required K8s fixtures
- Use `agent-browser` to navigate and capture screenshots to `$PR_SCREENSHOT_DIR`
- The Go backend serves the built frontend — do not use Vite

Workflow:

1. **Add E2E tests** in `frontend/e2e/` asserting the new/changed behavior.
2. **Write the capture script** at `scripts/pr-<N>/capture` (or use `--url` for simple pages).
3. **Run it** after the PR is created to capture screenshots:
   ```bash
   scripts/browser-capture-pr <N>
   # or for simple pages:
   scripts/browser-capture-pr <N> --url /profile
   ```
4. **Commit images** to the feature branch:
   ```bash
   git add docs/screenshots/pr-<N>/ && git commit -m "Add visual verification screenshots for PR #<N>"
   git push
   ```
5. **Reference in PR** using the **commit SHA** in raw GitHub URLs so images remain accessible after the branch is deleted on merge:
   ```bash
   SHA=$(git rev-parse HEAD)
   REPO=$(gh repo view --json nameWithOwner -q .nameWithOwner)
   gh pr comment <N> --body "![description](https://raw.githubusercontent.com/${REPO}/${SHA}/docs/screenshots/pr-<N>/filename.png)"
   ```
   Using the commit SHA (not the branch name) is the conventional approach — the SHA is immutable and resolves correctly both before and after merge. **Important**: PRs with screenshot references must be merged using a **merge commit** (not squash), so the referenced commit SHA survives in the target branch history.
6. **Annotate**: Include a brief caption describing what the screenshot shows and which script produced it.

### Configuration

Project defaults are in `agent-browser.json`: headless mode, self-signed cert acceptance, 1920x1080 viewport, screenshots to `tmp/screenshots/`.

## Contributing

The GitHub issue tracker is for use by project maintainers and their agents. Features and bugs should be reported using Discord. This project operates on a best effort support model; see the LICENSE for the terms of support.

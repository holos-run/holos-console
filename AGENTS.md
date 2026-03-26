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

### Identifying Your Agent Slot

Agents run in worktrees whose path encodes the agent slot. Identify your slot from your working directory — for example, if `pwd` is `/path/to/worktrees/holos-run/agent-2/holos-console`, your slot is `agent-2`. Include the slot in PR descriptions so reviewers know which agent produced the work.

Example workflow:
```bash
git checkout -b feat/add-playwright-config
# ... implement changes, committing as you go ...
git commit -m "Add webServer configuration to playwright.config.ts

Configure Playwright to automatically start Go backend and Vite dev
server before running E2E tests."

# Open a PR that closes the plan issue
gh pr create --title "Add Playwright E2E test infrastructure" --body "$(cat <<'EOF'
## Summary
- Configure Playwright to start Go backend and Vite dev server
- Add E2E test for the login flow

Closes: #42

## Test plan
- [ ] `make test-e2e` passes

🤖 Generated with [Claude Code](https://claude.com/claude-code) · agent-2
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
  - `dist/` - Embedded static files served at `/` (build output from frontend, not source)
- `proto/` - Protobuf source files
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
- `frontend/src/main.tsx` - Entry with TransportProvider, QueryClientProvider, RouterProvider
- `frontend/src/routes/__root.tsx` - Root layout with sidebar navigation
- `frontend/src/routes/_authenticated.tsx` - Auth layout with OIDC redirect
- `frontend/src/lib/auth.ts` - Auth context with OIDC PKCE flow
- `frontend/src/lib/transport.ts` - ConnectRPC transport setup with Bearer token injection
- `frontend/src/routes/` - File-based route definitions

### Authentication

OIDC PKCE flow: Requires `--enable-insecure-dex` flag for embedded Dex at `/dex/`, or an external OIDC provider configured with `--issuer`. Tokens stored in session storage, sent as `Authorization: Bearer` headers. Default credentials: `admin` / `verysecret` (override with `HOLOS_DEX_INITIAL_ADMIN_USERNAME`/`PASSWORD` env vars).

Backend auth: `LazyAuthInterceptor` in `console/rpc/auth.go` verifies JWTs from the `Authorization: Bearer` header and stores `rpc.Claims` in context. Lazy initialization avoids startup race with embedded Dex.

### RBAC

Three-tier access control model evaluated in order (highest role wins):

1. **Organization-level**: Per-org grants stored as JSON annotations on K8s Namespace objects (prefix configurable via `--organization-prefix`, default `org-`)
2. **Project-level**: Per-project grants stored as JSON annotations on K8s Namespace objects (prefix configurable via `--project-prefix`, default `prj-`)
3. **Secret-level**: Per-secret grants stored as JSON annotations on K8s Secret objects

Grant annotations: `console.holos.run/share-users`, `console.holos.run/share-roles`

Namespace prefix scheme (three-part naming: `{namespace-prefix}{type-prefix}{name}`):
- Organizations: `{namespace-prefix}{organization-prefix}{name}` (resource-type label: `organization`)
- Projects: `{namespace-prefix}{project-prefix}{name}` (resource-type label: `project`, optional organization label for IAM inheritance, project label stores project name)

The `--namespace-prefix` flag (default `"holos-"`) prefixes all console-managed namespace names, enabling multi-instance isolation in the same cluster (e.g., `prod-org-acme`, `ci-prj-api`).

Organization creation is controlled by `--disable-org-creation`, `--org-creator-users`, and `--org-creator-roles` CLI flags. By default all authenticated principals can create organizations (implicit grant). Setting `--disable-org-creation` disables this implicit grant; explicit `--org-creator-users` and `--org-creator-roles` lists are still honored.

The `--roles-claim` flag (default `"groups"`) configures which OIDC token claim is used to extract role memberships. This allows integration with identity providers that use non-standard claim names (e.g., `realm_roles`).

Roles: VIEWER (1), EDITOR (2), OWNER (3) defined in `proto/holos/console/v1/rbac.proto`

### Code Generation

Protobuf code is generated using buf. The `generate.go` file contains the `//go:generate buf generate` directive. After modifying `.proto` files in `proto/`, run:

```bash
make generate   # or: go generate ./...
```

This produces:
- `gen/**/*.pb.go` - Go structs for messages
- `gen/**/consolev1connect/*.connect.go` - ConnectRPC client/server bindings
- `frontend/src/gen/**/*_pb.ts` - TypeScript message classes (protobuf-es)
- `frontend/src/gen/**/*_connect.ts` - TypeScript ConnectRPC service definitions

### Adding New RPCs

1. Define RPC and messages in `proto/holos/console/v1/*.proto`
2. Run `make generate`
3. Implement handler method in `console/rpc/` (embed `Unimplemented*Handler` for forward compatibility)
4. Handler is auto-wired when service is registered in `console/console.go`

See `docs/rpc-service-definitions.md` for detailed examples.

### Testing Patterns

**Go tests**: Standard `*_test.go` files with table-driven tests. Uses `k8s.io/client-go/kubernetes/fake` for K8s operations. CLI integration tests use `testscript` in `console/testscript_test.go`.

**UI unit tests**: Vitest + React Testing Library + jsdom. Mock ConnectRPC clients with `vi.mock()`, cast mock responses with `as unknown as ReturnType<...>`. Setup in `frontend/src/test/setup.ts`.

**E2E tests**: Playwright in `frontend/e2e/`. `make test-e2e` orchestrates the full stack (builds Go binary, starts Go backend on :8443 and Vite on :5173). For iterating, start servers manually with `make run` + `make dev`, then run `cd frontend && npm run test:e2e`.

### Version Management

Version is determined by:
1. `console/version/{major,minor,patch}` files (embedded at compile time)
2. `GitDescribe` ldflags override (set by Makefile during build)

Build metadata (commit, tree state, date) injected via ldflags in Makefile.

### Container Builds

Trigger container image builds using the `container.yaml` GitHub workflow. The workflow runs from `main` and accepts a `git_ref` input specifying what to check out and build:

```bash
gh workflow run container.yaml --ref main -f git_ref=refs/heads/<branch-name>
gh workflow run container.yaml --ref main -f git_ref=refs/tags/v1.2.3
```

### Tool Dependencies

Tool versions are pinned in `tools.go` using the Go tools pattern. Install with `make tools`. Currently pins: buf.

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

All browser scripts require the dev stack running (`make run`). For hot reload verification, also run `make dev`.

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
```

Screenshots are saved to `tmp/screenshots/`. After restarting the server, run `scripts/browser-logout && scripts/browser-login` to get a fresh OIDC token (the old Dex signing keys are invalidated).

### Configuration

Project defaults are in `agent-browser.json`: headless mode, self-signed cert acceptance, 1920x1080 viewport, screenshots to `tmp/screenshots/`.

## Contributing

The GitHub issue tracker is for use by project maintainers and their agents. Features and bugs should be reported using Discord. This project operates on a best effort support model; see the LICENSE for the terms of support.

# Browser Automation

Coding agents can interact with the running console UI via `agent-browser`. This enables visual verification of changes, OIDC login automation, and end-to-end workflow testing through the browser.

## Setup

```bash
make agent-tools              # Install agent-browser + Chrome for Testing
scripts/test-agent-browser    # Verify installation
```

## Usage

All browser scripts require the dev stack running (`make run`). For hot reload verification, also run `make dev`. All browser scripts source `scripts/browser-env` and respect `HOLOS_BACKEND_PORT` / `HOLOS_VITE_PORT` environment variables (defaults: 8443 / 5173).

```bash
# Authenticate (OIDC auto-login via embedded Dex, no password prompt)
scripts/browser-login

# Clear session state (triggers fresh OIDC login on next navigation)
scripts/browser-logout

# Verify ID token and refresh token status on the profile page
scripts/browser-verify-change

# Run the full self-service workflow (create org -> project -> secret -> verify -> cleanup)
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

## Configuration

Project defaults are in `agent-browser.json`: headless mode, self-signed cert acceptance, 1920x1080 viewport, screenshots to `tmp/screenshots/`.

## Related

- [Per-Agent Dev Servers](per-agent-dev-servers.md) — Deterministic port assignment for concurrent agents
- [Visual Verification](visual-verification.md) — Screenshot capture workflow for PRs
- [Build Commands](build-commands.md) — `make agent-tools` setup

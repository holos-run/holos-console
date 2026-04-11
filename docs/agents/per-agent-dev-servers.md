# Per-Agent Dev Servers

`scripts/agent-dev` builds the frontend into the Go binary (`make generate` + `make build`), then starts the backend on a deterministic port (9000+N, where N is derived from the `agent-N` path segment in the working directory). It uses SIGPIPE-based lifecycle: the script writes the port assignment to stdout, then enters a heartbeat loop. When the pipe reader exits, SIGPIPE terminates the script and an EXIT trap kills the server. No PID files, no stale processes.

## Usage (pipe pattern)

```bash
scripts/agent-dev | {
  eval "$(head -1)"                     # sets BACKEND_PORT
  export HOLOS_BACKEND_PORT=$BACKEND_PORT
  scripts/browser-login                 # uses HOLOS_BACKEND_URL
  scripts/browser-capture-secret        # uses HOLOS_BACKEND_URL
  # block exits -> pipe breaks -> SIGPIPE -> server cleaned up
}
```

The Go backend serves the embedded frontend — no Vite dev server is needed for automated screenshot capture. This avoids OIDC port mismatch issues that arise when the Vite dev server runs on a different port than the backend.

`frontend/vite.config.ts` reads `HOLOS_BACKEND_PORT` and `HOLOS_VITE_PORT` from the environment (same defaults) for interactive development with `make dev`.

## Related

- [Browser Automation](browser-automation.md) — Scripts that use the agent dev server
- [Visual Verification](visual-verification.md) — Screenshot capture using `scripts/browser-capture-pr`

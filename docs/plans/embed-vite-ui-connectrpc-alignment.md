# Plan: Align embedded Vite UI with ConnectRPC routing guidance

## Goals
- Ensure the embedded React UI matches the research guidance for subpath routing, trailing slash behavior, and SPA fallback.
- Verify Go server routing keeps ConnectRPC paths at the root while serving the UI from `/ui/`.

## Current state vs research
- UI is mounted at `/ui/` with an embedded FS and a custom handler that serves `index.html` for unknown routes (meets SPA fallback guidance).
- Server redirects `/ui` to `/ui/` and `/` to `/ui/` (aligns with trailing slash behavior, but uses HTTP 302 instead of 301).
- Vite build `base` is set to `/ui/` (correct).
- React Router `basename` is `/ui/` (should be `/ui` per research).
- Dev server redirect for `/ui` uses HTTP 302 (should use 301 to mirror production behavior).

## Plan
1. Update React Router basename to `/ui` in `ui/src/main.tsx` and align router tests to use the same basename in `ui/src/App.test.tsx`.
2. Ensure the landing navigation link still resolves to `/ui/` (the test expects a trailing slash) while keeping the router basename at `/ui` per the research doc; use a router-aware anchor so SPA navigation still works.
3. Switch the `/ui` redirect status to HTTP 301 in the Go server (`console/console.go`) and Vite dev server plugin (`ui/vite.config.ts`) so the canonical URL is `/ui/`.
4. Re-check server routing to confirm `/ui/` routes are handled by the UI handler and ConnectRPC endpoints remain unaffected (no code change expected).
5. (Optional) Run the UI unit tests to ensure router changes still pass.

## Validation
- If tests are run, execute `npm test` or the existing project test target; otherwise note tests not run.

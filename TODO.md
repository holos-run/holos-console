# TODO

1. [x] Check that go structs are generated from proto files using go generate.
2. [ ] Initialize the vite + react + material ui + connect rpc client with a simple page that reports the current version.
3. [ ] Wire up auth, use Dex as an issuer to start with PKCE.
4. [ ] Wire up a user profile page, store state in a Secret. Make the backend pluggable so it can write to the filesystem instead of the kube API server.
5. [ ] Fix claude code not correctly killing the dev servers, creates lots of orphan vite and holos-console processes.

---

## Detailed Plan for Item 2: Vite + React + MUI + ConnectRPC Frontend

### Phase 1: Initialize Vite React TypeScript Project

1. [x] Create `ui/` directory at project root for the frontend source code.
2. [x] Initialize Vite with React TypeScript template (`npm create vite@latest ui -- --template react-ts`).
3. [x] Install Material UI dependencies (`@mui/material`, `@emotion/react`, `@emotion/styled`).
4. [x] Configure Vite to output build to `console/ui/` for Go embedding.
5. [x] Create a minimal App component with MUI ThemeProvider.
6. [x] Verify the dev server works (`npm run dev`).
7. [x] Commit: "Initialize Vite React TypeScript frontend with MUI"

### Phase 2: Add ConnectRPC Client with TypeScript Code Generation

8. [x] Add buf configuration for TypeScript/ES code generation (`buf.gen.yaml` updates).
9. [x] Install ConnectRPC client dependencies (`@connectrpc/connect`, `@connectrpc/connect-web`, `@bufbuild/protobuf`).
10. [x] Install buf plugins for TypeScript generation (`@bufbuild/protoc-gen-es`, `@connectrpc/protoc-gen-connect-es`).
11. [x] Configure buf to generate TypeScript client code to `ui/src/gen/`.
12. [x] Run `buf generate` to produce TypeScript client stubs.
13. [x] Create a ConnectRPC transport configured for the backend.
13.b [x] Document how the vite dev server is configured to make RPC calls to the go backend in dev mode.  Include this in the docs/dev-server.md file and mention this file exists in the CONTRIBUTING.md file.  Explain how the contributor can use a single env var to configure the host and port used by both the go backend and the react/vite frontend.
14. [x] Commit: "Add ConnectRPC TypeScript client generation"

### Phase 3: Create Version Display Page

15. [x] Create a Version component that calls `VersionService.GetVersion()`.
16. [x] Display version, git commit, tree state, and build date in a MUI Card.
17. [x] Wire up the Version component in App.tsx.
18. [x] Test manually with Vite dev server proxying to Go backend.
19. [x] Commit: "Add version display page using ConnectRPC"

### Phase 4: Wire Up Go Generate and Embedding

20. [ ] Create `ui/generate.go` with `//go:generate` directive to build the frontend.
21. [ ] The generate script should: run `npm ci`, run `npm run build`, ensure output lands in `console/ui/`.
22. [ ] Make the script idempotent (safe to run multiple times, handles missing node_modules).
23. [ ] Update `console/console.go` to serve SPA with fallback to index.html for client-side routing.
24. [ ] Ensure hard refresh works on any `/ui/*` path by serving index.html for non-file paths.
25. [ ] Test full flow: `go generate ./...` then `make build` then `make run`.
26. [ ] Commit: "Wire up frontend build to go generate with SPA fallback"

### Phase 5: Final Verification and Cleanup

27. [ ] Verify `go generate ./...` is idempotent (running twice produces same result).
28. [ ] Verify `make build` succeeds and binary serves the React app at `/ui/`.
29. [ ] Verify hard refresh works on `/ui/` and any sub-paths.
30. [ ] Verify the version RPC call works from the embedded frontend.
31. [ ] Update CONTRIBUTING.md if needed with frontend development workflow.
32. [ ] Final commit: "Complete frontend integration with ConnectRPC version display"

### Key Implementation Notes

- **Directory Structure**: `ui/` contains frontend source, `console/ui/` contains built assets for embedding.
- **Idempotent Generation**: The generate script checks for `node_modules` and only runs `npm ci` if needed, or uses `npm ci` which is deterministic.
- **SPA Fallback**: The Go server must serve `index.html` for any `/ui/*` path that doesn't match a static file, enabling React Router or similar client-side routing.
- **Dev Workflow**: During development, run Vite dev server (`cd ui && npm run dev`) and configure it to proxy `/holos.console.v1.*` to the Go backend.
- **Vite Base Path**: Configure `base: '/ui/'` in `vite.config.ts` so assets load correctly when served at `/ui/`.

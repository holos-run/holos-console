# Plan: ConnectRPC + TanStack Query integration

## Goal

Replace ad-hoc request caching (e.g., `fetchVersionInfo()` in `ui/src/components/VersionCard.tsx`) with a standard React data layer that:
- Deduplicates requests across components
- Caches results in a single SPA cache
- Provides consistent loading/error states
- Minimizes boilerplate for new RPC calls

## Recommendation

Adopt `@tanstack/react-query` with `@connectrpc/connect-query`.

Reasoning:
- `@connectrpc/connect-query` is the official integration for TanStack Query and ConnectRPC.
- It provides typed hooks keyed by Connect method + request, giving automatic dedupe and caching.
- TanStack Query is the de facto standard for React server state, with strong cache and invalidation primitives.

## Proposed approach

1. **Dependencies**
   - Add `@tanstack/react-query` and `@connectrpc/connect-query` to `ui/package.json`.
   - Verify versions compatible with the existing ConnectRPC v2 packages.

2. **Query client setup**
   - Create a `QueryClient` instance with global defaults:
     - `staleTime: Infinity` and `gcTime: Infinity` for version metadata (only fetch once).
     - `refetchOnWindowFocus: false` for stability.
   - Wrap the app in `QueryClientProvider` in `ui/src/main.tsx`.

3. **ConnectRPC integration**
   - Use `@connectrpc/connect-query` to create a typed query hook for `VersionService.GetVersion`.
   - Keep the existing `versionClient` transport from `ui/src/client.ts`.
   - Store shared query helpers in `ui/src/queries/` (new folder) to reuse across components.

4. **Replace `VersionCard` logic**
   - Replace local `useEffect` + manual cache with a query hook.
   - Render from `data`, `isLoading`, and `error`.
   - Preserve formatting via `formatValue`.

5. **Follow-up: apply pattern for other RPCs**
   - Add a small conventions doc or example that shows how to create new query hooks.

## TODO (implementation)

- [ ] Add `@tanstack/react-query` and `@connectrpc/connect-query` dependencies.
- [ ] Create `QueryClient` setup and add `QueryClientProvider` in `ui/src/main.tsx`.
- [ ] Add `ui/src/queries/version.ts` with a `useVersion` (or similar) hook that calls `VersionService.GetVersion`.
- [ ] Refactor `ui/src/components/VersionCard.tsx` to use the query hook.
- [ ] Set query defaults for once-only behavior (`staleTime`/`gcTime`/`refetchOnWindowFocus`).
- [ ] Document the pattern briefly (e.g., `docs/dev-server.md` or a short `ui/src/queries/README.md`).

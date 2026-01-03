# Fix Trailing Slash

When I visit https://localhost:5173/ui/ then click the Landing nav bar entry on the right it routes me to https://localhost:5173/ui without the trailing slash.  Then, when I reload the page using a hard refresh, vite complains:

> The server is configured with a public base URL of /ui/ - did you mean to visit /ui/ instead?

Think hard and do some web research on the idiomatic way to handle trailing slashes with a react router and vite the way we currently have the project configured.  Make a decision how to handle these trailing slashes uniformly.  Then:

1. [ ] Record the decision in docs/adrs/001-[AGENT PICK A GOOD NAME].md
2. [ ] Update this file with a detailed plan.
3. [ ] Commit the results including this file.

## Decision

Canonicalize the UI base URL to **`/ui/` (with trailing slash)** and ensure all entry points
redirect `/ui` to `/ui/` in both dev (Vite) and production (Go server). The React Router
basename should align with `/ui/` so in-app navigation keeps the trailing slash.

Rationale:
- Vite’s `base` expects a trailing slash for absolute paths (we already set `base: '/ui/'`).
- Hard refresh on `/ui` hits the dev server, which complains because the base is `/ui/`.
- Canonicalizing to `/ui/` prevents inconsistent URLs while preserving existing asset paths.

## Research notes

- Vite `base` configuration is documented with examples that include a trailing slash
  (e.g., `/foo/`), and the dev server treats `/ui` and `/ui/` differently.
- React Router supports `basename` for mounting under a subpath; keeping this aligned
  with the Vite base avoids mismatches.

## Plan

1. **ADR**
   - Add `docs/adrs/001-ui-base-trailing-slash.md` with context, decision, and consequences.

2. **Normalize URLs in dev**
   - Add a lightweight Vite dev server middleware (via `server.configureServer`) that
     redirects `/ui` to `/ui/` with a 301/302.
   - Confirm the warning no longer appears on hard refresh.

3. **Normalize URLs in production**
   - Add a redirect in the Go HTTP server for `/ui` → `/ui/` (similar to the existing `/` redirect).
   - Ensure static file handling still works for `/ui/` and deeper routes.

4. **Align React Router basename**
   - Update `BrowserRouter` `basename` to `/ui/` and ensure navigation links preserve
     the trailing slash for the landing route.

5. **Verification**
   - Visit `/ui/`, click “Landing”, confirm URL remains `/ui/`.
   - Hard refresh on `/ui` should redirect to `/ui/` without Vite warning.

## TODO

- [ ] Add ADR documenting the `/ui/` canonicalization decision.
- [ ] Add Vite dev server redirect `/ui` → `/ui/`.
- [ ] Add Go server redirect `/ui` → `/ui/`.
- [ ] Align React Router basename to `/ui/` and adjust navigation if needed.
- [ ] Verify dev and production behaviors with manual checks.

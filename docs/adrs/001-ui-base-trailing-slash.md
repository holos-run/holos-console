# ADR 001: Canonicalize UI base path without trailing slash

## Status

Accepted (Supersedes previous decision)

## Context

- The UI is served under `/ui` with React Router using `basename="/ui"`.
- Both the Go production server and Vite dev server need consistent redirect behavior.
- React Router v6 recommends configuring `basename` without a trailing slash for simple path joins.
- Users may navigate to `/ui/` or `/ui`; the system should canonicalize to one.

## Decision

Use `/ui` (no trailing slash) as the canonical base path. Redirect `/ui/` to `/ui` in both
the Vite dev server and the Go production server using HTTP 301 (Moved Permanently).

Configuration:
- Vite `base: '/ui'` (no trailing slash)
- React Router `basename="/ui"` (no trailing slash)
- Go mux: `/ui` serves index.html, `/ui/` redirects to `/ui`, `/ui/*` serves SPA routes
- Vite dev middleware: `/ui/` redirects to `/ui`

## Consequences

- URLs are consistent across dev and production.
- React Router's `useHref('/')` resolves to `/ui` which matches the canonical URL.
- Navigation links use standard `<Link to="/">` without workarounds.
- A redirect layer handles `/ui/` requests in both dev and prod.

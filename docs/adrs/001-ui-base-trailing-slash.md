# ADR 001: Canonicalize UI base path with trailing slash

## Status

Accepted

## Context

- The UI is served under `/ui/` (Vite `base: '/ui/'` and Go server static handler).
- React Router uses `basename` to mount the SPA.
- Navigating to the landing route currently results in `/ui` (no trailing slash), and
  a hard refresh causes Vite to warn that the base URL is `/ui/`.

## Decision

Use `/ui/` as the canonical base path with a trailing slash. Redirect `/ui` to `/ui/`
in both the Vite dev server and the Go production server. Align React Router’s
`basename` to `/ui/` so in-app navigation preserves the trailing slash.

## Consequences

- URLs are consistent across dev and production.
- Hard refreshes no longer trigger Vite’s base path warning.
- A small redirect layer is required in both dev and prod to handle `/ui`.

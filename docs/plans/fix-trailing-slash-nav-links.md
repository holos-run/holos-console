# Fix Trailing Slash

## Decision

Canonicalize the UI base URL to **`/ui` (no trailing slash)** and ensure all entry points
redirect `/ui/` to `/ui` in both dev (Vite) and production (Go server). The React Router
basename should align with `/ui` so in-app navigation keeps the trailing slash.

Rationale:
- Orient the entire system around React Router v6's strong recommendation to configure the base url as /ui with no trailing slash so that path joins are simple and consistent.

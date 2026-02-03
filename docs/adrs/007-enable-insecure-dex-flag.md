# ADR-007: Require --enable-insecure-dex to Start Embedded Dex Provider

## Status

Accepted

## Context

The holos-console binary embeds a [Dex](https://dexidp.io/) OIDC identity
provider for local development convenience. The embedded Dex configures an
auto-login connector that authenticates users without any credentials and
grants them the `owner` role (full permissions). Prior to this change, the
embedded Dex provider started unconditionally whenever the server ran—the
issuer URL was auto-derived from the listen address, and the `/dex/` handler
was always mounted.

A security audit (issue #134, FINDING-01) identified this as a medium-severity
issue: if an operator deploys the binary to production without configuring an
external OIDC provider, the auto-login connector grants unauthenticated access
with full owner privileges.

Issue #138 tracks the fix.

## Decision

Add a `--enable-insecure-dex` CLI flag (default `false`) that explicitly opts
in to starting the embedded Dex provider. When the flag is false:

- The OIDC issuer URL is **not** auto-derived from the listen address.
- The `/dex/` HTTP handler is **not** mounted.
- The `/api/debug/oidc` endpoint is **not** mounted.

An explicit `--issuer` flag continues to work regardless of
`--enable-insecure-dex` for configuring JWT validation against an external
OIDC provider.

The `make run` target and Playwright E2E server command pass
`--enable-insecure-dex` so development workflows are unaffected.

## Consequences

- **Production safety**: The binary is safe by default. Operators must
  explicitly opt in to the insecure embedded Dex provider. Accidental
  production deployments without an external OIDC provider will have no
  authentication endpoint rather than an unauthenticated one.

- **Development workflow**: Developers using `make run` or `make test-e2e`
  are unaffected because those targets include the flag. Developers invoking
  the binary directly must add `--enable-insecure-dex`.

- **Flag naming**: The `insecure` prefix in the flag name serves as a
  deliberate warning that this mode is not suitable for production.

## References

- Issue #134: Security audit findings
- Issue #138: Fix FINDING-01 embedded Dex auto-login connector
- ADR-002: BFF architecture with oauth2-proxy (production auth pattern)

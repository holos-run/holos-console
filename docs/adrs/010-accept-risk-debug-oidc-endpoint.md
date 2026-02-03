# ADR 010: Accept Risk of /api/debug/oidc Endpoint Exposing Provider Metadata

## Status

Accepted

## Context

The application security review (#134, FINDING-04) identified that the `/api/debug/oidc` endpoint is registered whenever `--issuer` is configured, not only in development mode. The endpoint fetches and returns the OIDC discovery document from the configured issuer along with debug notes (hints about checking `scopes_supported` and a reference to an internal investigation document).

The review recommended gating the endpoint behind a `--debug` or `--dev-mode` flag, or removing it in favor of the standard `/.well-known/openid-configuration` path.

## Decision

Accept the risk. The `/api/debug/oidc` endpoint will remain available whenever an issuer is configured.

**Rationale:**

1. **The OIDC discovery document is already public.** Every OIDC-compliant issuer publishes its discovery document at `{issuer}/.well-known/openid-configuration`. The endpoint does not expose any information that is not already available to anyone who knows the issuer URL.

2. **The debug notes are informational, not sensitive.** The additional fields (`scopes_supported` hint, investigation reference) are operator-facing troubleshooting aids. They do not reveal credentials, internal network topology, or exploitable implementation details.

3. **The endpoint is useful for production troubleshooting.** Operators diagnosing OIDC integration issues (e.g., missing groups claims, misconfigured scopes) benefit from having the discovery document and troubleshooting hints available without needing to manually curl the issuer or enable a separate debug flag.

4. **Adding a gating flag increases operational complexity for negligible security benefit.** A `--debug` or `--dev-mode` flag adds configuration surface area that operators must manage, with no meaningful reduction in attack surface since the underlying data is already public.

## Consequences

- The `/api/debug/oidc` endpoint remains unconditionally available when `--issuer` is set.
- No code changes are required.
- The finding is documented as accepted risk in this ADR.

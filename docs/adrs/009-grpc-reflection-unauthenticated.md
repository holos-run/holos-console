# ADR 009: Accept unauthenticated gRPC reflection endpoints

## Status

Accepted

## Context

The application security review (FINDING-03 in #134) identified that gRPC
reflection services (`grpc.reflection.v1` and `grpc.reflection.v1alpha`) are
registered without authentication interceptors. This allows unauthenticated
clients to enumerate all available RPC services, methods, and message schemas
via tools like `grpcurl`.

The finding is rated Low severity because the information disclosed is limited
to the API surface definition, which does not include any user data, secrets, or
internal state.

## Decision

Accept the risk. gRPC reflection endpoints remain unauthenticated.

Rationale:

1. **The API surface is already public.** Proto source files are checked into
   the `proto/` directory of this repository and generated TypeScript types are
   shipped in the UI bundle. Reflection does not expose information beyond what
   is already available to anyone with read access to the repository or the
   browser developer tools.

2. **Reflection aids developer and operator tooling.** Tools such as `grpcurl`
   and `grpcui` rely on reflection for service discovery. Requiring
   authentication would degrade the developer experience without a meaningful
   security benefit.

3. **No sensitive data is exposed.** Reflection returns only service names,
   method signatures, and message schemas. It does not return request/response
   payloads, user data, or server configuration.

## Consequences

- Unauthenticated clients can discover the full list of RPC services and their
  request/response message schemas.
- This is acceptable because the same information is publicly available in the
  source repository and client bundles.
- If the API surface becomes sensitive in the future (e.g., internal-only
  services are added), this decision should be revisited.

# Test Strategy

**Prefer unit tests over E2E tests.** Rendering, interaction, navigation logic, and ConnectRPC data shaping all belong in unit tests using mocked query hooks. Reserve E2E tests for:

- The OIDC login flow (requires a real Dex server)
- Full-stack CRUD round-trips that verify server-side behavior (requires a real Kubernetes cluster)

When a behavior can be verified with a unit test, write a unit test. Do not add an E2E test for the same behavior.

## Related

- [Testing Patterns](testing-patterns.md) — Specific patterns for Go, UI, and E2E tests
- [Contributing — Testing](../../CONTRIBUTING.md#testing) — Test make targets and single test invocations

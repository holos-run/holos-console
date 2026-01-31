# ADR 004: TanStack Query with ConnectRPC for Server State Management

## Status

Accepted

## Context

Holos Console is a React 19 frontend backed by a Go HTTPS server exposing
ConnectRPC services defined with Protocol Buffers. The project needs a strategy
for managing server state—data fetched from and synchronized with the backend—in
the React UI.

### What is TanStack Query?

TanStack Query (formerly React Query) was created by Tanner Linsley in 2019 to
solve a fundamental problem: managing server state in React applications. Unlike
client state (UI toggles, form inputs, theme preferences), server state is
asynchronous, has a remote source of truth, and can become stale without the
frontend's knowledge. Traditional approaches—manual `useEffect`/`fetch` patterns
or co-opting client state libraries like Redux for async data—produced fragile,
boilerplate-heavy code that poorly handled caching, background refetching, and
deduplication.

TanStack Query provides declarative primitives (`useQuery`, `useMutation`,
`useInfiniteQuery`) that automatically manage loading states, error states,
caching, cache invalidation, background refetching, request deduplication,
and stale-while-revalidate semantics. It has become the de facto standard for
server state in React, with over 4 billion downloads across the TanStack
ecosystem and adoption by companies including Walmart, CrowdStrike, and Dropbox.

In 2022, the library was rebranded from React Query to TanStack Query when it
expanded to support Vue, Solid, Svelte, and Angular through a framework-agnostic
core with adapter layers.

### What is ConnectRPC?

ConnectRPC was created by Buf Technologies in June 2022 as a response to the
growing complexity and instability of Google's gRPC ecosystem. Organizations
depending on gRPC faced breaking changes and lacked influence over the project's
direction. ConnectRPC joined the CNCF in June 2024 and is used in production by
CrowdStrike, PlanetScale, Redpanda, Bluesky, Chick-fil-A, and Dropbox.

ConnectRPC solves a specific problem: making Protocol Buffer-defined RPCs
accessible from web browsers without requiring a translation proxy. Traditional
gRPC requires HTTP/2 framing that browser APIs do not expose, forcing teams to
deploy Envoy or similar proxies to translate between browser requests and backend
gRPC services. ConnectRPC servers natively support three protocols—gRPC,
gRPC-Web, and the Connect protocol—so browsers can call backends directly over
standard HTTP.

The Connect protocol uses plain HTTP POST requests with JSON or Protobuf bodies,
making APIs curl-friendly and debuggable with standard HTTP tools. Error
responses are JSON with meaningful HTTP status codes rather than gRPC's binary
framing and trailer-based error model.

Key libraries:

- **connect-go**: Go server and client implementation
- **connect-es**: TypeScript client for browsers and Node.js
- **protobuf-es**: TypeScript Protocol Buffer runtime (message classes, serialization)
- **buf**: Modern protobuf compiler for code generation, linting, and breaking change detection

### Why they pair well together

The `@connectrpc/connect-query` package bridges ConnectRPC and TanStack Query,
eliminating the boilerplate that typically accompanies either technology when used
alone. It works by generating TanStack Query options directly from `.proto`
service definitions during `buf generate`. This means:

1. **Zero-boilerplate query keys.** Query keys are derived from the RPC method
   name and request message, so cache entries are automatically scoped and
   type-safe. Developers never manually construct or manage query keys.

2. **End-to-end type safety.** The chain from `.proto` schema → generated
   TypeScript types → query hook → component props is fully typed. Changing a
   field name in a `.proto` file produces a compile-time error in every component
   that references it.

3. **Single source of truth for the API contract.** Protocol Buffer schemas
   define request/response types, service methods, and documentation in one
   place. Both the Go backend and TypeScript frontend consume generated code from
   the same schema, eliminating the class of bugs where client and server
   disagree on field names, types, or required fields.

4. **Declarative data fetching with automatic lifecycle management.** TanStack
   Query manages caching, deduplication, background refetching, and garbage
   collection. ConnectRPC handles serialization, transport, and protocol
   negotiation. The application code is a single hook call:

   ```typescript
   const { data, isLoading } = useQuery(getSecret, { name: "my-secret" });
   ```

5. **Mutation integration.** `useMutation` hooks generated from RPC definitions
   pair with `queryClient.invalidateQueries` to keep cached data consistent
   after writes, using the same generated query keys.

## Decision

**Use TanStack Query with ConnectRPC via `@connectrpc/connect-query` for all
server state management in Holos Console.**

Specifically:

- All data fetching uses `useQuery` or `useSuspenseQuery` with generated query
  descriptors from `@connectrpc/connect-query`.
- All data mutations use `useMutation` with generated mutation descriptors.
- Cache invalidation after mutations uses the generated query keys.
- No global client-state library (Redux, Zustand, etc.) is used for server
  state. TanStack Query is the sole manager of data fetched from the backend.
- Client-only state (UI state, form state) uses React's built-in primitives
  (`useState`, `useReducer`, context).

## Consequences

### Positive

- Protobuf schemas are the single source of truth for the API contract across
  Go and TypeScript, eliminating client/server type drift.
- Generated query keys and hooks eliminate manual boilerplate and reduce the
  surface area for bugs.
- TanStack Query's caching, deduplication, and background refetching provide
  a responsive UI without manual cache management code.
- Adding a new RPC to the UI requires only `buf generate` and a hook call in
  the component—no manual type definitions, fetch wrappers, or state management
  code.
- The architecture is well-documented by both projects and widely adopted,
  making onboarding straightforward.

### Negative

- Developers must understand both TanStack Query concepts (query keys, stale
  time, cache invalidation) and ConnectRPC concepts (transports, interceptors,
  protobuf-es message types).
- The `buf generate` step adds a build dependency; forgetting to regenerate
  after `.proto` changes causes stale types.
- Connect-query is a younger project than either TanStack Query or ConnectRPC
  individually, though it is maintained by Buf Technologies and follows the
  same CNCF governance.

### Neutral

- The project already uses this stack (`ui/src/main.tsx` configures both
  `TransportProvider` and `QueryClientProvider`; `ui/src/queries/` contains
  TanStack Query hooks for RPC calls). This ADR formalizes the existing practice
  as a deliberate architectural decision.

## References

- [TanStack Query documentation](https://tanstack.com/query/latest)
- [ConnectRPC documentation](https://connectrpc.com/docs/introduction/)
- [connect-query Getting Started](https://connectrpc.com/docs/web/query/getting-started/)
- [Introducing Connect-Query (Buf blog)](https://buf.build/blog/introducing-connect-query)
- [Connect: A better gRPC (Buf blog)](https://buf.build/blog/connect-a-better-grpc)
- [Connect RPC joins CNCF](https://buf.build/blog/connect-rpc-joins-cncf)
- [ui/src/main.tsx](../../ui/src/main.tsx) - TransportProvider and QueryClientProvider setup
- [ui/src/queries/](../../ui/src/queries/) - Existing TanStack Query hooks for RPC calls

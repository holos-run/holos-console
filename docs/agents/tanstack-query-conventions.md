# TanStack Query Conventions

Source issue: HOL-946. This document codifies the frontend query patterns
identified in the HOL-943 audit and makes `frontend/src/queries/secrets.ts` the
reference implementation.

## Query Key Factories

All hand-written TanStack Query keys live in
`frontend/src/queries/keys.ts`. Use `keys.<resource>.<scope>(...)` for every
`queryKey` and invalidation call. Do not add ad-hoc query-key array literals in
query modules, routes, or components.

Key factories return readonly tuples with the resource first, then the scope or
operation, then identifiers:

```ts
keys.secrets.list(project)
keys.secrets.get(project, name)
keys.templates.policyState(namespace, name)
```

Use explicit prefix factories for broad invalidation. For example,
`keys.folders.listScope(organization)` invalidates every folder-list variant
under an organization, while `keys.folders.list(organization, parentType,
parentName)` targets one concrete list.

ConnectRPC queries created by `@connectrpc/connect-query` keep the library's
cache shape. Mutations that affect those library-owned reads invalidate
`keys.connect.all()`.

## Transport And Hook Split

Query hooks own transport and client creation:

```ts
const { isAuthenticated } = useAuth()
const transport = useTransport()
const client = useMemo(() => createClient(Service, transport), [transport])
```

Keep RPC request construction inside the hook's `queryFn` or `mutationFn`.
Routes and components should call hooks instead of creating clients directly.
The exception is a cross-namespace action that cannot bind a namespace at hook
creation time; when that is needed, the caller must use `keys.ts` for direct
invalidation.

Preserve public hook names when refactoring internals.

## Stale Time And GC Time Defaults

Use TanStack Query defaults unless a resource has a clear stability contract.
The current default means normal resource data becomes stale immediately and is
eligible for the app-level garbage-collection default.

Allowed overrides:

- Static server-binary data may use `staleTime: Infinity`, as
  `useListTemplateExamples()` does.
- Polling or live-status reads may set `refetchInterval` from caller options.
- Do not set `gcTime` in resource hooks unless an issue explicitly documents
  the memory and UX tradeoff.

## Enabled Guards

Every read hook gates on authentication and all required identifiers:

```ts
enabled: isAuthenticated && !!project && !!name
```

For caller-controlled optional reads, combine the caller flag with the standard
guard:

```ts
enabled: isAuthenticated && !!namespace && !!name && callerEnabled
```

Creation pages may read selected-entity store fallbacks, but query hooks should
still treat empty route/search-derived identifiers as disabled.

## Mutation Invalidation Matrix

Use the smallest scope that refreshes every visible stale surface. Invalidate
lists before details for predictable list-page refreshes.

| Mutation | Invalidation scope |
|---|---|
| `createSecret(project)` | `keys.secrets.list(project)`, `keys.secrets.get(project, name)` |
| `updateSecret(project)` | `keys.secrets.list(project)`, `keys.secrets.get(project, name)` |
| `updateSecretSharing(project)` | `keys.secrets.list(project)`, `keys.secrets.get(project, name)` |
| `deleteSecret(project)` | `keys.secrets.list(project)`, `keys.secrets.get(project, name)` |
| Create resource | Affected list key |
| Update resource | Affected list key and affected detail key |
| Delete resource | Affected list key; detail key when the deleted item has a detail cache |
| Sharing/default-sharing update | Affected list key and affected detail key when metadata appears in both |
| Render-affecting template/deployment update | The resource list, resource detail, and affected policy-state key |

`useGetSecretMetadata()` deliberately derives metadata from
`keys.secrets.list(project)` because there is no dedicated metadata RPC.
Invalidating the secret list therefore refreshes secret detail metadata panes.

## Optimistic Updates

Do not use optimistic updates by default. They are allowed only when the
mutation payload is sufficient to update all affected cache entries without
inventing server-owned fields.

When a future hook uses optimism, it must:

- cancel affected queries before writing optimistic cache state;
- snapshot every affected cache entry;
- roll back every snapshot in `onError`;
- invalidate the documented mutation scope in `onSettled`;
- avoid optimistic writes for secret material or any sensitive value.

Secrets currently use invalidation-only mutation handlers.

## Prefetch Policy

Do not prefetch automatically from list rows. The current resource lists are
metadata-heavy and details can be permission-sensitive, so prefetch must be an
explicit issue-level decision.

Allowed prefetch cases:

- a route transition where the destination is already certain;
- static reference data used by a soon-to-open editor;
- a measured latency problem with documented query scope and cancellation.

Prefetches must use `keys.ts` factories and the same auth/identifier guards as
the owning read hook.

## Dependent Queries

Express dependencies through enabled guards and stable fallback arrays. Fan-out
hooks should wait for structural parents, then map those results into child
queries with `useQueries`.

Use module-level empty sentinels for pending parent lists so dependency arrays
stay referentially stable:

```ts
const EMPTY_PROJECTS: readonly Project[] = []
const projects = useMemo(() => projectsQuery.data ?? EMPTY_PROJECTS, [
  projectsQuery.data,
])
```

Aggregate fan-out results with `aggregateFanOut` so partial data can remain
visible when one branch fails.

## KPD And Placeholder Rules For Grid Lists

ResourceGrid-backed list reads should keep previous data across route/search
parameter changes when showing stale rows is less disruptive than blanking the
table. Use:

```ts
placeholderData: keepPreviousData
```

Apply this to list-style hooks such as `useListSecrets()` and fan-out list
queries. Do not apply keep-previous-data to secret-value detail reads, editor
payloads, or any read where stale data could be mistaken for the selected
resource's sensitive content.

## URL-Driven Filter And Sort Coordination

ResourceGrid URL state is parsed and serialized by
`frontend/src/components/resource-grid/url-state.ts`. Query hooks should accept
already-normalized route/search params and put every server-side filter
dimension into the query key.

Client-only grid search, kind filters, and sort state stay in the route search
params and should not be duplicated in query keys unless the server request
uses them.

## Mutation Success Handlers

Mutation `onSuccess` handlers are responsible for cache invalidation only.
Toast copy and navigation belong to the route or component that initiated the
mutation, because that layer knows whether the user is staying in an editor,
returning to a list, or following a `returnTo` search param.

When a mutation needs both navigation and invalidation, await the mutation from
the caller, let the hook invalidate the documented scope, then navigate or show
toast from the caller after the mutation resolves.

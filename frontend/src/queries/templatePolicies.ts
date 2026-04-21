import { useMemo } from 'react'
import { create } from '@bufbuild/protobuf'
import { createClient } from '@connectrpc/connect'
import { useTransport } from '@connectrpc/connect-query'
import {
  useQuery,
  useQueries,
  useMutation,
  useQueryClient,
} from '@tanstack/react-query'
import {
  TemplatePolicyService,
  TemplatePolicySchema,
  TemplatePolicyKind,
} from '@/gen/holos/console/v1/template_policies_pb.js'
import type {
  TemplatePolicy,
  TemplatePolicyRule,
} from '@/gen/holos/console/v1/template_policies_pb.js'
import { useAuth } from '@/lib/auth'
import { useListFolders } from '@/queries/folders'
import type { Folder } from '@/gen/holos/console/v1/folders_pb.js'
import { namespaceForFolder, namespaceForOrg } from '@/lib/scope-labels'

// Re-export generated types/enums used by UI consumers. HOL-600 removed
// TemplatePolicyTarget from the proto — render-target selection now
// lives on TemplatePolicyBinding.
export type { TemplatePolicy, TemplatePolicyRule }
export { TemplatePolicyKind }

/** Query key helper for the template policies list at a given namespace. */
function templatePolicyListKey(namespace: string) {
  return ['templatePolicies', 'list', namespace] as const
}

/** Query key helper for a single template policy. */
function templatePolicyGetKey(namespace: string, name: string) {
  return ['templatePolicies', 'get', namespace, name] as const
}

// useListTemplatePolicies fetches all policies visible within a namespace.
// Mirrors the shape of useListTemplates in queries/templates.ts.
export function useListTemplatePolicies(namespace: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(
    () => createClient(TemplatePolicyService, transport),
    [transport],
  )
  return useQuery({
    queryKey: templatePolicyListKey(namespace),
    queryFn: async () => {
      const response = await client.listTemplatePolicies({ namespace })
      return response.policies
    },
    enabled: isAuthenticated && !!namespace,
  })
}

// Slice of the TanStack Query result this module cares about. Accepting an
// interface (vs a union of `useQuery` return types) keeps aggregateFanOut
// testable without importing TanStack internals and lets the disabled-query
// case be modeled with `fetchStatus === 'idle'`.
export interface FanOutQueryState<T> {
  data: T | undefined
  error: unknown
  isPending: boolean
  fetchStatus: 'fetching' | 'paused' | 'idle'
}

export interface FanOutAggregate<T> {
  data: T[] | undefined
  isPending: boolean
  error: Error | null
}

// aggregateFanOut merges org-scope + folder-scope `ListTemplatePolicies`
// results into a single list view. The rules:
//
// - `isPending` reports first-load only — a query counts as "still loading"
//   when it is pending AND actively fetching. A disabled query
//   (`fetchStatus === 'idle'`) is treated as resolved-empty so the aggregate
//   does not lock on an empty org name or unauthenticated user.
// - `error` is the first non-null error encountered, as an Error. Partial
//   data is preserved alongside the error so the caller can render rows
//   from successful queries with an inline warning rather than blanking
//   the whole grid.
// - `data` is the concatenated list whenever any query has resolved. It is
//   only `undefined` while the aggregate is still pending with nothing
//   materialized yet.
export function aggregateFanOut<T>(
  queries: FanOutQueryState<T[]>[],
): FanOutAggregate<T> {
  const firstLoadPending = queries.some(
    (q) => q.isPending && q.fetchStatus !== 'idle' && q.data === undefined,
  )
  const firstError = queries.find((q) => q.error != null)?.error
  const error =
    firstError instanceof Error
      ? firstError
      : firstError != null
        ? new Error(String(firstError))
        : null

  const hasAnyData = queries.some((q) => q.data !== undefined)
  if (!hasAnyData && firstLoadPending) {
    return { data: undefined, isPending: true, error }
  }

  const data: T[] = []
  for (const q of queries) {
    if (q.data) data.push(...q.data)
  }
  return { data, isPending: false, error }
}

// Module-level sentinel so the `folders` useMemo fallback preserves reference
// identity across renders when the folders list is still pending or empty.
const EMPTY_FOLDERS: readonly Folder[] = []

// useAllTemplatePoliciesForOrg fans a ListTemplatePolicies call across every
// namespace reachable from an organization root — the org namespace plus one
// namespace per folder visible to the caller — and flattens the results into
// one array. HOL-608 AC requires the unified Template Policies index to show
// org- and folder-scoped policies together, but TemplatePolicyService has no
// SearchTemplatePolicies RPC (tracked in HOL-590 as the eventual server-side
// consolidation). Until that lands this hook is the client-side fan-out.
//
// See aggregateFanOut for the exact pending / error semantics.
export function useAllTemplatePoliciesForOrg(
  orgName: string,
): FanOutAggregate<TemplatePolicy> {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(
    () => createClient(TemplatePolicyService, transport),
    [transport],
  )
  const orgNamespace = namespaceForOrg(orgName)
  const foldersQuery = useListFolders(orgName)
  const folders = useMemo(
    () => foldersQuery.data ?? EMPTY_FOLDERS,
    [foldersQuery.data],
  )

  const folderQueries = useQueries({
    queries: folders.map((folder) => ({
      queryKey: templatePolicyListKey(namespaceForFolder(folder.name)),
      queryFn: async (): Promise<TemplatePolicy[]> => {
        const response = await client.listTemplatePolicies({
          namespace: namespaceForFolder(folder.name),
        })
        return response.policies
      },
      enabled: isAuthenticated && !!folder.name,
    })),
  })

  const orgQuery = useQuery({
    queryKey: templatePolicyListKey(orgNamespace),
    queryFn: async () => {
      const response = await client.listTemplatePolicies({
        namespace: orgNamespace,
      })
      return response.policies
    },
    enabled: isAuthenticated && !!orgNamespace,
  })

  // Model the folders-list query as one more input to aggregateFanOut.
  // When folders are still loading we want the aggregate to report pending
  // (nothing has materialized yet). When the folders-list errored we want
  // the caller to see the error alongside any org-scoped policies that did
  // resolve, rather than blanking the whole grid on a structural failure.
  // Wrapping the folders query in a FanOutQueryState<TemplatePolicy[]>
  // (with data always [] on success) gives us both behaviors for free.
  const foldersAsQuery: FanOutQueryState<TemplatePolicy[]> = {
    data: foldersQuery.data === undefined ? undefined : [],
    error: foldersQuery.error,
    isPending: foldersQuery.isPending,
    fetchStatus: foldersQuery.fetchStatus,
  }

  return aggregateFanOut<TemplatePolicy>([
    foldersAsQuery,
    {
      data: orgQuery.data,
      error: orgQuery.error,
      isPending: orgQuery.isPending,
      fetchStatus: orgQuery.fetchStatus,
    },
    ...folderQueries.map((q) => ({
      data: q.data,
      error: q.error,
      isPending: q.isPending,
      fetchStatus: q.fetchStatus,
    })),
  ])
}

// useGetTemplatePolicy fetches a single policy by name within a namespace.
export function useGetTemplatePolicy(namespace: string, name: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(
    () => createClient(TemplatePolicyService, transport),
    [transport],
  )
  return useQuery({
    queryKey: templatePolicyGetKey(namespace, name),
    queryFn: async () => {
      const response = await client.getTemplatePolicy({ namespace, name })
      return response.policy
    },
    enabled: isAuthenticated && !!namespace && !!name,
  })
}

// useCreateTemplatePolicy creates a new policy at the given namespace.
export function useCreateTemplatePolicy(namespace: string) {
  const transport = useTransport()
  const client = useMemo(
    () => createClient(TemplatePolicyService, transport),
    [transport],
  )
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: {
      name: string
      displayName: string
      description: string
      rules: TemplatePolicyRule[]
    }) => {
      return client.createTemplatePolicy({
        namespace,
        policy: create(TemplatePolicySchema, {
          name: params.name,
          namespace,
          displayName: params.displayName,
          description: params.description,
          rules: params.rules,
        }),
      })
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: templatePolicyListKey(namespace) })
    },
  })
}

// useUpdateTemplatePolicy updates an existing policy.
export function useUpdateTemplatePolicy(namespace: string, name: string) {
  const transport = useTransport()
  const client = useMemo(
    () => createClient(TemplatePolicyService, transport),
    [transport],
  )
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: {
      displayName?: string
      description?: string
      rules: TemplatePolicyRule[]
    }) => {
      return client.updateTemplatePolicy({
        namespace,
        policy: create(TemplatePolicySchema, {
          name,
          namespace,
          displayName: params.displayName ?? '',
          description: params.description ?? '',
          rules: params.rules,
        }),
      })
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: templatePolicyListKey(namespace) })
      queryClient.invalidateQueries({ queryKey: templatePolicyGetKey(namespace, name) })
    },
  })
}

// useDeleteTemplatePolicy deletes a policy by name.
export function useDeleteTemplatePolicy(namespace: string) {
  const transport = useTransport()
  const client = useMemo(
    () => createClient(TemplatePolicyService, transport),
    [transport],
  )
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: { name: string }) =>
      client.deleteTemplatePolicy({ namespace, ...params }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: templatePolicyListKey(namespace) })
    },
  })
}

// countRulesByKind returns the number of REQUIRE and EXCLUDE rules in a policy.
// Used by list views to render a kind summary without reimplementing the count
// in each route.
export function countRulesByKind(policy: TemplatePolicy | undefined): {
  require: number
  exclude: number
} {
  let require = 0
  let exclude = 0
  for (const rule of policy?.rules ?? []) {
    if (rule.kind === TemplatePolicyKind.REQUIRE) require++
    else if (rule.kind === TemplatePolicyKind.EXCLUDE) exclude++
  }
  return { require, exclude }
}

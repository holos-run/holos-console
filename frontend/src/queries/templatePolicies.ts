import { useMemo } from 'react'
import { create } from '@bufbuild/protobuf'
import { createClient } from '@connectrpc/connect'
import { useTransport } from '@connectrpc/connect-query'
import {
  keepPreviousData,
  useQuery,
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
  LinkableTemplatePolicy,
} from '@/gen/holos/console/v1/template_policies_pb.js'
import { useAuth } from '@/lib/auth'
import { keys } from '@/queries/keys'

// Re-export generated types/enums used by UI consumers. HOL-600 removed
// TemplatePolicyTarget from the proto — render-target selection now
// lives on TemplatePolicyBinding.
export type { TemplatePolicy, TemplatePolicyRule, LinkableTemplatePolicy }
export { TemplatePolicyKind }

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
    queryKey: keys.templatePolicies.list(namespace),
    queryFn: async () => {
      const response = await client.listTemplatePolicies({ namespace })
      return response.policies
    },
    enabled: isAuthenticated && !!namespace,
    placeholderData: keepPreviousData,
  })
}

// useListLinkableTemplatePolicies fetches all TemplatePolicies in the given
// namespace. The response carries the owning namespace on each item so the UI
// can render a scope badge. BindingForm uses this hook to populate the policy
// picker (HOL-835).
//
// HOL-912 simplified the backend from an ancestor-walk RPC to a single-namespace
// list. The frontend consumer update to call useListTemplatePolicies directly
// is tracked in HOL-912 Phase 6.
export function useListLinkableTemplatePolicies(namespace: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(
    () => createClient(TemplatePolicyService, transport),
    [transport],
  )
  return useQuery({
    queryKey: keys.templatePolicies.linkable(namespace),
    queryFn: async () => {
      const response = await client.listLinkableTemplatePolicies({
        namespace,
      })
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

// useGetTemplatePolicy fetches a single policy by name within a namespace.
export function useGetTemplatePolicy(namespace: string, name: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(
    () => createClient(TemplatePolicyService, transport),
    [transport],
  )
  return useQuery({
    queryKey: keys.templatePolicies.get(namespace, name),
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
      queryClient.invalidateQueries({ queryKey: keys.templatePolicies.list(namespace) })
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
      queryClient.invalidateQueries({ queryKey: keys.templatePolicies.list(namespace) })
      queryClient.invalidateQueries({ queryKey: keys.templatePolicies.get(namespace, name) })
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
      queryClient.invalidateQueries({ queryKey: keys.templatePolicies.list(namespace) })
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

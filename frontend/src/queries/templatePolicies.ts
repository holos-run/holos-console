import { useMemo } from 'react'
import { create } from '@bufbuild/protobuf'
import { createClient } from '@connectrpc/connect'
import { useTransport } from '@connectrpc/connect-query'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
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

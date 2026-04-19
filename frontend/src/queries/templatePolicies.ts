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
import { type TemplateScopeRef, namespaceForRef } from '@/lib/scope-shim'
import { useAuth } from '@/lib/auth'

// Re-export generated types/enums used by UI consumers. HOL-600 removed
// TemplatePolicyTarget from the proto — render-target selection now
// lives on TemplatePolicyBinding.
export type { TemplatePolicy, TemplatePolicyRule }
export { TemplatePolicyKind }

/** Query key helper for the template policies list at a given scope. */
function templatePolicyListKey(scope: TemplateScopeRef) {
  return ['templatePolicies', 'list', scope.scope, scope.scopeName] as const
}

/** Query key helper for a single template policy. */
function templatePolicyGetKey(scope: TemplateScopeRef, name: string) {
  return ['templatePolicies', 'get', scope.scope, scope.scopeName, name] as const
}

// useListTemplatePolicies fetches all policies visible within a scope.
// Mirrors the shape of useListTemplates in queries/templates.ts.
export function useListTemplatePolicies(scope: TemplateScopeRef) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(
    () => createClient(TemplatePolicyService, transport),
    [transport],
  )
  return useQuery({
    queryKey: templatePolicyListKey(scope),
    queryFn: async () => {
      const response = await client.listTemplatePolicies({ namespace: namespaceForRef(scope) })
      return response.policies
    },
    enabled: isAuthenticated && !!scope.scopeName,
  })
}

// useGetTemplatePolicy fetches a single policy by name within a scope.
export function useGetTemplatePolicy(scope: TemplateScopeRef, name: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(
    () => createClient(TemplatePolicyService, transport),
    [transport],
  )
  return useQuery({
    queryKey: templatePolicyGetKey(scope, name),
    queryFn: async () => {
      const response = await client.getTemplatePolicy({ namespace: namespaceForRef(scope), name })
      return response.policy
    },
    enabled: isAuthenticated && !!scope.scopeName && !!name,
  })
}

// useCreateTemplatePolicy creates a new policy at the given scope.
export function useCreateTemplatePolicy(scope: TemplateScopeRef) {
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
      const ns = namespaceForRef(scope)
      return client.createTemplatePolicy({
        namespace: ns,
        policy: create(TemplatePolicySchema, {
          name: params.name,
          namespace: ns,
          displayName: params.displayName,
          description: params.description,
          rules: params.rules,
        }),
      })
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: templatePolicyListKey(scope) })
    },
  })
}

// useUpdateTemplatePolicy updates an existing policy.
export function useUpdateTemplatePolicy(scope: TemplateScopeRef, name: string) {
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
      const ns = namespaceForRef(scope)
      return client.updateTemplatePolicy({
        namespace: ns,
        policy: create(TemplatePolicySchema, {
          name,
          namespace: ns,
          displayName: params.displayName ?? '',
          description: params.description ?? '',
          rules: params.rules,
        }),
      })
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: templatePolicyListKey(scope) })
      queryClient.invalidateQueries({ queryKey: templatePolicyGetKey(scope, name) })
    },
  })
}

// useDeleteTemplatePolicy deletes a policy by name.
export function useDeleteTemplatePolicy(scope: TemplateScopeRef) {
  const transport = useTransport()
  const client = useMemo(
    () => createClient(TemplatePolicyService, transport),
    [transport],
  )
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: { name: string }) =>
      client.deleteTemplatePolicy({ namespace: namespaceForRef(scope), ...params }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: templatePolicyListKey(scope) })
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

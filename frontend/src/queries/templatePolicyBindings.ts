import { useMemo } from 'react'
import { create } from '@bufbuild/protobuf'
import { createClient } from '@connectrpc/connect'
import { useTransport } from '@connectrpc/connect-query'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  TemplatePolicyBindingService,
  TemplatePolicyBindingSchema,
  TemplatePolicyBindingTargetKind,
} from '@/gen/holos/console/v1/template_policy_bindings_pb.js'
import type {
  TemplatePolicyBinding,
  TemplatePolicyBindingTargetRef,
  LinkedTemplatePolicyRef,
} from '@/gen/holos/console/v1/template_policy_bindings_pb.js'
import type { TemplateScopeRef } from '@/gen/holos/console/v1/policy_state_pb.js'
import { useAuth } from '@/lib/auth'

// Re-export generated types/enums used by UI consumers.
export type { TemplatePolicyBinding, TemplatePolicyBindingTargetRef, LinkedTemplatePolicyRef }
export { TemplatePolicyBindingTargetKind }

/** Query key helper for the template policy bindings list at a given scope. */
function bindingListKey(scope: TemplateScopeRef) {
  return ['templatePolicyBindings', 'list', scope.scope, scope.scopeName] as const
}

/** Query key helper for a single template policy binding. */
function bindingGetKey(scope: TemplateScopeRef, name: string) {
  return ['templatePolicyBindings', 'get', scope.scope, scope.scopeName, name] as const
}

// useListTemplatePolicyBindings fetches all bindings visible within a scope.
// Mirrors the shape of useListTemplatePolicies in queries/templatePolicies.ts.
export function useListTemplatePolicyBindings(scope: TemplateScopeRef) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(
    () => createClient(TemplatePolicyBindingService, transport),
    [transport],
  )
  return useQuery({
    queryKey: bindingListKey(scope),
    queryFn: async () => {
      const response = await client.listTemplatePolicyBindings({ scope })
      return response.bindings
    },
    enabled: isAuthenticated && !!scope.scopeName,
  })
}

// useGetTemplatePolicyBinding fetches a single binding by name within a scope.
export function useGetTemplatePolicyBinding(scope: TemplateScopeRef, name: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(
    () => createClient(TemplatePolicyBindingService, transport),
    [transport],
  )
  return useQuery({
    queryKey: bindingGetKey(scope, name),
    queryFn: async () => {
      const response = await client.getTemplatePolicyBinding({ scope, name })
      return response.binding
    },
    enabled: isAuthenticated && !!scope.scopeName && !!name,
  })
}

// useCreateTemplatePolicyBinding creates a new binding at the given scope.
export function useCreateTemplatePolicyBinding(scope: TemplateScopeRef) {
  const transport = useTransport()
  const client = useMemo(
    () => createClient(TemplatePolicyBindingService, transport),
    [transport],
  )
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: {
      name: string
      displayName: string
      description: string
      policyRef: LinkedTemplatePolicyRef
      targetRefs: TemplatePolicyBindingTargetRef[]
    }) =>
      client.createTemplatePolicyBinding({
        scope,
        binding: create(TemplatePolicyBindingSchema, {
          name: params.name,
          scopeRef: scope,
          displayName: params.displayName,
          description: params.description,
          policyRef: params.policyRef,
          targetRefs: params.targetRefs,
        }),
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: bindingListKey(scope) })
    },
  })
}

// useUpdateTemplatePolicyBinding updates an existing binding.
export function useUpdateTemplatePolicyBinding(
  scope: TemplateScopeRef,
  name: string,
) {
  const transport = useTransport()
  const client = useMemo(
    () => createClient(TemplatePolicyBindingService, transport),
    [transport],
  )
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: {
      displayName?: string
      description?: string
      policyRef: LinkedTemplatePolicyRef
      targetRefs: TemplatePolicyBindingTargetRef[]
    }) =>
      client.updateTemplatePolicyBinding({
        scope,
        binding: create(TemplatePolicyBindingSchema, {
          name,
          scopeRef: scope,
          displayName: params.displayName ?? '',
          description: params.description ?? '',
          policyRef: params.policyRef,
          targetRefs: params.targetRefs,
        }),
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: bindingListKey(scope) })
      queryClient.invalidateQueries({ queryKey: bindingGetKey(scope, name) })
    },
  })
}

// useDeleteTemplatePolicyBinding deletes a binding by name.
export function useDeleteTemplatePolicyBinding(scope: TemplateScopeRef) {
  const transport = useTransport()
  const client = useMemo(
    () => createClient(TemplatePolicyBindingService, transport),
    [transport],
  )
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: { name: string }) =>
      client.deleteTemplatePolicyBinding({ scope, ...params }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: bindingListKey(scope) })
    },
  })
}

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
import { useAuth } from '@/lib/auth'

// Re-export generated types/enums used by UI consumers.
export type { TemplatePolicyBinding, TemplatePolicyBindingTargetRef, LinkedTemplatePolicyRef }
export { TemplatePolicyBindingTargetKind }

/** Query key helper for the template policy bindings list at a given namespace. */
function bindingListKey(namespace: string) {
  return ['templatePolicyBindings', 'list', namespace] as const
}

/** Query key helper for a single template policy binding. */
function bindingGetKey(namespace: string, name: string) {
  return ['templatePolicyBindings', 'get', namespace, name] as const
}

// useListTemplatePolicyBindings fetches all bindings visible within a namespace.
// Mirrors the shape of useListTemplatePolicies in queries/templatePolicies.ts.
export function useListTemplatePolicyBindings(namespace: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(
    () => createClient(TemplatePolicyBindingService, transport),
    [transport],
  )
  return useQuery({
    queryKey: bindingListKey(namespace),
    queryFn: async () => {
      const response = await client.listTemplatePolicyBindings({ namespace })
      return response.bindings
    },
    enabled: isAuthenticated && !!namespace,
  })
}

// useGetTemplatePolicyBinding fetches a single binding by name within a namespace.
export function useGetTemplatePolicyBinding(namespace: string, name: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(
    () => createClient(TemplatePolicyBindingService, transport),
    [transport],
  )
  return useQuery({
    queryKey: bindingGetKey(namespace, name),
    queryFn: async () => {
      const response = await client.getTemplatePolicyBinding({ namespace, name })
      return response.binding
    },
    enabled: isAuthenticated && !!namespace && !!name,
  })
}

// useCreateTemplatePolicyBinding creates a new binding at the given namespace.
export function useCreateTemplatePolicyBinding(namespace: string) {
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
    }) => {
      return client.createTemplatePolicyBinding({
        namespace,
        binding: create(TemplatePolicyBindingSchema, {
          name: params.name,
          namespace,
          displayName: params.displayName,
          description: params.description,
          policyRef: params.policyRef,
          targetRefs: params.targetRefs,
        }),
      })
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: bindingListKey(namespace) })
    },
  })
}

// useUpdateTemplatePolicyBinding updates an existing binding.
export function useUpdateTemplatePolicyBinding(
  namespace: string,
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
    }) => {
      return client.updateTemplatePolicyBinding({
        namespace,
        binding: create(TemplatePolicyBindingSchema, {
          name,
          namespace,
          displayName: params.displayName ?? '',
          description: params.description ?? '',
          policyRef: params.policyRef,
          targetRefs: params.targetRefs,
        }),
      })
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: bindingListKey(namespace) })
      queryClient.invalidateQueries({ queryKey: bindingGetKey(namespace, name) })
    },
  })
}

// useDeleteTemplatePolicyBinding deletes a binding by name.
export function useDeleteTemplatePolicyBinding(namespace: string) {
  const transport = useTransport()
  const client = useMemo(
    () => createClient(TemplatePolicyBindingService, transport),
    [transport],
  )
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: { name: string }) =>
      client.deleteTemplatePolicyBinding({ namespace, ...params }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: bindingListKey(namespace) })
    },
  })
}

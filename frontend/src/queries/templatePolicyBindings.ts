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
import { useListFolders } from '@/queries/folders'
import type { Folder } from '@/gen/holos/console/v1/folders_pb.js'
import { namespaceForFolder, namespaceForOrg } from '@/lib/scope-labels'
import { keys } from '@/queries/keys'
import {
  aggregateFanOut,
  type FanOutAggregate,
  type FanOutQueryState,
} from '@/queries/templatePolicies'

// Re-export generated types/enums used by UI consumers.
export type { TemplatePolicyBinding, TemplatePolicyBindingTargetRef, LinkedTemplatePolicyRef }
export { TemplatePolicyBindingTargetKind }

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
    queryKey: keys.templatePolicyBindings.list(namespace),
    queryFn: async () => {
      const response = await client.listTemplatePolicyBindings({ namespace })
      return response.bindings
    },
    enabled: isAuthenticated && !!namespace,
  })
}

// Module-level sentinel preserves reference identity across renders when the
// folders list is still pending or empty.
const EMPTY_FOLDERS: readonly Folder[] = []

// useAllTemplatePolicyBindingsForOrg fans a ListTemplatePolicyBindings call
// across every namespace reachable from an organization root — the org
// namespace plus one namespace per folder visible to the caller — and
// flattens the results into one array. Bindings live only at org or folder
// scope (HOL-590), so project namespaces are not fanned out.
//
// Used by the unified org-level Template Policy Bindings index (HOL-793).
// Semantics match useAllTemplatePoliciesForOrg; partial data + error is
// preserved so the caller can keep successfully-loaded rows visible while
// rendering a warning banner.
export function useAllTemplatePolicyBindingsForOrg(
  orgName: string,
): FanOutAggregate<TemplatePolicyBinding> {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(
    () => createClient(TemplatePolicyBindingService, transport),
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
      queryKey: keys.templatePolicyBindings.list(namespaceForFolder(folder.name)),
      queryFn: async (): Promise<TemplatePolicyBinding[]> => {
        const response = await client.listTemplatePolicyBindings({
          namespace: namespaceForFolder(folder.name),
        })
        return response.bindings
      },
      enabled: isAuthenticated && !!folder.name,
    })),
  })

  const orgQuery = useQuery({
    queryKey: keys.templatePolicyBindings.list(orgNamespace),
    queryFn: async () => {
      const response = await client.listTemplatePolicyBindings({
        namespace: orgNamespace,
      })
      return response.bindings
    },
    enabled: isAuthenticated && !!orgNamespace,
  })

  // Wrap the folders-list query as a FanOutQueryState<TemplatePolicyBinding[]>
  // with data=[] on success so other scopes' rows keep rendering when the
  // folders list itself errors.
  const foldersAsQuery: FanOutQueryState<TemplatePolicyBinding[]> = {
    data: foldersQuery.data === undefined ? undefined : [],
    error: foldersQuery.error,
    isPending: foldersQuery.isPending,
    fetchStatus: foldersQuery.fetchStatus,
  }

  return aggregateFanOut<TemplatePolicyBinding>([
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

// useGetTemplatePolicyBinding fetches a single binding by name within a namespace.
export function useGetTemplatePolicyBinding(namespace: string, name: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(
    () => createClient(TemplatePolicyBindingService, transport),
    [transport],
  )
  return useQuery({
    queryKey: keys.templatePolicyBindings.get(namespace, name),
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
      queryClient.invalidateQueries({ queryKey: keys.templatePolicyBindings.list(namespace) })
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
      queryClient.invalidateQueries({ queryKey: keys.templatePolicyBindings.list(namespace) })
      queryClient.invalidateQueries({ queryKey: keys.templatePolicyBindings.get(namespace, name) })
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
      queryClient.invalidateQueries({ queryKey: keys.templatePolicyBindings.list(namespace) })
    },
  })
}

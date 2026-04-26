// templateRequirements.ts — TanStack Query hooks for TemplateRequirementService
// (HOL-1013).
//
// TemplateRequirement lives in an organization or folder namespace. The hooks
// below back the org-scoped Requirements ResourceGrid page that appears under
// the Templates sidebar collapsible.

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
  TemplateRequirementService,
  TemplateRequirementSchema,
} from '@/gen/holos/console/v1/template_requirements_pb.js'
import type {
  TemplateRequirement,
  TemplateRequirementTargetRef,
} from '@/gen/holos/console/v1/template_requirements_pb.js'
import type { LinkedTemplateRef } from '@/gen/holos/console/v1/policy_state_pb.js'
import { useAuth } from '@/lib/auth'
import { keys } from '@/queries/keys'

// Re-export proto types so consumers import from one place.
export type { TemplateRequirement, TemplateRequirementTargetRef, LinkedTemplateRef }

// useListTemplateRequirements lists all TemplateRequirement resources in an
// organization or folder namespace. Backed by
// TemplateRequirementService.ListTemplateRequirements.
export function useListTemplateRequirements(namespace: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(
    () => createClient(TemplateRequirementService, transport),
    [transport],
  )
  return useQuery({
    queryKey: keys.templateRequirements.list(namespace),
    queryFn: async () => {
      const response = await client.listTemplateRequirements({ namespace })
      return response.requirements
    },
    enabled: isAuthenticated && !!namespace,
    placeholderData: keepPreviousData,
  })
}

// useDeleteTemplateRequirement deletes a TemplateRequirement by name.
export function useDeleteTemplateRequirement(namespace: string) {
  const transport = useTransport()
  const client = useMemo(
    () => createClient(TemplateRequirementService, transport),
    [transport],
  )
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: { name: string }) =>
      client.deleteTemplateRequirement({ namespace, ...params }),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: keys.templateRequirements.list(namespace),
      })
    },
  })
}

// useGetTemplateRequirement fetches a single TemplateRequirement by (namespace, name).
export function useGetTemplateRequirement(namespace: string, name: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(
    () => createClient(TemplateRequirementService, transport),
    [transport],
  )
  return useQuery({
    queryKey: keys.templateRequirements.get(namespace, name),
    queryFn: async () => {
      const response = await client.getTemplateRequirement({ namespace, name })
      return response.requirement
    },
    enabled: isAuthenticated && !!namespace && !!name,
  })
}

// useCreateTemplateRequirement creates a new TemplateRequirement in an org or folder namespace.
export function useCreateTemplateRequirement(namespace: string) {
  const transport = useTransport()
  const client = useMemo(
    () => createClient(TemplateRequirementService, transport),
    [transport],
  )
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: {
      name: string
      requires?: LinkedTemplateRef
      targetRefs?: TemplateRequirementTargetRef[]
      cascadeDelete?: boolean
    }) => {
      return client.createTemplateRequirement({
        namespace,
        requirement: create(TemplateRequirementSchema, {
          name: params.name,
          namespace,
          requires: params.requires,
          targetRefs: params.targetRefs ?? [],
          cascadeDelete: params.cascadeDelete,
        }),
      })
    },
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: keys.templateRequirements.list(namespace),
      })
    },
  })
}

// useUpdateTemplateRequirement updates an existing TemplateRequirement.
export function useUpdateTemplateRequirement(namespace: string, name: string) {
  const transport = useTransport()
  const client = useMemo(
    () => createClient(TemplateRequirementService, transport),
    [transport],
  )
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: {
      requires?: LinkedTemplateRef
      targetRefs?: TemplateRequirementTargetRef[]
      cascadeDelete?: boolean
    }) => {
      return client.updateTemplateRequirement({
        namespace,
        requirement: create(TemplateRequirementSchema, {
          name,
          namespace,
          requires: params.requires,
          targetRefs: params.targetRefs ?? [],
          cascadeDelete: params.cascadeDelete,
        }),
      })
    },
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: keys.templateRequirements.list(namespace),
      })
      queryClient.invalidateQueries({
        queryKey: keys.templateRequirements.get(namespace, name),
      })
    },
  })
}

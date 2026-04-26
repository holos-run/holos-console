// templateRequirements.ts — TanStack Query hooks for TemplateRequirementService
// (HOL-1013).
//
// TemplateRequirement lives in an organization or folder namespace. The hooks
// below back the org-scoped Requirements ResourceGrid page that appears under
// the Templates sidebar collapsible.

import { useMemo } from 'react'
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
} from '@/gen/holos/console/v1/template_requirements_pb.js'
import type { TemplateRequirement } from '@/gen/holos/console/v1/template_requirements_pb.js'
import { useAuth } from '@/lib/auth'
import { keys } from '@/queries/keys'

// Re-export proto types so consumers import from one place.
export type { TemplateRequirement }

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

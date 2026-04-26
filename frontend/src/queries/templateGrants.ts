// templateGrants.ts — TanStack Query hooks for TemplateGrantService (HOL-1013).
//
// TemplateGrant lives in an organization or folder namespace. The hooks below
// back the org-scoped Grants ResourceGrid page that appears under the
// Templates sidebar collapsible.

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
  TemplateGrantService,
  TemplateGrantSchema,
} from '@/gen/holos/console/v1/template_grants_pb.js'
import type {
  TemplateGrant,
  TemplateGrantFromRef,
  TemplateGrantToRef,
} from '@/gen/holos/console/v1/template_grants_pb.js'
import { useAuth } from '@/lib/auth'
import { keys } from '@/queries/keys'

// Re-export proto types so consumers import from one place.
export type { TemplateGrant, TemplateGrantFromRef, TemplateGrantToRef }

// useListTemplateGrants lists all TemplateGrant resources in an organization
// or folder namespace. Backed by TemplateGrantService.ListTemplateGrants.
export function useListTemplateGrants(namespace: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(
    () => createClient(TemplateGrantService, transport),
    [transport],
  )
  return useQuery({
    queryKey: keys.templateGrants.list(namespace),
    queryFn: async () => {
      const response = await client.listTemplateGrants({ namespace })
      return response.grants
    },
    enabled: isAuthenticated && !!namespace,
    placeholderData: keepPreviousData,
  })
}

// useDeleteTemplateGrant deletes a TemplateGrant by name.
export function useDeleteTemplateGrant(namespace: string) {
  const transport = useTransport()
  const client = useMemo(
    () => createClient(TemplateGrantService, transport),
    [transport],
  )
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: { name: string }) =>
      client.deleteTemplateGrant({ namespace, ...params }),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: keys.templateGrants.list(namespace),
      })
    },
  })
}

// useGetTemplateGrant fetches a single TemplateGrant by (namespace, name).
export function useGetTemplateGrant(namespace: string, name: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(
    () => createClient(TemplateGrantService, transport),
    [transport],
  )
  return useQuery({
    queryKey: keys.templateGrants.get(namespace, name),
    queryFn: async () => {
      const response = await client.getTemplateGrant({ namespace, name })
      return response.grant
    },
    enabled: isAuthenticated && !!namespace && !!name,
  })
}

// useCreateTemplateGrant creates a new TemplateGrant in an org or folder namespace.
export function useCreateTemplateGrant(namespace: string) {
  const transport = useTransport()
  const client = useMemo(
    () => createClient(TemplateGrantService, transport),
    [transport],
  )
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: {
      name: string
      from?: TemplateGrantFromRef[]
      to?: TemplateGrantToRef[]
    }) => {
      return client.createTemplateGrant({
        namespace,
        grant: create(TemplateGrantSchema, {
          name: params.name,
          namespace,
          from: params.from ?? [],
          to: params.to ?? [],
        }),
      })
    },
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: keys.templateGrants.list(namespace),
      })
    },
  })
}

// useUpdateTemplateGrant updates an existing TemplateGrant.
export function useUpdateTemplateGrant(namespace: string, name: string) {
  const transport = useTransport()
  const client = useMemo(
    () => createClient(TemplateGrantService, transport),
    [transport],
  )
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: {
      from?: TemplateGrantFromRef[]
      to?: TemplateGrantToRef[]
    }) => {
      return client.updateTemplateGrant({
        namespace,
        grant: create(TemplateGrantSchema, {
          name,
          namespace,
          from: params.from ?? [],
          to: params.to ?? [],
        }),
      })
    },
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: keys.templateGrants.list(namespace),
      })
      queryClient.invalidateQueries({
        queryKey: keys.templateGrants.get(namespace, name),
      })
    },
  })
}

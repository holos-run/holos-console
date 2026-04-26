import { useMemo } from 'react'
import { createClient } from '@connectrpc/connect'
import { useQuery, useTransport } from '@connectrpc/connect-query'
import { useQuery as useTanstackQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  OrganizationService,
} from '@/gen/holos/console/v1/organizations_pb.js'
import { useAuth } from '@/lib/auth'
import { keys } from '@/queries/keys'

export function useListOrganizations() {
  const { isAuthenticated } = useAuth()
  return useQuery(
    OrganizationService.method.listOrganizations,
    {},
    { enabled: isAuthenticated },
  )
}

/**
 * TanStack-native list hook for ResourceGrid v1 pages.
 * Uses keys.organizations.list() for consistent cache targeting.
 * keepPreviousData is intentionally omitted: the organizations list uses a
 * user-identity-agnostic key on a shared QueryClient, so KPD would return a
 * prior user's cached rows when a different user logs in (E2E cross-user
 * scenario). The ConnectRPC transport already carries the current user's auth
 * token, so the response is always scoped to the current session.
 */
export function useListOrganizationsKPD() {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(OrganizationService, transport), [transport])
  return useTanstackQuery({
    queryKey: keys.organizations.list(),
    queryFn: async () => {
      const response = await client.listOrganizations({})
      return response.organizations
    },
    enabled: isAuthenticated,
  })
}

export function useGetOrganization(name: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(OrganizationService, transport), [transport])
  return useTanstackQuery({
    queryKey: keys.organizations.get(name),
    queryFn: async () => {
      const response = await client.getOrganization({ name })
      return response.organization
    },
    enabled: isAuthenticated && name.length > 0,
  })
}

export function useGetOrganizationRaw(name: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(OrganizationService, transport), [transport])
  return useTanstackQuery({
    queryKey: keys.organizations.raw(name),
    queryFn: async () => {
      const response = await client.getOrganizationRaw({ name })
      return response.raw
    },
    enabled: isAuthenticated && name.length > 0,
  })
}

export function useCreateOrganization() {
  const transport = useTransport()
  const client = useMemo(() => createClient(OrganizationService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: { name: string; displayName?: string; description?: string; populateDefaults?: boolean }) =>
      client.createOrganization(params),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: keys.connect.all() })
    },
  })
}

export function useUpdateOrganization() {
  const transport = useTransport()
  const client = useMemo(() => createClient(OrganizationService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: {
      name: string
      displayName?: string
      description?: string
      defaultFolder?: string
      gatewayNamespace?: string
    }) => client.updateOrganization(params),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: keys.connect.all() })
    },
  })
}

export function useUpdateOrganizationSharing() {
  const transport = useTransport()
  const client = useMemo(() => createClient(OrganizationService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: {
      name: string
      userGrants: { principal: string; role: number }[]
      roleGrants: { principal: string; role: number }[]
    }) => client.updateOrganizationSharing(params),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: keys.connect.all() })
    },
  })
}

export function useUpdateOrganizationDefaultSharing() {
  const transport = useTransport()
  const client = useMemo(() => createClient(OrganizationService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: {
      name: string
      defaultUserGrants: { principal: string; role: number }[]
      defaultRoleGrants: { principal: string; role: number }[]
    }) => client.updateOrganizationDefaultSharing(params),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: keys.connect.all() })
    },
  })
}

export function useDeleteOrganization() {
  const transport = useTransport()
  const client = useMemo(() => createClient(OrganizationService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: { name: string }) => client.deleteOrganization(params),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: keys.connect.all() })
    },
  })
}

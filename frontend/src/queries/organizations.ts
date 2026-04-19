import { useMemo } from 'react'
import { createClient } from '@connectrpc/connect'
import { useQuery, useTransport } from '@connectrpc/connect-query'
import { useQuery as useTanstackQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  OrganizationService,
} from '@/gen/holos/console/v1/organizations_pb.js'
import { useAuth } from '@/lib/auth'

export function useListOrganizations() {
  const { isAuthenticated } = useAuth()
  return useQuery(
    OrganizationService.method.listOrganizations,
    {},
    { enabled: isAuthenticated },
  )
}

export function useGetOrganization(name: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(OrganizationService, transport), [transport])
  return useTanstackQuery({
    queryKey: ['connect-query', 'getOrganization', name],
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
    queryKey: ['connect-query', 'getOrganizationRaw', name],
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
      queryClient.invalidateQueries({ queryKey: ['connect-query'] })
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
      queryClient.invalidateQueries({ queryKey: ['connect-query'] })
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
      queryClient.invalidateQueries({ queryKey: ['connect-query'] })
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
      queryClient.invalidateQueries({ queryKey: ['connect-query'] })
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
      queryClient.invalidateQueries({ queryKey: ['connect-query'] })
    },
  })
}

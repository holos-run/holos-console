import { useMemo } from 'react'
import { createClient } from '@connectrpc/connect'
import { useQuery, useTransport } from '@connectrpc/connect-query'
import { useMutation, useQueryClient } from '@tanstack/react-query'
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

export function useCreateOrganization() {
  const transport = useTransport()
  const client = useMemo(() => createClient(OrganizationService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: { name: string; displayName?: string; description?: string }) =>
      client.createOrganization(params),
    onSuccess: () => {
      // Invalidate all connect-query keys so listOrganizations refetches
      queryClient.invalidateQueries({ queryKey: ['connect-query'] })
    },
  })
}

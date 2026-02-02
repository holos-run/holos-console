import { useQuery, useMutation } from '@connectrpc/connect-query'
import { createConnectQueryKey } from '@connectrpc/connect-query-core'
import { useQueryClient } from '@tanstack/react-query'
import {
  OrganizationService,
} from '../gen/holos/console/v1/organizations_pb.js'
import type { ListOrganizationsResponse, Organization } from '../gen/holos/console/v1/organizations_pb.js'

export function useListOrganizations() {
  return useQuery(
    OrganizationService.method.listOrganizations,
    {},
  )
}

export function useGetOrganization(name: string) {
  return useQuery(
    OrganizationService.method.getOrganization,
    { name },
  )
}

export function useDeleteOrganization() {
  const queryClient = useQueryClient()
  return useMutation(OrganizationService.method.deleteOrganization, {
    onMutate: async (variables) => {
      const listKey = createConnectQueryKey({
        schema: OrganizationService.method.listOrganizations,
        cardinality: 'finite',
      })
      await queryClient.cancelQueries({ queryKey: listKey })

      const previousQueries = queryClient.getQueriesData<ListOrganizationsResponse>({ queryKey: listKey })

      queryClient.setQueriesData<ListOrganizationsResponse>(
        { queryKey: listKey },
        (old) => {
          if (!old) return old
          return {
            ...old,
            organizations: old.organizations.filter((o: Organization) => o.name !== variables.name),
          }
        },
      )

      return { previousQueries }
    },
    onError: (_err, _variables, context) => {
      if (context?.previousQueries) {
        for (const [key, data] of context.previousQueries) {
          queryClient.setQueryData(key, data)
        }
      }
    },
    onSettled: () => {
      const listKey = createConnectQueryKey({
        schema: OrganizationService.method.listOrganizations,
        cardinality: 'finite',
      })
      queryClient.invalidateQueries({ queryKey: listKey })
    },
  })
}

export function useCreateOrganization() {
  const queryClient = useQueryClient()
  return useMutation(OrganizationService.method.createOrganization, {
    onSuccess: () => {
      const listKey = createConnectQueryKey({
        schema: OrganizationService.method.listOrganizations,
        cardinality: 'finite',
      })
      queryClient.invalidateQueries({ queryKey: listKey })
    },
  })
}

export function useUpdateOrganization() {
  return useMutation(OrganizationService.method.updateOrganization)
}

export function useUpdateOrganizationSharing() {
  return useMutation(OrganizationService.method.updateOrganizationSharing)
}

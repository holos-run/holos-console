import { useQuery } from '@connectrpc/connect-query'
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

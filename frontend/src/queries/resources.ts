import { create } from '@bufbuild/protobuf'
import { useQuery } from '@connectrpc/connect-query'
import {
  ListResourcesRequestSchema,
  ResourceService,
  type ResourceType,
} from '@/gen/holos/console/v1/resources_pb.js'
import { useAuth } from '@/lib/auth'

export function useListResources(
  organization: string,
  types: ResourceType[] = [],
) {
  const { isAuthenticated } = useAuth()
  return useQuery(
    ResourceService.method.listResources,
    create(ListResourcesRequestSchema, { organization, types }),
    { enabled: isAuthenticated && !!organization },
  )
}

import { create } from '@bufbuild/protobuf'
import { useQuery } from '@connectrpc/connect-query'
import {
  ListProjectsRequestSchema,
  ProjectService,
} from '@/gen/holos/console/v1/projects_pb.js'
import { useAuth } from '@/lib/auth'

export function useListProjects(organization: string) {
  const { isAuthenticated } = useAuth()
  return useQuery(
    ProjectService.method.listProjects,
    create(ListProjectsRequestSchema, { organization }),
    { enabled: isAuthenticated && !!organization },
  )
}

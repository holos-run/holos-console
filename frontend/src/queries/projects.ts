import { useMemo } from 'react'
import { create } from '@bufbuild/protobuf'
import { createClient } from '@connectrpc/connect'
import { useQuery, useTransport } from '@connectrpc/connect-query'
import { useMutation, useQueryClient } from '@tanstack/react-query'
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

export function useCreateProject() {
  const transport = useTransport()
  const client = useMemo(() => createClient(ProjectService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: { name: string; displayName?: string; description?: string; organization: string }) =>
      client.createProject(params),
    onSuccess: () => {
      // Invalidate all connect-query keys so listProjects refetches
      queryClient.invalidateQueries({ queryKey: ['connect-query'] })
    },
  })
}

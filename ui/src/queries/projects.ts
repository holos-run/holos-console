import { create } from '@bufbuild/protobuf'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import { createConnectQueryKey } from '@connectrpc/connect-query-core'
import { useQueryClient } from '@tanstack/react-query'
import {
  ListProjectsRequestSchema,
  ProjectService,
} from '../gen/holos/console/v1/projects_pb.js'
import type { ListProjectsResponse, Project } from '../gen/holos/console/v1/projects_pb.js'

export function useListProjects(organization: string) {
  return useQuery(
    ProjectService.method.listProjects,
    create(ListProjectsRequestSchema, { organization }),
  )
}

export function useGetProject(name: string) {
  return useQuery(
    ProjectService.method.getProject,
    { name },
  )
}

export function useDeleteProject() {
  const queryClient = useQueryClient()
  return useMutation(ProjectService.method.deleteProject, {
    onMutate: async (variables) => {
      const listKey = createConnectQueryKey({
        schema: ProjectService.method.listProjects,
        cardinality: 'finite',
      })
      await queryClient.cancelQueries({ queryKey: listKey })

      const previousQueries = queryClient.getQueriesData<ListProjectsResponse>({ queryKey: listKey })

      queryClient.setQueriesData<ListProjectsResponse>(
        { queryKey: listKey },
        (old) => {
          if (!old) return old
          return {
            ...old,
            projects: old.projects.filter((p: Project) => p.name !== variables.name),
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
        schema: ProjectService.method.listProjects,
        cardinality: 'finite',
      })
      queryClient.invalidateQueries({ queryKey: listKey })
    },
  })
}

export function useCreateProject() {
  const queryClient = useQueryClient()
  return useMutation(ProjectService.method.createProject, {
    onSuccess: () => {
      const listKey = createConnectQueryKey({
        schema: ProjectService.method.listProjects,
        cardinality: 'finite',
      })
      queryClient.invalidateQueries({ queryKey: listKey })
    },
  })
}

import { useMemo } from 'react'
import { create } from '@bufbuild/protobuf'
import { createClient } from '@connectrpc/connect'
import { useQuery, useTransport } from '@connectrpc/connect-query'
import { useQuery as useTanstackQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  ListProjectsRequestSchema,
  ProjectService,
} from '@/gen/holos/console/v1/projects_pb.js'
import type { ParentType } from '@/gen/holos/console/v1/folders_pb.js'
import { useAuth } from '@/lib/auth'

export function useListProjects(organization: string) {
  const { isAuthenticated } = useAuth()
  return useQuery(
    ProjectService.method.listProjects,
    create(ListProjectsRequestSchema, { organization }),
    { enabled: isAuthenticated && !!organization },
  )
}

export function useGetProject(name: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(ProjectService, transport), [transport])
  return useTanstackQuery({
    queryKey: ['connect-query', 'getProject', name],
    queryFn: async () => {
      const response = await client.getProject({ name })
      return response.project
    },
    enabled: isAuthenticated && name.length > 0,
  })
}

export function useCreateProject() {
  const transport = useTransport()
  const client = useMemo(() => createClient(ProjectService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: { name: string; displayName?: string; description?: string; organization: string; parentType?: number; parentName?: string }) =>
      client.createProject(params),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['connect-query'] })
    },
  })
}

export function useUpdateProject() {
  const transport = useTransport()
  const client = useMemo(() => createClient(ProjectService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: { name: string; displayName?: string; description?: string; parentType?: ParentType; parentName?: string }) =>
      client.updateProject(params),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['connect-query'] })
    },
  })
}

export function useUpdateProjectSharing() {
  const transport = useTransport()
  const client = useMemo(() => createClient(ProjectService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: {
      name: string
      userGrants: { principal: string; role: number }[]
      roleGrants: { principal: string; role: number }[]
    }) => client.updateProjectSharing(params),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['connect-query'] })
    },
  })
}

export function useUpdateProjectDefaultSharing() {
  const transport = useTransport()
  const client = useMemo(() => createClient(ProjectService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: {
      name: string
      defaultUserGrants: { principal: string; role: number }[]
      defaultRoleGrants: { principal: string; role: number }[]
    }) => client.updateProjectDefaultSharing(params),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['connect-query'] })
    },
  })
}

export function useDeleteProject() {
  const transport = useTransport()
  const client = useMemo(() => createClient(ProjectService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: { name: string }) => client.deleteProject(params),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['connect-query'] })
    },
  })
}

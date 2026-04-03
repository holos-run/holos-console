import { useMemo } from 'react'
import { createClient } from '@connectrpc/connect'
import { useTransport } from '@connectrpc/connect-query'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { DeploymentService } from '@/gen/holos/console/v1/deployments_pb.js'
import { useAuth } from '@/lib/auth'

function deploymentListKey(project: string) {
  return ['deployments', 'list', project] as const
}

function deploymentGetKey(project: string, name: string) {
  return ['deployments', 'get', project, name] as const
}

export function useListDeployments(project: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(DeploymentService, transport), [transport])
  return useQuery({
    queryKey: deploymentListKey(project),
    queryFn: async () => {
      const response = await client.listDeployments({ project })
      return response.deployments
    },
    enabled: isAuthenticated && !!project,
  })
}

export function useGetDeployment(project: string, name: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(DeploymentService, transport), [transport])
  return useQuery({
    queryKey: deploymentGetKey(project, name),
    queryFn: async () => {
      const response = await client.getDeployment({ project, name })
      return response.deployment
    },
    enabled: isAuthenticated && !!project && !!name,
  })
}

export function useCreateDeployment(project: string) {
  const transport = useTransport()
  const client = useMemo(() => createClient(DeploymentService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: { name: string; image: string; tag: string; template: string; displayName?: string; description?: string }) =>
      client.createDeployment({ project, ...params }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: deploymentListKey(project) })
    },
  })
}

export function useUpdateDeployment(project: string, name: string) {
  const transport = useTransport()
  const client = useMemo(() => createClient(DeploymentService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: { image?: string; tag?: string; displayName?: string; description?: string }) =>
      client.updateDeployment({ project, name, ...params }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: deploymentListKey(project) })
      queryClient.invalidateQueries({ queryKey: deploymentGetKey(project, name) })
    },
  })
}

export function useDeleteDeployment(project: string) {
  const transport = useTransport()
  const client = useMemo(() => createClient(DeploymentService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: { name: string }) =>
      client.deleteDeployment({ project, ...params }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: deploymentListKey(project) })
    },
  })
}

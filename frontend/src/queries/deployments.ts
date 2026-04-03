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

function deploymentStatusKey(project: string, name: string) {
  return ['deployments', 'status', project, name] as const
}

function deploymentLogsKey(project: string, name: string, container?: string, tailLines?: number, previous?: boolean) {
  return ['deployments', 'logs', project, name, container, tailLines, previous] as const
}

export function useGetDeploymentStatus(project: string, name: string, options?: { refetchInterval?: number }) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(DeploymentService, transport), [transport])
  return useQuery({
    queryKey: deploymentStatusKey(project, name),
    queryFn: async () => {
      const response = await client.getDeploymentStatus({ project, name })
      return response.status
    },
    enabled: isAuthenticated && !!project && !!name,
    refetchInterval: options?.refetchInterval,
  })
}

export function useGetDeploymentLogs(
  project: string,
  name: string,
  options?: { container?: string; tailLines?: number; previous?: boolean },
) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(DeploymentService, transport), [transport])
  return useQuery({
    queryKey: deploymentLogsKey(project, name, options?.container, options?.tailLines, options?.previous),
    queryFn: async () => {
      const response = await client.getDeploymentLogs({
        project,
        name,
        container: options?.container ?? '',
        tailLines: options?.tailLines ?? 0,
        previous: options?.previous ?? false,
      })
      return response.logs
    },
    enabled: isAuthenticated && !!project && !!name,
  })
}

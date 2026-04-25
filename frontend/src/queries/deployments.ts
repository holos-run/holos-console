import { useMemo } from 'react'
import { createClient } from '@connectrpc/connect'
import { useTransport } from '@connectrpc/connect-query'
import { keepPreviousData, useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { DeploymentService } from '@/gen/holos/console/v1/deployments_pb.js'
import type { EnvVar } from '@/gen/holos/console/v1/deployments_pb.js'
import { useAuth } from '@/lib/auth'
import { keys } from '@/queries/keys'

export function useListDeployments(project: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(DeploymentService, transport), [transport])
  return useQuery({
    queryKey: keys.deployments.list(project),
    queryFn: async () => {
      const response = await client.listDeployments({ project })
      return response.deployments
    },
    enabled: isAuthenticated && !!project,
    placeholderData: keepPreviousData,
  })
}

export function useGetDeployment(project: string, name: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(DeploymentService, transport), [transport])
  return useQuery({
    queryKey: keys.deployments.get(project, name),
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
    mutationFn: (params: { name: string; image: string; tag: string; template: string; displayName?: string; description?: string; port?: number; command?: string[]; args?: string[]; env?: EnvVar[] }) =>
      client.createDeployment({ project, ...params }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: keys.deployments.list(project) })
    },
  })
}

export function useUpdateDeployment(project: string, name: string) {
  const transport = useTransport()
  const client = useMemo(() => createClient(DeploymentService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: { image?: string; tag?: string; displayName?: string; description?: string; port?: number; command?: string[]; args?: string[]; env?: EnvVar[] }) =>
      client.updateDeployment({ project, name, ...params }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: keys.deployments.list(project) })
      queryClient.invalidateQueries({ queryKey: keys.deployments.get(project, name) })
      // HOL-559: a successful UpdateDeployment re-renders against the
      // current TemplatePolicy chain and records a fresh applied render
      // set on the backend. Invalidate the policy-state query so the
      // UI's drift badge + diff refresh from the authoritative state
      // rather than continuing to show the stale "drifted" snapshot.
      queryClient.invalidateQueries({ queryKey: keys.deployments.policyState(project, name) })
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
      queryClient.invalidateQueries({ queryKey: keys.deployments.list(project) })
    },
  })
}

export function useGetDeploymentStatusSummary(
  project: string,
  name: string,
  options?: { refetchInterval?: number },
) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(DeploymentService, transport), [transport])
  return useQuery({
    queryKey: keys.deployments.statusSummary(project, name),
    queryFn: async () => {
      const response = await client.getDeploymentStatusSummary({ project, name })
      return response.summary
    },
    enabled: isAuthenticated && !!project && !!name,
    refetchInterval: options?.refetchInterval,
  })
}

export function useGetDeploymentStatus(project: string, name: string, options?: { refetchInterval?: number }) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(DeploymentService, transport), [transport])
  return useQuery({
    queryKey: keys.deployments.status(project, name),
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
    queryKey: keys.deployments.logs(project, name, options?.container, options?.tailLines, options?.previous),
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

export function useGetDeploymentRenderPreview(project: string, name: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(DeploymentService, transport), [transport])
  return useQuery({
    queryKey: keys.deployments.renderPreview(project, name),
    queryFn: async () => {
      const response = await client.getDeploymentRenderPreview({ project, name })
      return response
    },
    enabled: isAuthenticated && !!project && !!name,
  })
}

// useGetDeploymentPolicyState fetches the TemplatePolicy drift snapshot for
// a deployment (HOL-567). The response's PolicyState is sourced from the
// folder-namespace render-state store — see PolicySection's component-level
// comment for the storage-isolation guarantee. This hook is the sole read
// path used by the drift UI; never infer drift from other deployment
// fields.
export function useGetDeploymentPolicyState(project: string, name: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(DeploymentService, transport), [transport])
  return useQuery({
    queryKey: keys.deployments.policyState(project, name),
    queryFn: async () => {
      const response = await client.getDeploymentPolicyState({ project, name })
      return response.state
    },
    enabled: isAuthenticated && !!project && !!name,
  })
}

export function useListNamespaceSecrets(project: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(DeploymentService, transport), [transport])
  return useQuery({
    queryKey: keys.deployments.namespaceSecrets(project),
    queryFn: async () => {
      const response = await client.listNamespaceSecrets({ project })
      return response.secrets
    },
    enabled: isAuthenticated && !!project,
  })
}

export function useListNamespaceConfigMaps(project: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(DeploymentService, transport), [transport])
  return useQuery({
    queryKey: keys.deployments.namespaceConfigMaps(project),
    queryFn: async () => {
      const response = await client.listNamespaceConfigMaps({ project })
      return response.configMaps
    },
    enabled: isAuthenticated && !!project,
  })
}

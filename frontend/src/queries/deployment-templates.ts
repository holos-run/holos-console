import { useMemo } from 'react'
import { createClient } from '@connectrpc/connect'
import { useTransport } from '@connectrpc/connect-query'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { DeploymentTemplateService } from '@/gen/holos/console/v1/deployment_templates_pb.js'
import { useAuth } from '@/lib/auth'

function templateListKey(project: string) {
  return ['deployment-templates', 'list', project] as const
}

function templateGetKey(project: string, name: string) {
  return ['deployment-templates', 'get', project, name] as const
}

export function useListDeploymentTemplates(project: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(DeploymentTemplateService, transport), [transport])
  return useQuery({
    queryKey: templateListKey(project),
    queryFn: async () => {
      const response = await client.listDeploymentTemplates({ project })
      return response.templates
    },
    enabled: isAuthenticated && !!project,
  })
}

export function useGetDeploymentTemplate(project: string, name: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(DeploymentTemplateService, transport), [transport])
  return useQuery({
    queryKey: templateGetKey(project, name),
    queryFn: async () => {
      const response = await client.getDeploymentTemplate({ project, name })
      return response.template
    },
    enabled: isAuthenticated && !!project && !!name,
  })
}

export function useCreateDeploymentTemplate(project: string) {
  const transport = useTransport()
  const client = useMemo(() => createClient(DeploymentTemplateService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: { name: string; displayName: string; description: string; cueTemplate: string }) =>
      client.createDeploymentTemplate({ project, ...params }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: templateListKey(project) })
    },
  })
}

export function useUpdateDeploymentTemplate(project: string, name: string) {
  const transport = useTransport()
  const client = useMemo(() => createClient(DeploymentTemplateService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: { displayName?: string; description?: string; cueTemplate?: string }) =>
      client.updateDeploymentTemplate({ project, name, ...params }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: templateListKey(project) })
      queryClient.invalidateQueries({ queryKey: templateGetKey(project, name) })
    },
  })
}

export function useDeleteDeploymentTemplate(project: string) {
  const transport = useTransport()
  const client = useMemo(() => createClient(DeploymentTemplateService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: { name: string }) =>
      client.deleteDeploymentTemplate({ project, ...params }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: templateListKey(project) })
    },
  })
}

export function useCloneDeploymentTemplate(project: string) {
  const transport = useTransport()
  const client = useMemo(() => createClient(DeploymentTemplateService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: { sourceName: string; name: string; displayName: string }) =>
      client.cloneDeploymentTemplate({ project, ...params }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: templateListKey(project) })
    },
  })
}

export function useRenderDeploymentTemplate(
  cueTemplate: string,
  cueInput = '',
  enabled = true,
  cueSystemInput = '',
) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(DeploymentTemplateService, transport), [transport])
  return useQuery({
    queryKey: ['deployment-templates', 'render', cueTemplate, cueInput, cueSystemInput] as const,
    queryFn: async () => {
      const response = await client.renderDeploymentTemplate({
        cueTemplate,
        cueInput,
        cueSystemInput,
      })
      return { renderedYaml: response.renderedYaml, renderedJson: response.renderedJson }
    },
    enabled: isAuthenticated && !!cueTemplate && enabled,
    retry: false,
  })
}

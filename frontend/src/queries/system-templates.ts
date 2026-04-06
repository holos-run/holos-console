import { useMemo } from 'react'
import { createClient } from '@connectrpc/connect'
import { useTransport } from '@connectrpc/connect-query'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { SystemTemplateService } from '@/gen/holos/console/v1/system_templates_pb.js'
import { useAuth } from '@/lib/auth'

function systemTemplateListKey(org: string) {
  return ['system-templates', 'list', org] as const
}

function systemTemplateGetKey(org: string, name: string) {
  return ['system-templates', 'get', org, name] as const
}

export function useListSystemTemplates(org: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(SystemTemplateService, transport), [transport])
  return useQuery({
    queryKey: systemTemplateListKey(org),
    queryFn: async () => {
      const response = await client.listSystemTemplates({ org })
      return response.templates
    },
    enabled: isAuthenticated && !!org,
  })
}

export function useGetSystemTemplate(org: string, name: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(SystemTemplateService, transport), [transport])
  return useQuery({
    queryKey: systemTemplateGetKey(org, name),
    queryFn: async () => {
      const response = await client.getSystemTemplate({ org, name })
      return response.template
    },
    enabled: isAuthenticated && !!org && !!name,
  })
}

export function useCreateSystemTemplate(org: string) {
  const transport = useTransport()
  const client = useMemo(() => createClient(SystemTemplateService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: { name: string; displayName: string; description: string; cueTemplate: string; mandatory: boolean; enabled: boolean }) =>
      client.createSystemTemplate({ org, ...params }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: systemTemplateListKey(org) })
    },
  })
}

export function useUpdateSystemTemplate(org: string, name: string) {
  const transport = useTransport()
  const client = useMemo(() => createClient(SystemTemplateService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: { displayName?: string; description?: string; cueTemplate?: string; mandatory?: boolean; enabled?: boolean }) =>
      client.updateSystemTemplate({ org, name, ...params }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: systemTemplateListKey(org) })
      queryClient.invalidateQueries({ queryKey: systemTemplateGetKey(org, name) })
    },
  })
}

export function useDeleteSystemTemplate(org: string) {
  const transport = useTransport()
  const client = useMemo(() => createClient(SystemTemplateService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: { name: string }) =>
      client.deleteSystemTemplate({ org, ...params }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: systemTemplateListKey(org) })
    },
  })
}

export function useCloneSystemTemplate(org: string) {
  const transport = useTransport()
  const client = useMemo(() => createClient(SystemTemplateService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: { sourceName: string; name: string; displayName: string }) =>
      client.cloneSystemTemplate({ org, ...params }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: systemTemplateListKey(org) })
    },
  })
}

export function useRenderSystemTemplate(
  cueTemplate: string,
  cueInput = '',
  enabled = true,
  cueSystemInput = '',
) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(SystemTemplateService, transport), [transport])
  return useQuery({
    queryKey: ['system-templates', 'render', cueTemplate, cueInput, cueSystemInput] as const,
    queryFn: async () => {
      const response = await client.renderSystemTemplate({
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

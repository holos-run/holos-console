import { useMemo } from 'react'
import { createClient } from '@connectrpc/connect'
import { useTransport } from '@connectrpc/connect-query'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { OrgTemplateService } from '@/gen/holos/console/v1/org_templates_pb.js'
import { useAuth } from '@/lib/auth'

function orgTemplateListKey(org: string) {
  return ['org-templates', 'list', org] as const
}

function orgTemplateGetKey(org: string, name: string) {
  return ['org-templates', 'get', org, name] as const
}

export function useListOrgTemplates(org: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(OrgTemplateService, transport), [transport])
  return useQuery({
    queryKey: orgTemplateListKey(org),
    queryFn: async () => {
      const response = await client.listOrgTemplates({ org })
      return response.templates
    },
    enabled: isAuthenticated && !!org,
  })
}

export function useGetOrgTemplate(org: string, name: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(OrgTemplateService, transport), [transport])
  return useQuery({
    queryKey: orgTemplateGetKey(org, name),
    queryFn: async () => {
      const response = await client.getOrgTemplate({ org, name })
      return response.template
    },
    enabled: isAuthenticated && !!org && !!name,
  })
}

export function useCreateOrgTemplate(org: string) {
  const transport = useTransport()
  const client = useMemo(() => createClient(OrgTemplateService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: { name: string; displayName: string; description: string; cueTemplate: string; mandatory: boolean; enabled: boolean }) =>
      client.createOrgTemplate({ org, ...params }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: orgTemplateListKey(org) })
    },
  })
}

export function useUpdateOrgTemplate(org: string, name: string) {
  const transport = useTransport()
  const client = useMemo(() => createClient(OrgTemplateService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: { displayName?: string; description?: string; cueTemplate?: string; mandatory?: boolean; enabled?: boolean }) =>
      client.updateOrgTemplate({ org, name, ...params }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: orgTemplateListKey(org) })
      queryClient.invalidateQueries({ queryKey: orgTemplateGetKey(org, name) })
    },
  })
}

export function useDeleteOrgTemplate(org: string) {
  const transport = useTransport()
  const client = useMemo(() => createClient(OrgTemplateService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: { name: string }) =>
      client.deleteOrgTemplate({ org, ...params }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: orgTemplateListKey(org) })
    },
  })
}

export function useCloneOrgTemplate(org: string) {
  const transport = useTransport()
  const client = useMemo(() => createClient(OrgTemplateService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: { sourceName: string; name: string; displayName: string }) =>
      client.cloneOrgTemplate({ org, ...params }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: orgTemplateListKey(org) })
    },
  })
}

export function useRenderOrgTemplate(
  cueTemplate: string,
  cueInput = '',
  enabled = true,
  cuePlatformInput = '',
) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(OrgTemplateService, transport), [transport])
  return useQuery({
    queryKey: ['org-templates', 'render', cueTemplate, cueInput, cuePlatformInput] as const,
    queryFn: async () => {
      const response = await client.renderOrgTemplate({
        cueTemplate,
        cueInput,
        cuePlatformInput,
      })
      return { renderedYaml: response.renderedYaml, renderedJson: response.renderedJson }
    },
    enabled: isAuthenticated && !!cueTemplate && enabled,
    retry: false,
  })
}

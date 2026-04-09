import { useMemo } from 'react'
import { createClient } from '@connectrpc/connect'
import { useTransport } from '@connectrpc/connect-query'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { TemplateService } from '@/gen/holos/console/v1/templates_pb.js'
import type { TemplateScopeRef, LinkedTemplateRef, TemplateDefaults } from '@/gen/holos/console/v1/templates_pb.js'
import { useAuth } from '@/lib/auth'

// Re-export types used by consumers.
export type { TemplateScopeRef, LinkedTemplateRef, TemplateDefaults }

function templateListKey(scope: TemplateScopeRef) {
  return ['templates', 'list', scope.scope, scope.scopeName] as const
}

function templateGetKey(scope: TemplateScopeRef, name: string) {
  return ['templates', 'get', scope.scope, scope.scopeName, name] as const
}

export function useListTemplates(scope: TemplateScopeRef) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(TemplateService, transport), [transport])
  return useQuery({
    queryKey: templateListKey(scope),
    queryFn: async () => {
      const response = await client.listTemplates({ scope })
      return response.templates
    },
    enabled: isAuthenticated && !!scope.scopeName,
  })
}

export function useGetTemplate(scope: TemplateScopeRef, name: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(TemplateService, transport), [transport])
  return useQuery({
    queryKey: templateGetKey(scope, name),
    queryFn: async () => {
      const response = await client.getTemplate({ scope, name })
      return response.template
    },
    enabled: isAuthenticated && !!scope.scopeName && !!name,
  })
}

export function useCreateTemplate(scope: TemplateScopeRef) {
  const transport = useTransport()
  const client = useMemo(() => createClient(TemplateService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: {
      name: string
      displayName: string
      description: string
      cueTemplate: string
      linkedTemplates?: LinkedTemplateRef[]
      mandatory?: boolean
      enabled?: boolean
    }) =>
      client.createTemplate({
        scope,
        template: {
          name: params.name,
          scopeRef: scope,
          displayName: params.displayName,
          description: params.description,
          cueTemplate: params.cueTemplate,
          linkedTemplates: params.linkedTemplates ?? [],
          mandatory: params.mandatory ?? false,
          enabled: params.enabled ?? false,
        },
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: templateListKey(scope) })
    },
  })
}

export function useUpdateTemplate(scope: TemplateScopeRef, name: string) {
  const transport = useTransport()
  const client = useMemo(() => createClient(TemplateService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: {
      displayName?: string
      description?: string
      cueTemplate?: string
      linkedTemplates?: LinkedTemplateRef[]
      mandatory?: boolean
      enabled?: boolean
    }) =>
      client.updateTemplate({
        scope,
        template: {
          name,
          scopeRef: scope,
          ...params,
          linkedTemplates: params.linkedTemplates ?? [],
        },
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: templateListKey(scope) })
      queryClient.invalidateQueries({ queryKey: templateGetKey(scope, name) })
    },
  })
}

export function useDeleteTemplate(scope: TemplateScopeRef) {
  const transport = useTransport()
  const client = useMemo(() => createClient(TemplateService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: { name: string }) =>
      client.deleteTemplate({ scope, ...params }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: templateListKey(scope) })
    },
  })
}

export function useCloneTemplate(scope: TemplateScopeRef) {
  const transport = useTransport()
  const client = useMemo(() => createClient(TemplateService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: { sourceName: string; name: string; displayName: string }) =>
      client.cloneTemplate({ scope, ...params }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: templateListKey(scope) })
    },
  })
}

function linkableTemplatesKey(scope: TemplateScopeRef) {
  return ['templates', 'linkable', scope.scope, scope.scopeName] as const
}

export function useListLinkableTemplates(scope: TemplateScopeRef) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(TemplateService, transport), [transport])
  return useQuery({
    queryKey: linkableTemplatesKey(scope),
    queryFn: async () => {
      const response = await client.listLinkableTemplates({ scope })
      return response.templates
    },
    enabled: isAuthenticated && !!scope.scopeName,
  })
}

export function useRenderTemplate(
  scope: TemplateScopeRef,
  cueTemplate: string,
  cueProjectInput = '',
  enabled = true,
  cuePlatformInput = '',
) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(TemplateService, transport), [transport])
  return useQuery({
    queryKey: ['templates', 'render', scope.scope, scope.scopeName, cueTemplate, cueProjectInput, cuePlatformInput] as const,
    queryFn: async () => {
      const response = await client.renderTemplate({
        scope,
        cueTemplate,
        cueProjectInput,
        cuePlatformInput,
      })
      return { renderedYaml: response.renderedYaml, renderedJson: response.renderedJson }
    },
    enabled: isAuthenticated && !!cueTemplate && enabled,
    retry: false,
  })
}

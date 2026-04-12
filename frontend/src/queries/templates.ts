import { useMemo } from 'react'
import { create } from '@bufbuild/protobuf'
import { createClient } from '@connectrpc/connect'
import { useTransport } from '@connectrpc/connect-query'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  TemplateService,
  TemplateScopeRefSchema,
  TemplateScope,
} from '@/gen/holos/console/v1/templates_pb.js'
import type { TemplateScopeRef, LinkableTemplate, LinkedTemplateRef } from '@/gen/holos/console/v1/templates_pb.js'
import { useAuth } from '@/lib/auth'

// Re-export types used by consumers.
export type { TemplateScopeRef, LinkableTemplate, LinkedTemplateRef }
export { TemplateScope }

// makeScope is a helper to build a TemplateScopeRef from scope and scopeName.
export function makeScope(scope: TemplateScope, scopeName: string): TemplateScopeRef {
  return create(TemplateScopeRefSchema, { scope, scopeName })
}

// makeOrgScope builds a TemplateScopeRef for an organization-scoped template.
export function makeOrgScope(org: string): TemplateScopeRef {
  return makeScope(TemplateScope.ORGANIZATION, org)
}

// makeProjectScope builds a TemplateScopeRef for a project-scoped template.
export function makeProjectScope(project: string): TemplateScopeRef {
  return makeScope(TemplateScope.PROJECT, project)
}

function templateListKey(scope: TemplateScopeRef) {
  return ['templates', 'list', scope.scope, scope.scopeName] as const
}

function templateGetKey(scope: TemplateScopeRef, name: string) {
  return ['templates', 'get', scope.scope, scope.scopeName, name] as const
}

function linkableTemplatesKey(scope: TemplateScopeRef) {
  return ['templates', 'linkable', scope.scope, scope.scopeName] as const
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
      mandatory?: boolean
      enabled?: boolean
      linkedTemplates?: LinkedTemplateRef[]
    }) =>
      client.createTemplate({
        scope,
        template: {
          name: params.name,
          scopeRef: scope,
          displayName: params.displayName,
          description: params.description,
          cueTemplate: params.cueTemplate,
          mandatory: params.mandatory ?? false,
          enabled: params.enabled ?? false,
          linkedTemplates: params.linkedTemplates ?? [],
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
      mandatory?: boolean
      enabled?: boolean
      linkedTemplates?: LinkedTemplateRef[]
    }) =>
      client.updateTemplate({
        scope,
        template: {
          name,
          scopeRef: scope,
          displayName: params.displayName ?? '',
          description: params.description ?? '',
          cueTemplate: params.cueTemplate ?? '',
          mandatory: params.mandatory ?? false,
          enabled: params.enabled ?? false,
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
    mutationFn: (params: { name: string }) => client.deleteTemplate({ scope, ...params }),
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

// useListLinkableTemplates returns enabled ancestor templates that can be
// explicitly linked to templates at the given scope.
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

// useRenderTemplate renders a CUE template with the given inputs. The scope
// parameter determines which ancestor platform templates are resolved.
export function useRenderTemplate(
  scope: TemplateScopeRef,
  cueTemplate: string,
  cueInput = '',
  enabled = true,
  cuePlatformInput = '',
) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(TemplateService, transport), [transport])
  return useQuery({
    queryKey: ['templates', 'render', scope.scope, scope.scopeName, cueTemplate, cueInput, cuePlatformInput] as const,
    queryFn: async () => {
      const response = await client.renderTemplate({
        scope,
        cueTemplate,
        cueProjectInput: cueInput,
        cuePlatformInput,
      })
      return { renderedYaml: response.renderedYaml, renderedJson: response.renderedJson }
    },
    enabled: isAuthenticated && !!cueTemplate && enabled,
    retry: false,
  })
}

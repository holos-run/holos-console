// deployment-templates.ts provides backward-compatible query hooks over the
// new unified TemplateService (v1alpha2). Route files continue to use these
// hooks until phase 11 (frontend folder-aware routing) migrates them to
// useListTemplates, useGetTemplate, etc. directly.
import { useMemo } from 'react'
import { create } from '@bufbuild/protobuf'
import { createClient } from '@connectrpc/connect'
import { useTransport } from '@connectrpc/connect-query'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  TemplateService,
  TemplateScope,
  TemplateScopeRefSchema,
} from '@/gen/holos/console/v1/templates_pb.js'
import type { TemplateScopeRef } from '@/gen/holos/console/v1/templates_pb.js'
import { useAuth } from '@/lib/auth'

// makeProjectScope builds a TemplateScopeRef for a project-scoped template.
function makeProjectScope(project: string): TemplateScopeRef {
  return create(TemplateScopeRefSchema, { scope: TemplateScope.PROJECT, scopeName: project })
}

function templateListKey(project: string) {
  return ['deployment-templates', 'list', project] as const
}

function templateGetKey(project: string, name: string) {
  return ['deployment-templates', 'get', project, name] as const
}

export function useListDeploymentTemplates(project: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(TemplateService, transport), [transport])
  return useQuery({
    queryKey: templateListKey(project),
    queryFn: async () => {
      const response = await client.listTemplates({ scope: makeProjectScope(project) })
      return response.templates
    },
    enabled: isAuthenticated && !!project,
  })
}

export function useGetDeploymentTemplate(project: string, name: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(TemplateService, transport), [transport])
  return useQuery({
    queryKey: templateGetKey(project, name),
    queryFn: async () => {
      const response = await client.getTemplate({ scope: makeProjectScope(project), name })
      return response.template
    },
    enabled: isAuthenticated && !!project && !!name,
  })
}

export function useCreateDeploymentTemplate(project: string) {
  const transport = useTransport()
  const client = useMemo(() => createClient(TemplateService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: { name: string; displayName: string; description: string; cueTemplate: string; linkedOrgTemplates?: string[] }) => {
      const scope = makeProjectScope(project)
      return client.createTemplate({
        scope,
        template: {
          name: params.name,
          scopeRef: scope,
          displayName: params.displayName,
          description: params.description,
          cueTemplate: params.cueTemplate,
          linkedTemplates: [],
          mandatory: false,
          enabled: false,
        },
      })
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: templateListKey(project) })
    },
  })
}

export function useUpdateDeploymentTemplate(project: string, name: string) {
  const transport = useTransport()
  const client = useMemo(() => createClient(TemplateService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: { displayName?: string; description?: string; cueTemplate?: string; linkedOrgTemplates?: string[] }) => {
      const scope = makeProjectScope(project)
      return client.updateTemplate({
        scope,
        template: {
          name,
          scopeRef: scope,
          displayName: params.displayName ?? '',
          description: params.description ?? '',
          cueTemplate: params.cueTemplate ?? '',
          linkedTemplates: [],
        },
      })
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: templateListKey(project) })
      queryClient.invalidateQueries({ queryKey: templateGetKey(project, name) })
    },
  })
}

export function useDeleteDeploymentTemplate(project: string) {
  const transport = useTransport()
  const client = useMemo(() => createClient(TemplateService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: { name: string }) =>
      client.deleteTemplate({ scope: makeProjectScope(project), ...params }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: templateListKey(project) })
    },
  })
}

export function useCloneDeploymentTemplate(project: string) {
  const transport = useTransport()
  const client = useMemo(() => createClient(TemplateService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: { sourceName: string; name: string; displayName: string }) =>
      client.cloneTemplate({ scope: makeProjectScope(project), ...params }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: templateListKey(project) })
    },
  })
}

function linkableOrgTemplatesKey(project: string) {
  return ['deployment-templates', 'linkable-org-templates', project] as const
}

export function useListLinkableOrgTemplates(project: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(TemplateService, transport), [transport])
  return useQuery({
    queryKey: linkableOrgTemplatesKey(project),
    queryFn: async () => {
      const response = await client.listLinkableTemplates({ scope: makeProjectScope(project) })
      // Return in the shape expected by v1alpha1 consumers: { name, displayName, description, mandatory }
      return response.templates.map(t => ({
        name: t.name,
        displayName: t.displayName,
        description: t.description,
        mandatory: t.mandatory,
      }))
    },
    enabled: isAuthenticated && !!project,
  })
}

export function useRenderDeploymentTemplate(
  cueTemplate: string,
  cueInput = '',
  enabled = true,
  cuePlatformInput = '',
) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(TemplateService, transport), [transport])
  return useQuery({
    queryKey: ['deployment-templates', 'render', cueTemplate, cueInput, cuePlatformInput] as const,
    queryFn: async () => {
      const response = await client.renderTemplate({
        // Scope is not known in this legacy hook — use a placeholder.
        // Phase 11 will migrate callers to useRenderTemplate with explicit scope.
        scope: create(TemplateScopeRefSchema, { scope: TemplateScope.PROJECT, scopeName: '' }),
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

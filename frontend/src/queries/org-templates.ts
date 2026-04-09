// org-templates.ts provides backward-compatible query hooks over the new
// unified TemplateService (v1alpha2). Route files continue to use these hooks
// until phase 11 (frontend folder-aware routing) migrates them to
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

// makeOrgScope builds a TemplateScopeRef for an organization-scoped template.
function makeOrgScope(org: string): TemplateScopeRef {
  return create(TemplateScopeRefSchema, { scope: TemplateScope.ORGANIZATION, scopeName: org })
}

function orgTemplateListKey(org: string) {
  return ['org-templates', 'list', org] as const
}

function orgTemplateGetKey(org: string, name: string) {
  return ['org-templates', 'get', org, name] as const
}

export function useListOrgTemplates(org: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(TemplateService, transport), [transport])
  return useQuery({
    queryKey: orgTemplateListKey(org),
    queryFn: async () => {
      const response = await client.listTemplates({ scope: makeOrgScope(org) })
      return response.templates
    },
    enabled: isAuthenticated && !!org,
  })
}

export function useGetOrgTemplate(org: string, name: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(TemplateService, transport), [transport])
  return useQuery({
    queryKey: orgTemplateGetKey(org, name),
    queryFn: async () => {
      const response = await client.getTemplate({ scope: makeOrgScope(org), name })
      return response.template
    },
    enabled: isAuthenticated && !!org && !!name,
  })
}

export function useCreateOrgTemplate(org: string) {
  const transport = useTransport()
  const client = useMemo(() => createClient(TemplateService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: { name: string; displayName: string; description: string; cueTemplate: string; mandatory: boolean; enabled: boolean }) => {
      const scope = makeOrgScope(org)
      return client.createTemplate({
        scope,
        template: {
          name: params.name,
          scopeRef: scope,
          displayName: params.displayName,
          description: params.description,
          cueTemplate: params.cueTemplate,
          mandatory: params.mandatory,
          enabled: params.enabled,
          linkedTemplates: [],
        },
      })
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: orgTemplateListKey(org) })
    },
  })
}

export function useUpdateOrgTemplate(org: string, name: string) {
  const transport = useTransport()
  const client = useMemo(() => createClient(TemplateService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: { displayName?: string; description?: string; cueTemplate?: string; mandatory?: boolean; enabled?: boolean }) => {
      const scope = makeOrgScope(org)
      return client.updateTemplate({
        scope,
        template: {
          name,
          scopeRef: scope,
          displayName: params.displayName ?? '',
          description: params.description ?? '',
          cueTemplate: params.cueTemplate ?? '',
          mandatory: params.mandatory ?? false,
          enabled: params.enabled ?? false,
          linkedTemplates: [],
        },
      })
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: orgTemplateListKey(org) })
      queryClient.invalidateQueries({ queryKey: orgTemplateGetKey(org, name) })
    },
  })
}

export function useDeleteOrgTemplate(org: string) {
  const transport = useTransport()
  const client = useMemo(() => createClient(TemplateService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: { name: string }) =>
      client.deleteTemplate({ scope: makeOrgScope(org), ...params }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: orgTemplateListKey(org) })
    },
  })
}

export function useCloneOrgTemplate(org: string) {
  const transport = useTransport()
  const client = useMemo(() => createClient(TemplateService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: { sourceName: string; name: string; displayName: string }) =>
      client.cloneTemplate({ scope: makeOrgScope(org), ...params }),
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
  const client = useMemo(() => createClient(TemplateService, transport), [transport])
  return useQuery({
    queryKey: ['org-templates', 'render', cueTemplate, cueInput, cuePlatformInput] as const,
    queryFn: async () => {
      const response = await client.renderTemplate({
        // Scope is not known in this legacy hook — use a placeholder.
        // Phase 11 will migrate callers to useRenderTemplate with explicit scope.
        scope: create(TemplateScopeRefSchema, { scope: TemplateScope.ORGANIZATION, scopeName: '' }),
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

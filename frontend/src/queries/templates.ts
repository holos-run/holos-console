import { useMemo } from 'react'
import { create } from '@bufbuild/protobuf'
import { createClient } from '@connectrpc/connect'
import { useTransport } from '@connectrpc/connect-query'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  TemplateService,
  ReleaseSchema,
} from '@/gen/holos/console/v1/templates_pb.js'
import type { LinkableTemplate, Release, TemplateUpdate, TemplateDefaults } from '@/gen/holos/console/v1/templates_pb.js'
import {
  TemplateScopeRefSchema,
  TemplateScope,
} from '@/gen/holos/console/v1/policy_state_pb.js'
import type { TemplateScopeRef, LinkedTemplateRef } from '@/gen/holos/console/v1/policy_state_pb.js'
import { useAuth } from '@/lib/auth'

// Re-export types used by consumers.
export type { TemplateScopeRef, LinkableTemplate, LinkedTemplateRef, Release, TemplateUpdate, TemplateDefaults }
export { TemplateScope }

/** Build a composite key that uniquely identifies a linkable template across scopes. */
export function linkableKey(scope: number | undefined, scopeName: string | undefined, name: string): string {
  return `${scope ?? 0}/${scopeName ?? ''}/${name}`
}

/** Parse a composite key back into its constituent parts. */
export function parseLinkableKey(key: string): { scope: number; scopeName: string; name: string } {
  const parts = key.split('/')
  return { scope: Number(parts[0]), scopeName: parts[1] ?? '', name: parts.slice(2).join('/') }
}

// makeScope is a helper to build a TemplateScopeRef from scope and scopeName.
export function makeScope(scope: TemplateScope, scopeName: string): TemplateScopeRef {
  return create(TemplateScopeRefSchema, { scope, scopeName })
}

// makeOrgScope builds a TemplateScopeRef for an organization-scoped template.
export function makeOrgScope(org: string): TemplateScopeRef {
  return makeScope(TemplateScope.ORGANIZATION, org)
}

// makeFolderScope builds a TemplateScopeRef for a folder-scoped template.
export function makeFolderScope(folder: string): TemplateScopeRef {
  return makeScope(TemplateScope.FOLDER, folder)
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

function linkableTemplatesKey(scope: TemplateScopeRef, includeSelfScope: boolean) {
  return ['templates', 'linkable', scope.scope, scope.scopeName, includeSelfScope] as const
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

function templateDefaultsKey(scope: TemplateScopeRef, name: string) {
  return ['templates', 'defaults', scope.scope, scope.scopeName, name] as const
}

// useGetTemplateDefaults fetches the TemplateDefaults payload for a given
// template via the explicit TemplateService.GetTemplateDefaults RPC. Per
// ADR 027, this is the sole source of truth for Create Deployment form
// pre-fill; callers must not read Template.defaults from the list response.
//
// The hook is disabled when name is empty so the RPC is never called
// eagerly on mount before the user selects a template.
export function useGetTemplateDefaults(
  params: { scope: TemplateScopeRef; name: string },
  options?: { enabled?: boolean },
) {
  const { scope, name } = params
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(TemplateService, transport), [transport])
  const callerEnabled = options?.enabled ?? true
  return useQuery({
    queryKey: templateDefaultsKey(scope, name),
    queryFn: async () => {
      const response = await client.getTemplateDefaults({ scope, name })
      return response.defaults
    },
    enabled: isAuthenticated && !!scope.scopeName && !!name && callerEnabled,
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
      enabled?: boolean
      linkedTemplates?: LinkedTemplateRef[]
      updateLinkedTemplates?: boolean
    }) =>
      client.updateTemplate({
        scope,
        updateLinkedTemplates: params.updateLinkedTemplates ?? false,
        template: {
          name,
          scopeRef: scope,
          displayName: params.displayName ?? '',
          description: params.description ?? '',
          cueTemplate: params.cueTemplate ?? '',
          enabled: params.enabled ?? false,
          linkedTemplates: params.linkedTemplates ?? [],
        },
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: templateListKey(scope) })
      queryClient.invalidateQueries({ queryKey: templateGetKey(scope, name) })
      // Invalidate all check-updates queries for this scope so upgrade badges
      // and dialogs reflect the new state immediately after a template update.
      queryClient.invalidateQueries({ queryKey: ['templates', 'checkUpdates'] })
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

// useListLinkableTemplates returns enabled templates that can be explicitly
// linked to templates at the given scope. By default only ancestor-scope
// templates are returned — the semantics required by the project-template
// linking UI. Pass `{ includeSelfScope: true }` to also include templates at
// the request's own scope; the TemplatePolicy editor uses this so org-scope
// policies (which have no ancestors) and folder-scope policies can pick
// same-scope templates. See HOL-561.
export function useListLinkableTemplates(
  scope: TemplateScopeRef,
  options?: { includeSelfScope?: boolean },
) {
  const includeSelfScope = options?.includeSelfScope ?? false
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(TemplateService, transport), [transport])
  return useQuery({
    queryKey: linkableTemplatesKey(scope, includeSelfScope),
    queryFn: async () => {
      const response = await client.listLinkableTemplates({ scope, includeSelfScope })
      return response.templates
    },
    enabled: isAuthenticated && !!scope.scopeName,
  })
}

// --- Release hooks ---

function releaseListKey(scope: TemplateScopeRef, templateName: string) {
  return ['releases', 'list', scope.scope, scope.scopeName, templateName] as const
}

function releaseGetKey(scope: TemplateScopeRef, templateName: string, version: string) {
  return ['releases', 'get', scope.scope, scope.scopeName, templateName, version] as const
}

export function useListReleases(scope: TemplateScopeRef, templateName: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(TemplateService, transport), [transport])
  return useQuery({
    queryKey: releaseListKey(scope, templateName),
    queryFn: async () => {
      const response = await client.listReleases({ scope, templateName })
      return response.releases
    },
    enabled: isAuthenticated && !!scope.scopeName && !!templateName,
  })
}

export function useGetRelease(scope: TemplateScopeRef, templateName: string, version: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(TemplateService, transport), [transport])
  return useQuery({
    queryKey: releaseGetKey(scope, templateName, version),
    queryFn: async () => {
      const response = await client.getRelease({ scope, templateName, version })
      return response.release
    },
    enabled: isAuthenticated && !!scope.scopeName && !!templateName && !!version,
  })
}

export function useCreateRelease(scope: TemplateScopeRef, templateName: string) {
  const transport = useTransport()
  const client = useMemo(() => createClient(TemplateService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: {
      version: string
      changelog: string
      upgradeAdvice?: string
      cueTemplate: string
      defaults?: Release['defaults']
    }) =>
      client.createRelease({
        scope,
        release: create(ReleaseSchema, {
          templateName,
          scopeRef: scope,
          version: params.version,
          changelog: params.changelog,
          upgradeAdvice: params.upgradeAdvice ?? '',
          cueTemplate: params.cueTemplate,
          defaults: params.defaults,
        }),
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: releaseListKey(scope, templateName) })
    },
  })
}

// --- CheckUpdates hooks ---

function checkUpdatesKey(scope: TemplateScopeRef, templateName: string) {
  return ['templates', 'checkUpdates', scope.scope, scope.scopeName, templateName] as const
}

// useCheckUpdates returns available version updates for linked templates.
// When templateName is provided, only that template's links are checked.
// When empty, all templates in the scope are checked.
// Pass options.enabled to control when the query fires (defaults to true).
// Pass options.includeCurrent to include entries for templates already at their
// latest version (useful for the version status indicator).
export function useCheckUpdates(scope: TemplateScopeRef, templateName = '', options?: { enabled?: boolean; includeCurrent?: boolean }) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(TemplateService, transport), [transport])
  const callerEnabled = options?.enabled ?? true
  const includeCurrent = options?.includeCurrent ?? false
  return useQuery({
    queryKey: [...checkUpdatesKey(scope, templateName), includeCurrent] as const,
    queryFn: async () => {
      const response = await client.checkUpdates({ scope, templateName, includeCurrent })
      return response.updates
    },
    enabled: isAuthenticated && !!scope.scopeName && callerEnabled,
  })
}

// useGetProjectTemplatePolicyState fetches the TemplatePolicy drift snapshot
// for a project-scope template (HOL-567). PolicyState is sourced from the
// folder-namespace render-state store — see PolicySection's component-level
// comment for the storage-isolation guarantee. This RPC is the sole read
// path used by the drift UI for project-scope templates; never infer drift
// from other template fields.
//
// The request uses TEMPLATE_SCOPE_PROJECT; the backend validates the scope
// and rejects non-project scopes with InvalidArgument.
function projectTemplatePolicyStateKey(scope: TemplateScopeRef, name: string) {
  return ['templates', 'policy-state', scope.scope, scope.scopeName, name] as const
}

export function useGetProjectTemplatePolicyState(scope: TemplateScopeRef, name: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(TemplateService, transport), [transport])
  return useQuery({
    queryKey: projectTemplatePolicyStateKey(scope, name),
    queryFn: async () => {
      const response = await client.getProjectTemplatePolicyState({ scope, name })
      return response.state
    },
    enabled:
      isAuthenticated &&
      !!scope.scopeName &&
      !!name &&
      scope.scope === TemplateScope.PROJECT,
  })
}

// useRenderTemplate renders a CUE template with the given inputs. The scope
// parameter determines which ancestor platform templates are resolved.
// linkedTemplates optionally passes explicit linked template refs to unify
// with the project template for a combined preview.
export function useRenderTemplate(
  scope: TemplateScopeRef,
  cueTemplate: string,
  cueInput = '',
  enabled = true,
  cuePlatformInput = '',
  linkedTemplates: LinkedTemplateRef[] = [],
) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(TemplateService, transport), [transport])
  // Serialize linked templates into the query key so the query refetches when
  // the linked selection changes.
  const linkedKey = linkedTemplates.map(t => `${t.scope}/${t.scopeName}/${t.name}@${t.versionConstraint ?? ''}`).join(',')
  return useQuery({
    queryKey: ['templates', 'render', scope.scope, scope.scopeName, cueTemplate, cueInput, cuePlatformInput, linkedKey] as const,
    queryFn: async () => {
      const response = await client.renderTemplate({
        scope,
        cueTemplate,
        cueProjectInput: cueInput,
        cuePlatformInput,
        linkedTemplates,
      })
      return {
        renderedYaml: response.renderedYaml,
        renderedJson: response.renderedJson,
        platformResourcesYaml: response.platformResourcesYaml,
        platformResourcesJson: response.platformResourcesJson,
        projectResourcesYaml: response.projectResourcesYaml,
        projectResourcesJson: response.projectResourcesJson,
      }
    },
    enabled: isAuthenticated && !!cueTemplate && enabled,
    retry: false,
  })
}

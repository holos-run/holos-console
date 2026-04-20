import { useMemo } from 'react'
import { create } from '@bufbuild/protobuf'
import { createClient } from '@connectrpc/connect'
import { useTransport } from '@connectrpc/connect-query'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  TemplateService,
  ReleaseSchema,
} from '@/gen/holos/console/v1/templates_pb.js'
import type {
  LinkableTemplate,
  Release,
  TemplateUpdate,
  TemplateDefaults,
} from '@/gen/holos/console/v1/templates_pb.js'
import type { LinkedTemplateRef } from '@/gen/holos/console/v1/policy_state_pb.js'
import { useAuth } from '@/lib/auth'

// Re-export generated types used by consumers.
export type { LinkableTemplate, LinkedTemplateRef, Release, TemplateUpdate, TemplateDefaults }

// linkableKey builds a composite key that uniquely identifies a linkable
// template across namespaces. HOL-623 reworked the UI to key templates by
// (namespace, name) only — no more TemplateScopeRef. Consumers that need a
// stable React key (e.g. select option values, table row ids) should use
// this helper.
export function linkableKey(namespace: string | undefined, name: string): string {
  return `${namespace ?? ''}/${name}`
}

// parseLinkableKey reverses linkableKey. The name segment may itself contain
// slashes, so we split from the left only once.
export function parseLinkableKey(key: string): { namespace: string; name: string } {
  const slash = key.indexOf('/')
  if (slash < 0) return { namespace: '', name: key }
  return { namespace: key.slice(0, slash), name: key.slice(slash + 1) }
}

function templateListKey(namespace: string) {
  return ['templates', 'list', namespace] as const
}

function templateGetKey(namespace: string, name: string) {
  return ['templates', 'get', namespace, name] as const
}

function linkableTemplatesKey(namespace: string, includeSelfScope: boolean) {
  return ['templates', 'linkable', namespace, includeSelfScope] as const
}

export function useListTemplates(namespace: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(TemplateService, transport), [transport])
  return useQuery({
    queryKey: templateListKey(namespace),
    queryFn: async () => {
      const response = await client.listTemplates({ namespace })
      return response.templates
    },
    enabled: isAuthenticated && !!namespace,
  })
}

function templateDefaultsKey(namespace: string, name: string) {
  return ['templates', 'defaults', namespace, name] as const
}

// useGetTemplateDefaults fetches the TemplateDefaults payload for a given
// template via the explicit TemplateService.GetTemplateDefaults RPC. Per
// ADR 027, this is the sole source of truth for Create Deployment form
// pre-fill; callers must not read Template.defaults from the list response.
//
// The hook is disabled when name is empty so the RPC is never called
// eagerly on mount before the user selects a template.
export function useGetTemplateDefaults(
  params: { namespace: string; name: string },
  options?: { enabled?: boolean },
) {
  const { namespace, name } = params
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(TemplateService, transport), [transport])
  const callerEnabled = options?.enabled ?? true
  return useQuery({
    queryKey: templateDefaultsKey(namespace, name),
    queryFn: async () => {
      const response = await client.getTemplateDefaults({ namespace, name })
      return response.defaults
    },
    enabled: isAuthenticated && !!namespace && !!name && callerEnabled,
  })
}

export function useGetTemplate(namespace: string, name: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(TemplateService, transport), [transport])
  return useQuery({
    queryKey: templateGetKey(namespace, name),
    queryFn: async () => {
      const response = await client.getTemplate({ namespace, name })
      return response.template
    },
    enabled: isAuthenticated && !!namespace && !!name,
  })
}

export function useCreateTemplate(namespace: string) {
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
    }) => {
      return client.createTemplate({
        namespace,
        template: {
          name: params.name,
          namespace,
          displayName: params.displayName,
          description: params.description,
          cueTemplate: params.cueTemplate,
          enabled: params.enabled ?? false,
          linkedTemplates: params.linkedTemplates ?? [],
        },
      })
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: templateListKey(namespace) })
    },
  })
}

export function useUpdateTemplate(namespace: string, name: string) {
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
    }) => {
      return client.updateTemplate({
        namespace,
        updateLinkedTemplates: params.updateLinkedTemplates ?? false,
        template: {
          name,
          namespace,
          displayName: params.displayName ?? '',
          description: params.description ?? '',
          cueTemplate: params.cueTemplate ?? '',
          enabled: params.enabled ?? false,
          linkedTemplates: params.linkedTemplates ?? [],
        },
      })
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: templateListKey(namespace) })
      queryClient.invalidateQueries({ queryKey: templateGetKey(namespace, name) })
      // Invalidate all check-updates queries so upgrade badges and dialogs
      // reflect the new state immediately after a template update.
      queryClient.invalidateQueries({ queryKey: ['templates', 'checkUpdates'] })
      // HOL-559: a successful UpdateTemplate re-renders against the
      // current TemplatePolicy chain and records a fresh applied render
      // set on the backend. Invalidate all policy-state queries for this
      // namespace so the list-row drift badge and the detail PolicySection
      // both refresh from the authoritative state rather than showing
      // the stale "drifted" snapshot after reconcile.
      queryClient.invalidateQueries({ queryKey: ['templates', 'policy-state', namespace] })
    },
  })
}

export function useDeleteTemplate(namespace: string) {
  const transport = useTransport()
  const client = useMemo(() => createClient(TemplateService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: { name: string }) =>
      client.deleteTemplate({ namespace, ...params }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: templateListKey(namespace) })
    },
  })
}

export function useCloneTemplate(namespace: string) {
  const transport = useTransport()
  const client = useMemo(() => createClient(TemplateService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: { sourceName: string; name: string; displayName: string }) =>
      client.cloneTemplate({ namespace, ...params }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: templateListKey(namespace) })
    },
  })
}

// useListLinkableTemplates returns enabled templates that can be explicitly
// linked to templates at the given namespace. By default only ancestor-scope
// templates are returned — the semantics required by the project-template
// linking UI. Pass `{ includeSelfScope: true }` to also include templates at
// the request's own namespace; the TemplatePolicy editor uses this so org-scope
// policies (which have no ancestors) and folder-scope policies can pick
// same-scope templates. See HOL-561.
export function useListLinkableTemplates(
  namespace: string,
  options?: { includeSelfScope?: boolean },
) {
  const includeSelfScope = options?.includeSelfScope ?? false
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(TemplateService, transport), [transport])
  return useQuery({
    queryKey: linkableTemplatesKey(namespace, includeSelfScope),
    queryFn: async () => {
      const response = await client.listLinkableTemplates({ namespace, includeSelfScope })
      return response.templates
    },
    enabled: isAuthenticated && !!namespace,
  })
}

// --- Release hooks ---

function releaseListKey(namespace: string, templateName: string) {
  return ['releases', 'list', namespace, templateName] as const
}

function releaseGetKey(namespace: string, templateName: string, version: string) {
  return ['releases', 'get', namespace, templateName, version] as const
}

export function useListReleases(namespace: string, templateName: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(TemplateService, transport), [transport])
  return useQuery({
    queryKey: releaseListKey(namespace, templateName),
    queryFn: async () => {
      const response = await client.listReleases({ namespace, templateName })
      return response.releases
    },
    enabled: isAuthenticated && !!namespace && !!templateName,
  })
}

export function useGetRelease(namespace: string, templateName: string, version: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(TemplateService, transport), [transport])
  return useQuery({
    queryKey: releaseGetKey(namespace, templateName, version),
    queryFn: async () => {
      const response = await client.getRelease({ namespace, templateName, version })
      return response.release
    },
    enabled: isAuthenticated && !!namespace && !!templateName && !!version,
  })
}

export function useCreateRelease(namespace: string, templateName: string) {
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
    }) => {
      return client.createRelease({
        namespace,
        release: create(ReleaseSchema, {
          templateName,
          namespace,
          version: params.version,
          changelog: params.changelog,
          upgradeAdvice: params.upgradeAdvice ?? '',
          cueTemplate: params.cueTemplate,
          defaults: params.defaults,
        }),
      })
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: releaseListKey(namespace, templateName) })
    },
  })
}

// --- CheckUpdates hooks ---

function checkUpdatesKey(namespace: string, templateName: string) {
  return ['templates', 'checkUpdates', namespace, templateName] as const
}

// useCheckUpdates returns available version updates for linked templates.
// When templateName is provided, only that template's links are checked.
// When empty, all templates in the namespace are checked.
// Pass options.enabled to control when the query fires (defaults to true).
// Pass options.includeCurrent to include entries for templates already at their
// latest version (useful for the version status indicator).
export function useCheckUpdates(
  namespace: string,
  templateName = '',
  options?: { enabled?: boolean; includeCurrent?: boolean },
) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(TemplateService, transport), [transport])
  const callerEnabled = options?.enabled ?? true
  const includeCurrent = options?.includeCurrent ?? false
  return useQuery({
    queryKey: [...checkUpdatesKey(namespace, templateName), includeCurrent] as const,
    queryFn: async () => {
      const response = await client.checkUpdates({ namespace, templateName, includeCurrent })
      return response.updates
    },
    enabled: isAuthenticated && !!namespace && callerEnabled,
  })
}

// useGetProjectTemplatePolicyState fetches the TemplatePolicy drift snapshot
// for a project-scope template (HOL-567). PolicyState is sourced from the
// folder-namespace render-state store — see PolicySection's component-level
// comment for the storage-isolation guarantee. This RPC is the sole read
// path used by the drift UI for project-scope templates; never infer drift
// from other template fields.
//
// The hook is always enabled when namespace and name are set. The backend
// validates that the namespace corresponds to a project scope and rejects
// non-project scopes with InvalidArgument; the UI should therefore only
// invoke this hook on project-scope editor pages. See the callsite.
function projectTemplatePolicyStateKey(namespace: string, name: string) {
  return ['templates', 'policy-state', namespace, name] as const
}

export function useGetProjectTemplatePolicyState(namespace: string, name: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(TemplateService, transport), [transport])
  return useQuery({
    queryKey: projectTemplatePolicyStateKey(namespace, name),
    queryFn: async () => {
      const response = await client.getProjectTemplatePolicyState({ namespace, name })
      return response.state
    },
    enabled: isAuthenticated && !!namespace && !!name,
  })
}

// useRenderTemplate renders a CUE template with the given inputs. The namespace
// parameter determines which ancestor platform templates are resolved.
// linkedTemplates optionally passes explicit linked template refs to unify
// with the project template for a combined preview.
export function useRenderTemplate(
  namespace: string,
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
  const linkedKey = linkedTemplates
    .map(t => `${t.namespace}/${t.name}@${t.versionConstraint ?? ''}`)
    .join(',')
  return useQuery({
    queryKey: ['templates', 'render', namespace, cueTemplate, cueInput, cuePlatformInput, linkedKey] as const,
    queryFn: async () => {
      const response = await client.renderTemplate({
        namespace,
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

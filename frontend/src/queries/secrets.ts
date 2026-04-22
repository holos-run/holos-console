import { useMemo } from 'react'
import { createClient } from '@connectrpc/connect'
import { useTransport } from '@connectrpc/connect-query'
import { useQuery, useQueries, useMutation, useQueryClient } from '@tanstack/react-query'
import { SecretsService } from '@/gen/holos/console/v1/secrets_pb.js'
import type { SecretMetadata } from '@/gen/holos/console/v1/secrets_pb.js'
import { useAuth } from '@/lib/auth'
import { useGetProject } from '@/queries/projects'
import { useListFolders } from '@/queries/folders'
import { aggregateFanOut, type FanOutAggregate, type FanOutQueryState } from '@/queries/templatePolicies'
import type { LineageDirection } from '@/components/resource-grid/types'

function listSecretsKey(project: string) {
  return ['secrets', 'list', project] as const
}

export function useListSecrets(project: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(SecretsService, transport), [transport])
  return useQuery({
    queryKey: listSecretsKey(project),
    queryFn: async () => {
      const response = await client.listSecrets({ project })
      return response.secrets
    },
    enabled: isAuthenticated && !!project,
  })
}

export function useGetSecret(project: string, name: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(SecretsService, transport), [transport])
  return useQuery({
    queryKey: ['secrets', 'get', project, name],
    queryFn: async () => {
      const response = await client.getSecret({ name, project })
      return response.data as Record<string, Uint8Array>
    },
    enabled: isAuthenticated && !!project && !!name,
  })
}

// GetSecret only returns data (bytes), not metadata (description, url, grants).
// There is no dedicated GetSecretMetadata RPC, so we derive metadata from the
// listSecrets cache. Uses the same query key as useListSecrets so TanStack Query
// deduplicates the request when both hooks are active on the same page.
export function useGetSecretMetadata(project: string, name: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(SecretsService, transport), [transport])
  return useQuery({
    queryKey: listSecretsKey(project),
    queryFn: async () => {
      const response = await client.listSecrets({ project })
      return response.secrets
    },
    enabled: isAuthenticated && !!project && !!name,
    select: (secrets) => secrets.find((s: SecretMetadata) => s.name === name) ?? null,
  })
}

export function useCreateSecret(project: string) {
  const transport = useTransport()
  const client = useMemo(() => createClient(SecretsService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: {
      name: string
      data: Record<string, Uint8Array>
      userGrants: { principal: string; role: number }[]
      roleGrants: { principal: string; role: number }[]
      description?: string
      url?: string
    }) => client.createSecret({ ...params, project }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: listSecretsKey(project) })
    },
  })
}

export function useDeleteSecret(project: string) {
  const transport = useTransport()
  const client = useMemo(() => createClient(SecretsService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (name: string) => client.deleteSecret({ name, project }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: listSecretsKey(project) })
    },
  })
}

export function useUpdateSecret(project: string) {
  const transport = useTransport()
  const client = useMemo(() => createClient(SecretsService, transport), [transport])
  return useMutation({
    mutationFn: (params: {
      name: string
      data: Record<string, Uint8Array>
      description?: string
      url?: string
    }) => client.updateSecret({ ...params, project }),
  })
}

export function useUpdateSecretSharing(project: string) {
  const transport = useTransport()
  const client = useMemo(() => createClient(SecretsService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: {
      name: string
      userGrants: { principal: string; role: number }[]
      roleGrants: { principal: string; role: number }[]
    }) => client.updateSharing({ ...params, project }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: listSecretsKey(project) })
    },
  })
}

// Module-level sentinel so the `folders` useMemo fallback preserves reference
// identity across renders when the folders list is still pending or empty.
const EMPTY_FOLDERS: readonly { name: string }[] = []

/**
 * useAllSecretsForProject fans a ListSecrets call across the project itself
 * plus its ancestor folders and org when the lineage filter requests it.
 *
 * The fan-out pattern mirrors useAllTemplatesForOrg in templates.ts. When
 * lineage is 'descendants' (the default), only the project's own secrets are
 * fetched. When lineage is 'ancestors' or 'both', the hook additionally
 * queries each ancestor folder and the org namespace.
 *
 * Result rows carry a `scope` field (project name, folder name, or org name)
 * that the caller maps to the ResourceGrid Row.parentId so the lineage filter
 * UI can surface the correct parent label.
 */
export interface SecretRow {
  secret: SecretMetadata
  /** The project/folder/org name the secret was fetched from. */
  scope: string
}

export function useAllSecretsForProject(
  projectName: string,
  options: { lineage?: LineageDirection } = {},
): FanOutAggregate<SecretRow> {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(SecretsService, transport), [transport])

  const lineage = options.lineage ?? 'descendants'
  const includeAncestors = lineage === 'ancestors' || lineage === 'both'

  // Fetch the project record so we can resolve its parent folder and org.
  const projectQuery = useGetProject(projectName)
  const project = projectQuery.data
  const orgName = project?.organization ?? ''

  // Fetch all folders in the org so we can walk up the ancestor chain.
  const foldersQuery = useListFolders(includeAncestors ? orgName : '')
  const folders = useMemo(
    () => foldersQuery.data ?? EMPTY_FOLDERS,
    [foldersQuery.data],
  )

  // Build the set of ancestor scopes: immediate parent folder (if any) up to
  // the org. When the project has a folder parent, that folder is the direct
  // ancestor. For simplicity we include all folders in the org when ancestors
  // are requested — the recursive flag on the grid controls depth in the UI.
  const ancestorFolderNames = useMemo<string[]>(() => {
    if (!includeAncestors || !project) return []
    return folders.map((f) => f.name)
  }, [includeAncestors, project, folders])

  // Always fetch the project's own secrets.
  const projectSecretsQuery = useQuery({
    queryKey: [...listSecretsKey(projectName), 'fanout'] as const,
    queryFn: async (): Promise<SecretRow[]> => {
      const response = await client.listSecrets({ project: projectName })
      return response.secrets.map((s) => ({ secret: s, scope: projectName }))
    },
    enabled: isAuthenticated && !!projectName,
  })

  // Fan-out to ancestor folder scopes (useQueries to avoid conditional hooks).
  const folderQueries = useQueries({
    queries: ancestorFolderNames.map((folderName) => ({
      queryKey: [...listSecretsKey(folderName), 'fanout'] as const,
      queryFn: async (): Promise<SecretRow[]> => {
        const response = await client.listSecrets({ project: folderName })
        return response.secrets.map((s) => ({ secret: s, scope: folderName }))
      },
      enabled: isAuthenticated && !!folderName,
    })),
  })

  // Fan-out to the org scope.
  const orgQuery = useQuery({
    queryKey: [...listSecretsKey(orgName), 'fanout'] as const,
    queryFn: async (): Promise<SecretRow[]> => {
      const response = await client.listSecrets({ project: orgName })
      return response.secrets.map((s) => ({ secret: s, scope: orgName }))
    },
    enabled: isAuthenticated && !!orgName && includeAncestors,
  })

  // Model the project-level query and structural queries as FanOutQueryState
  // inputs so aggregateFanOut can handle pending/error semantics uniformly.
  const projectAsQuery: FanOutQueryState<SecretRow[]> = {
    data: projectSecretsQuery.data,
    error: projectSecretsQuery.error,
    isPending: projectSecretsQuery.isPending,
    fetchStatus: projectSecretsQuery.fetchStatus,
  }

  const foldersListAsQuery: FanOutQueryState<SecretRow[]> = {
    data: foldersQuery.data === undefined ? undefined : [],
    error: foldersQuery.error,
    isPending: foldersQuery.isPending,
    fetchStatus: foldersQuery.fetchStatus,
  }

  const orgAsQuery: FanOutQueryState<SecretRow[]> = {
    data: orgQuery.data,
    error: orgQuery.error,
    isPending: orgQuery.isPending,
    fetchStatus: orgQuery.fetchStatus,
  }

  const allQueries: FanOutQueryState<SecretRow[]>[] = [
    projectAsQuery,
    ...(includeAncestors ? [foldersListAsQuery] : []),
    ...(includeAncestors ? [orgAsQuery] : []),
    ...folderQueries.map((q) => ({
      data: q.data,
      error: q.error,
      isPending: q.isPending,
      fetchStatus: q.fetchStatus,
    })),
  ]

  return aggregateFanOut<SecretRow>(allQueries)
}


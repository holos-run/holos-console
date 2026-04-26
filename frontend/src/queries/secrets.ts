import { useMemo } from 'react'
import { createClient } from '@connectrpc/connect'
import { useTransport } from '@connectrpc/connect-query'
import {
  keepPreviousData,
  useQuery,
  useMutation,
  useQueryClient,
  type QueryClient,
} from '@tanstack/react-query'
import { SecretsService } from '@/gen/holos/console/v1/secrets_pb.js'
import type { SecretMetadata } from '@/gen/holos/console/v1/secrets_pb.js'
import { useAuth } from '@/lib/auth'
import { aggregateFanOut, type FanOutAggregate, type FanOutQueryState } from '@/queries/templatePolicies'
import { keys } from '@/queries/keys'

function invalidateSecretListAndDetail(
  queryClient: QueryClient,
  project: string,
  name: string,
) {
  queryClient.invalidateQueries({ queryKey: keys.secrets.list(project) })
  queryClient.invalidateQueries({ queryKey: keys.secrets.get(project, name) })
}

export function useListSecrets(project: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(SecretsService, transport), [transport])
  return useQuery({
    queryKey: keys.secrets.list(project),
    queryFn: async () => {
      const response = await client.listSecrets({ project })
      return response.secrets
    },
    enabled: isAuthenticated && !!project,
    placeholderData: keepPreviousData,
  })
}

export function useGetSecret(project: string, name: string) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(SecretsService, transport), [transport])
  return useQuery({
    queryKey: keys.secrets.get(project, name),
    queryFn: async () => {
      const response = await client.getSecret({ name, project })
      return response.data as Record<string, Uint8Array>
    },
    enabled: isAuthenticated && !!project && !!name,
  })
}

/**
 * useGetSecretRaw fetches the full Kubernetes Secret object as verbatim JSON.
 * The query is disabled by default; pass enabled=true to trigger the fetch.
 * This keeps transport and client creation inside the hook, preventing spurious
 * auth refreshes when the detail page mounts in read-only mode.
 */
export function useGetSecretRaw(project: string, name: string, enabled: boolean) {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(SecretsService, transport), [transport])
  return useQuery({
    queryKey: keys.secrets.raw(project, name),
    queryFn: async () => {
      const response = await client.getSecretRaw({ name, project })
      return response.raw
    },
    enabled: isAuthenticated && !!project && !!name && enabled,
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
    queryKey: keys.secrets.list(project),
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
    onSuccess: (_data, variables) => {
      invalidateSecretListAndDetail(queryClient, project, variables.name)
    },
  })
}

export function useDeleteSecret(project: string) {
  const transport = useTransport()
  const client = useMemo(() => createClient(SecretsService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (name: string) => client.deleteSecret({ name, project }),
    onSuccess: (_data, name) => {
      invalidateSecretListAndDetail(queryClient, project, name)
    },
  })
}

export function useUpdateSecret(project: string) {
  const transport = useTransport()
  const client = useMemo(() => createClient(SecretsService, transport), [transport])
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (params: {
      name: string
      data: Record<string, Uint8Array>
      description?: string
      url?: string
    }) => client.updateSecret({ ...params, project }),
    onSuccess: (_data, variables) => {
      invalidateSecretListAndDetail(queryClient, project, variables.name)
    },
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
    onSuccess: (_data, variables) => {
      invalidateSecretListAndDetail(queryClient, project, variables.name)
    },
  })
}

/**
 * useAllSecretsForProject fetches the secrets for the given project namespace.
 *
 * Result rows carry a `scope` field (the project name) that the caller maps
 * to the ResourceGrid Row.parentId.
 */
export interface SecretRow {
  secret: SecretMetadata
  /** The project name the secret was fetched from. */
  scope: string
}

export function useAllSecretsForProject(
  projectName: string,
): FanOutAggregate<SecretRow> {
  const { isAuthenticated } = useAuth()
  const transport = useTransport()
  const client = useMemo(() => createClient(SecretsService, transport), [transport])

  const projectSecretsQuery = useQuery({
    queryKey: keys.secrets.fanout(projectName),
    queryFn: async (): Promise<SecretRow[]> => {
      const response = await client.listSecrets({ project: projectName })
      return response.secrets.map((s) => ({ secret: s, scope: projectName }))
    },
    enabled: isAuthenticated && !!projectName,
    placeholderData: keepPreviousData,
  })

  const projectAsQuery: FanOutQueryState<SecretRow[]> = {
    data: projectSecretsQuery.data,
    error: projectSecretsQuery.error,
    isPending: projectSecretsQuery.isPending,
    fetchStatus: projectSecretsQuery.fetchStatus,
  }

  return aggregateFanOut<SecretRow>([projectAsQuery])
}

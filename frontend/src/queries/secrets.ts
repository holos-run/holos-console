import { useMemo } from 'react'
import { createClient } from '@connectrpc/connect'
import { useTransport } from '@connectrpc/connect-query'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { SecretsService } from '@/gen/holos/console/v1/secrets_pb.js'
import type { SecretMetadata } from '@/gen/holos/console/v1/secrets_pb.js'

function listSecretsKey(project: string) {
  return ['secrets', 'list', project] as const
}

export function useListSecrets(project: string) {
  const transport = useTransport()
  const client = useMemo(() => createClient(SecretsService, transport), [transport])
  return useQuery({
    queryKey: listSecretsKey(project),
    queryFn: async () => {
      const response = await client.listSecrets({ project })
      return response.secrets
    },
    enabled: !!project,
  })
}

export function useGetSecret(project: string, name: string) {
  const transport = useTransport()
  const client = useMemo(() => createClient(SecretsService, transport), [transport])
  return useQuery({
    queryKey: ['secrets', 'get', project, name],
    queryFn: async () => {
      const response = await client.getSecret({ name, project })
      return response.data as Record<string, Uint8Array>
    },
    enabled: !!project && !!name,
  })
}

// GetSecret only returns data (bytes), not metadata (description, url, grants).
// There is no dedicated GetSecretMetadata RPC, so we derive metadata from the
// listSecrets cache. Uses the same query key as useListSecrets so TanStack Query
// deduplicates the request when both hooks are active on the same page.
export function useGetSecretMetadata(project: string, name: string) {
  const transport = useTransport()
  const client = useMemo(() => createClient(SecretsService, transport), [transport])
  return useQuery({
    queryKey: listSecretsKey(project),
    queryFn: async () => {
      const response = await client.listSecrets({ project })
      return response.secrets
    },
    enabled: !!project && !!name,
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

export function useSecretsClient() {
  const transport = useTransport()
  return useMemo(() => createClient(SecretsService, transport), [transport])
}

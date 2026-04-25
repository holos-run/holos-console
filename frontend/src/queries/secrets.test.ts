import { create } from '@bufbuild/protobuf'
import { renderHook, waitFor, act } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import React from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Mock } from 'vitest'
import {
  SecretMetadataSchema,
  type SecretMetadata,
} from '@/gen/holos/console/v1/secrets_pb.js'
import { keys } from '@/queries/keys'
import {
  useCreateSecret,
  useDeleteSecret,
  useListSecrets,
  useUpdateSecret,
  useUpdateSecretSharing,
} from '@/queries/secrets'

vi.mock('@connectrpc/connect', () => ({
  createClient: vi.fn(),
}))

vi.mock('@connectrpc/connect-query', () => ({
  useTransport: vi.fn(),
}))

vi.mock('@/lib/auth', () => ({
  useAuth: vi.fn(),
}))

import { createClient } from '@connectrpc/connect'
import { useTransport } from '@connectrpc/connect-query'
import { useAuth } from '@/lib/auth'

function makeWrapper(queryClient: QueryClient) {
  return ({ children }: { children: React.ReactNode }) =>
    React.createElement(QueryClientProvider, { client: queryClient }, children)
}

function secret(name: string): SecretMetadata {
  return create(SecretMetadataSchema, {
    name,
    accessible: true,
    userGrants: [],
    roleGrants: [],
  })
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((res) => {
    resolve = res
  })
  return { promise, resolve }
}

function expectSecretInvalidation(invalidateSpy: unknown, project: string, name: string) {
  expect(invalidateSpy).toHaveBeenCalledWith({
    queryKey: keys.secrets.list(project),
  })
  expect(invalidateSpy).toHaveBeenCalledWith({
    queryKey: keys.secrets.get(project, name),
  })
}

describe('secret query keys', () => {
  let queryClient: QueryClient
  let mockClient: Record<string, Mock>

  beforeEach(() => {
    vi.clearAllMocks()
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mockClient = {
      listSecrets: vi.fn().mockResolvedValue({ secrets: [secret('api-key')] }),
    }
    ;(createClient as Mock).mockReturnValue(mockClient)
    ;(useTransport as Mock).mockReturnValue({})
    ;(useAuth as Mock).mockReturnValue({ isAuthenticated: true })
  })

  it('uses the canonical list key factory', async () => {
    const { result } = renderHook(() => useListSecrets('demo-project'), {
      wrapper: makeWrapper(queryClient),
    })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    const matches = queryClient.getQueryCache().findAll({
      queryKey: keys.secrets.list('demo-project'),
    })
    expect(matches).toHaveLength(1)
    expect(matches[0]?.queryKey).toEqual(keys.secrets.list('demo-project'))
  })
})

describe('secret mutation invalidation', () => {
  let queryClient: QueryClient
  let mockClient: Record<string, Mock>

  beforeEach(() => {
    vi.clearAllMocks()
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mockClient = {
      createSecret: vi.fn().mockResolvedValue({}),
      deleteSecret: vi.fn().mockResolvedValue({}),
      updateSecret: vi.fn().mockResolvedValue({}),
      updateSharing: vi.fn().mockResolvedValue({}),
    }
    ;(createClient as Mock).mockReturnValue(mockClient)
    ;(useTransport as Mock).mockReturnValue({})
  })

  it('invalidates list and detail keys after create', async () => {
    const invalidateSpy = vi.spyOn(queryClient, 'invalidateQueries')
    const { result } = renderHook(() => useCreateSecret('demo-project'), {
      wrapper: makeWrapper(queryClient),
    })

    await act(async () => {
      await result.current.mutateAsync({
        name: 'api-key',
        data: {},
        userGrants: [],
        roleGrants: [],
      })
    })

    expectSecretInvalidation(invalidateSpy, 'demo-project', 'api-key')
  })

  it('invalidates list and detail keys after delete', async () => {
    const invalidateSpy = vi.spyOn(queryClient, 'invalidateQueries')
    const { result } = renderHook(() => useDeleteSecret('demo-project'), {
      wrapper: makeWrapper(queryClient),
    })

    await act(async () => {
      await result.current.mutateAsync('api-key')
    })

    expectSecretInvalidation(invalidateSpy, 'demo-project', 'api-key')
  })

  it('invalidates list and detail keys after value update', async () => {
    const invalidateSpy = vi.spyOn(queryClient, 'invalidateQueries')
    const { result } = renderHook(() => useUpdateSecret('demo-project'), {
      wrapper: makeWrapper(queryClient),
    })

    await act(async () => {
      await result.current.mutateAsync({ name: 'api-key', data: {} })
    })

    expectSecretInvalidation(invalidateSpy, 'demo-project', 'api-key')
  })

  it('invalidates list and detail keys after sharing update', async () => {
    const invalidateSpy = vi.spyOn(queryClient, 'invalidateQueries')
    const { result } = renderHook(() => useUpdateSecretSharing('demo-project'), {
      wrapper: makeWrapper(queryClient),
    })

    await act(async () => {
      await result.current.mutateAsync({
        name: 'api-key',
        userGrants: [],
        roleGrants: [],
      })
    })

    expectSecretInvalidation(invalidateSpy, 'demo-project', 'api-key')
  })
})

describe('secret list keep-previous-data', () => {
  let queryClient: QueryClient
  let mockClient: Record<string, Mock>

  beforeEach(() => {
    vi.clearAllMocks()
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mockClient = {
      listSecrets: vi.fn(),
    }
    ;(createClient as Mock).mockReturnValue(mockClient)
    ;(useTransport as Mock).mockReturnValue({})
    ;(useAuth as Mock).mockReturnValue({ isAuthenticated: true })
  })

  it('keeps previous list data while the next project list is loading', async () => {
    const alpha = deferred<{ secrets: SecretMetadata[] }>()
    const beta = deferred<{ secrets: SecretMetadata[] }>()
    mockClient.listSecrets.mockImplementation(({ project }: { project: string }) => {
      if (project === 'alpha') return alpha.promise
      if (project === 'beta') return beta.promise
      throw new Error(`unexpected project ${project}`)
    })

    const { result, rerender } = renderHook(
      ({ project }) => useListSecrets(project),
      {
        initialProps: { project: 'alpha' },
        wrapper: makeWrapper(queryClient),
      },
    )

    await act(async () => {
      alpha.resolve({ secrets: [secret('alpha-secret')] })
      await alpha.promise
    })
    await waitFor(() => expect(result.current.data?.[0]?.name).toBe('alpha-secret'))

    rerender({ project: 'beta' })

    await waitFor(() =>
      expect(mockClient.listSecrets).toHaveBeenCalledWith({ project: 'beta' }),
    )
    expect(result.current.data?.[0]?.name).toBe('alpha-secret')
    expect(result.current.isPlaceholderData).toBe(true)

    await act(async () => {
      beta.resolve({ secrets: [secret('beta-secret')] })
      await beta.promise
    })
    await waitFor(() => expect(result.current.data?.[0]?.name).toBe('beta-secret'))
    expect(result.current.isPlaceholderData).toBe(false)
  })
})

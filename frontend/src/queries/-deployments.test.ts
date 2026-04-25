import { renderHook, waitFor, act } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import React from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Mock } from 'vitest'
import { keys } from '@/queries/keys'
import {
  useListDeployments,
  useCreateDeployment,
  useDeleteDeployment,
  useUpdateDeployment,
} from '@/queries/deployments'

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

function deployment(name: string) {
  return {
    name,
    project: 'demo-project',
    displayName: name,
    description: '',
    image: 'ghcr.io/org/app',
    tag: 'v1.0.0',
    template: 'web-app',
    phase: 0,
    message: '',
    createdAt: '',
  }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((res) => {
    resolve = res
  })
  return { promise, resolve }
}

// ---------------------------------------------------------------------------
// Query key shape
// ---------------------------------------------------------------------------

describe('deployment query keys', () => {
  let queryClient: QueryClient
  let mockClient: Record<string, Mock>

  beforeEach(() => {
    vi.clearAllMocks()
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mockClient = {
      listDeployments: vi.fn().mockResolvedValue({ deployments: [deployment('api')] }),
    }
    ;(createClient as Mock).mockReturnValue(mockClient)
    ;(useTransport as Mock).mockReturnValue({})
    ;(useAuth as Mock).mockReturnValue({ isAuthenticated: true })
  })

  it('uses the canonical list key factory', async () => {
    const { result } = renderHook(() => useListDeployments('demo-project'), {
      wrapper: makeWrapper(queryClient),
    })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    const matches = queryClient.getQueryCache().findAll({
      queryKey: keys.deployments.list('demo-project'),
    })
    expect(matches).toHaveLength(1)
    expect(matches[0]?.queryKey).toEqual(keys.deployments.list('demo-project'))
  })

  it('is disabled when project is empty', () => {
    const { result } = renderHook(() => useListDeployments(''), {
      wrapper: makeWrapper(queryClient),
    })

    expect(result.current.fetchStatus).toBe('idle')
    expect(mockClient.listDeployments).not.toHaveBeenCalled()
  })
})

// ---------------------------------------------------------------------------
// Mutation invalidation
// ---------------------------------------------------------------------------

describe('deployment mutation invalidation', () => {
  let queryClient: QueryClient
  let mockClient: Record<string, Mock>

  beforeEach(() => {
    vi.clearAllMocks()
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mockClient = {
      createDeployment: vi.fn().mockResolvedValue({}),
      deleteDeployment: vi.fn().mockResolvedValue({}),
      updateDeployment: vi.fn().mockResolvedValue({}),
    }
    ;(createClient as Mock).mockReturnValue(mockClient)
    ;(useTransport as Mock).mockReturnValue({})
  })

  it('invalidates deployment list after create', async () => {
    const invalidateSpy = vi.spyOn(queryClient, 'invalidateQueries')
    const { result } = renderHook(() => useCreateDeployment('demo-project'), {
      wrapper: makeWrapper(queryClient),
    })

    await act(async () => {
      await result.current.mutateAsync({
        name: 'api',
        image: 'ghcr.io/org/app',
        tag: 'v1.0.0',
        template: 'web-app',
      })
    })

    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: keys.deployments.list('demo-project'),
    })
  })

  it('invalidates list only after delete', async () => {
    const invalidateSpy = vi.spyOn(queryClient, 'invalidateQueries')
    const { result } = renderHook(() => useDeleteDeployment('demo-project'), {
      wrapper: makeWrapper(queryClient),
    })

    await act(async () => {
      await result.current.mutateAsync({ name: 'api' })
    })

    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: keys.deployments.list('demo-project'),
    })
  })

  it('invalidates list, detail, and policyState after update', async () => {
    const invalidateSpy = vi.spyOn(queryClient, 'invalidateQueries')
    const { result } = renderHook(
      () => useUpdateDeployment('demo-project', 'api'),
      { wrapper: makeWrapper(queryClient) },
    )

    await act(async () => {
      await result.current.mutateAsync({ tag: 'v2.0.0' })
    })

    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: keys.deployments.list('demo-project'),
    })
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: keys.deployments.get('demo-project', 'api'),
    })
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: keys.deployments.policyState('demo-project', 'api'),
    })
  })
})

// ---------------------------------------------------------------------------
// Keep-previous-data across project param changes
// ---------------------------------------------------------------------------

describe('deployment list keep-previous-data', () => {
  let queryClient: QueryClient
  let mockClient: Record<string, Mock>

  beforeEach(() => {
    vi.clearAllMocks()
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mockClient = {
      listDeployments: vi.fn(),
    }
    ;(createClient as Mock).mockReturnValue(mockClient)
    ;(useTransport as Mock).mockReturnValue({})
    ;(useAuth as Mock).mockReturnValue({ isAuthenticated: true })
  })

  it('keeps previous list data while the next project list is loading', async () => {
    const alpha = deferred<{ deployments: ReturnType<typeof deployment>[] }>()
    const beta = deferred<{ deployments: ReturnType<typeof deployment>[] }>()
    mockClient.listDeployments.mockImplementation(({ project }: { project: string }) => {
      if (project === 'alpha') return alpha.promise
      if (project === 'beta') return beta.promise
      throw new Error(`unexpected project ${project}`)
    })

    const { result, rerender } = renderHook(
      ({ project }) => useListDeployments(project),
      {
        initialProps: { project: 'alpha' },
        wrapper: makeWrapper(queryClient),
      },
    )

    await act(async () => {
      alpha.resolve({ deployments: [deployment('alpha-dep')] })
      await alpha.promise
    })
    await waitFor(() => expect(result.current.data?.[0]?.name).toBe('alpha-dep'))

    rerender({ project: 'beta' })

    await waitFor(() =>
      expect(mockClient.listDeployments).toHaveBeenCalledWith({ project: 'beta' }),
    )
    expect(result.current.data?.[0]?.name).toBe('alpha-dep')
    expect(result.current.isPlaceholderData).toBe(true)

    await act(async () => {
      beta.resolve({ deployments: [deployment('beta-dep')] })
      await beta.promise
    })
    await waitFor(() => expect(result.current.data?.[0]?.name).toBe('beta-dep'))
    expect(result.current.isPlaceholderData).toBe(false)
  })
})

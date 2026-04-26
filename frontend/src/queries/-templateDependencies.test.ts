/**
 * Tests for templateDependencies query hooks (HOL-1019).
 *
 * Covers:
 *  - useGetTemplateDependency — success, disabled when namespace/name empty, disabled unauthenticated
 *  - useCreateTemplateDependency — calls RPC, invalidates list on success
 *  - useUpdateTemplateDependency — calls RPC, invalidates list and get on success
 */

import { renderHook, waitFor, act } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import React from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Mock } from 'vitest'
import { create } from '@bufbuild/protobuf'
import {
  TemplateDependencySchema,
} from '@/gen/holos/console/v1/template_dependencies_pb.js'
import { keys } from '@/queries/keys'
import {
  useGetTemplateDependency,
  useCreateTemplateDependency,
  useUpdateTemplateDependency,
} from '@/queries/templateDependencies'

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

const NS = 'holos-project-test-proj'
const NAME = 'dep-a'

function makeWrapper(queryClient: QueryClient) {
  return ({ children }: { children: React.ReactNode }) =>
    React.createElement(QueryClientProvider, { client: queryClient }, children)
}

// ---------------------------------------------------------------------------
// useGetTemplateDependency
// ---------------------------------------------------------------------------

describe('useGetTemplateDependency', () => {
  let queryClient: QueryClient
  let mockClient: Record<string, Mock>

  beforeEach(() => {
    vi.clearAllMocks()
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mockClient = {
      getTemplateDependency: vi.fn().mockResolvedValue({
        dependency: create(TemplateDependencySchema, { name: NAME, namespace: NS }),
      }),
    }
    ;(createClient as Mock).mockReturnValue(mockClient)
    ;(useTransport as Mock).mockReturnValue({})
    ;(useAuth as Mock).mockReturnValue({ isAuthenticated: true })
  })

  it('fetches the dependency and returns it', async () => {
    const { result } = renderHook(
      () => useGetTemplateDependency(NS, NAME),
      { wrapper: makeWrapper(queryClient) },
    )

    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    expect(mockClient.getTemplateDependency).toHaveBeenCalledWith({
      namespace: NS,
      name: NAME,
    })
    expect(result.current.data?.name).toBe(NAME)
    expect(result.current.data?.namespace).toBe(NS)
  })

  it('is disabled when namespace is empty', () => {
    const { result } = renderHook(
      () => useGetTemplateDependency('', NAME),
      { wrapper: makeWrapper(queryClient) },
    )

    expect(result.current.fetchStatus).toBe('idle')
    expect(mockClient.getTemplateDependency).not.toHaveBeenCalled()
  })

  it('is disabled when name is empty', () => {
    const { result } = renderHook(
      () => useGetTemplateDependency(NS, ''),
      { wrapper: makeWrapper(queryClient) },
    )

    expect(result.current.fetchStatus).toBe('idle')
    expect(mockClient.getTemplateDependency).not.toHaveBeenCalled()
  })

  it('is disabled when user is not authenticated', () => {
    ;(useAuth as Mock).mockReturnValue({ isAuthenticated: false })

    const { result } = renderHook(
      () => useGetTemplateDependency(NS, NAME),
      { wrapper: makeWrapper(queryClient) },
    )

    expect(result.current.fetchStatus).toBe('idle')
    expect(mockClient.getTemplateDependency).not.toHaveBeenCalled()
  })

  it('surfaces errors on RPC failure', async () => {
    const err = new Error('rpc error')
    mockClient.getTemplateDependency.mockRejectedValue(err)

    const { result } = renderHook(
      () => useGetTemplateDependency(NS, NAME),
      { wrapper: makeWrapper(queryClient) },
    )

    await waitFor(() => expect(result.current.isError).toBe(true))
    expect(result.current.error).toBe(err)
  })
})

// ---------------------------------------------------------------------------
// useCreateTemplateDependency
// ---------------------------------------------------------------------------

describe('useCreateTemplateDependency', () => {
  let queryClient: QueryClient
  let mockClient: Record<string, Mock>
  let invalidateSpy: ReturnType<typeof vi.spyOn>

  beforeEach(() => {
    vi.clearAllMocks()
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mockClient = {
      createTemplateDependency: vi.fn().mockResolvedValue({ name: NAME }),
    }
    ;(createClient as Mock).mockReturnValue(mockClient)
    ;(useTransport as Mock).mockReturnValue({})
    invalidateSpy = vi.spyOn(queryClient, 'invalidateQueries')
  })

  it('calls createTemplateDependency RPC with correct params', async () => {
    const { result } = renderHook(
      () => useCreateTemplateDependency(NS),
      { wrapper: makeWrapper(queryClient) },
    )

    await act(async () => {
      await result.current.mutateAsync({ name: NAME })
    })

    expect(mockClient.createTemplateDependency).toHaveBeenCalledWith(
      expect.objectContaining({
        namespace: NS,
        dependency: expect.objectContaining({ name: NAME, namespace: NS }),
      }),
    )
  })

  it('invalidates the list query on success', async () => {
    const { result } = renderHook(
      () => useCreateTemplateDependency(NS),
      { wrapper: makeWrapper(queryClient) },
    )

    await act(async () => {
      await result.current.mutateAsync({ name: NAME })
    })

    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: keys.templateDependencies.list(NS),
    })
  })

  it('surfaces errors on RPC failure', async () => {
    const err = new Error('create failed')
    mockClient.createTemplateDependency.mockRejectedValue(err)

    const { result } = renderHook(
      () => useCreateTemplateDependency(NS),
      { wrapper: makeWrapper(queryClient) },
    )

    await act(async () => {
      await expect(result.current.mutateAsync({ name: NAME })).rejects.toThrow('create failed')
    })
  })
})

// ---------------------------------------------------------------------------
// useUpdateTemplateDependency
// ---------------------------------------------------------------------------

describe('useUpdateTemplateDependency', () => {
  let queryClient: QueryClient
  let mockClient: Record<string, Mock>
  let invalidateSpy: ReturnType<typeof vi.spyOn>

  beforeEach(() => {
    vi.clearAllMocks()
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mockClient = {
      updateTemplateDependency: vi.fn().mockResolvedValue({}),
    }
    ;(createClient as Mock).mockReturnValue(mockClient)
    ;(useTransport as Mock).mockReturnValue({})
    invalidateSpy = vi.spyOn(queryClient, 'invalidateQueries')
  })

  it('calls updateTemplateDependency RPC with correct params', async () => {
    const { result } = renderHook(
      () => useUpdateTemplateDependency(NS, NAME),
      { wrapper: makeWrapper(queryClient) },
    )

    await act(async () => {
      await result.current.mutateAsync({ cascadeDelete: false })
    })

    expect(mockClient.updateTemplateDependency).toHaveBeenCalledWith(
      expect.objectContaining({
        namespace: NS,
        dependency: expect.objectContaining({ name: NAME, namespace: NS }),
      }),
    )
  })

  it('invalidates list and get keys on success', async () => {
    const { result } = renderHook(
      () => useUpdateTemplateDependency(NS, NAME),
      { wrapper: makeWrapper(queryClient) },
    )

    await act(async () => {
      await result.current.mutateAsync({})
    })

    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: keys.templateDependencies.list(NS),
    })
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: keys.templateDependencies.get(NS, NAME),
    })
  })

  it('surfaces errors on RPC failure', async () => {
    const err = new Error('update failed')
    mockClient.updateTemplateDependency.mockRejectedValue(err)

    const { result } = renderHook(
      () => useUpdateTemplateDependency(NS, NAME),
      { wrapper: makeWrapper(queryClient) },
    )

    await act(async () => {
      await expect(result.current.mutateAsync({})).rejects.toThrow('update failed')
    })
  })
})

/**
 * Tests for templateGrants query hooks (HOL-1019).
 *
 * Covers:
 *  - useGetTemplateGrant — success, disabled when namespace/name empty, disabled unauthenticated
 *  - useCreateTemplateGrant — calls RPC, invalidates list on success
 *  - useUpdateTemplateGrant — calls RPC, invalidates list and get on success
 */

import { renderHook, waitFor, act } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import React from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Mock } from 'vitest'
import { create } from '@bufbuild/protobuf'
import {
  TemplateGrantSchema,
} from '@/gen/holos/console/v1/template_grants_pb.js'
import { keys } from '@/queries/keys'
import {
  useGetTemplateGrant,
  useCreateTemplateGrant,
  useUpdateTemplateGrant,
} from '@/queries/templateGrants'

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

const NS = 'holos-org-test-org'
const NAME = 'grant-a'

function makeWrapper(queryClient: QueryClient) {
  return ({ children }: { children: React.ReactNode }) =>
    React.createElement(QueryClientProvider, { client: queryClient }, children)
}

// ---------------------------------------------------------------------------
// useGetTemplateGrant
// ---------------------------------------------------------------------------

describe('useGetTemplateGrant', () => {
  let queryClient: QueryClient
  let mockClient: Record<string, Mock>

  beforeEach(() => {
    vi.clearAllMocks()
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mockClient = {
      getTemplateGrant: vi.fn().mockResolvedValue({
        grant: create(TemplateGrantSchema, { name: NAME, namespace: NS }),
      }),
    }
    ;(createClient as Mock).mockReturnValue(mockClient)
    ;(useTransport as Mock).mockReturnValue({})
    ;(useAuth as Mock).mockReturnValue({ isAuthenticated: true })
  })

  it('fetches the grant and returns it', async () => {
    const { result } = renderHook(
      () => useGetTemplateGrant(NS, NAME),
      { wrapper: makeWrapper(queryClient) },
    )

    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    expect(mockClient.getTemplateGrant).toHaveBeenCalledWith({
      namespace: NS,
      name: NAME,
    })
    expect(result.current.data?.name).toBe(NAME)
    expect(result.current.data?.namespace).toBe(NS)
  })

  it('is disabled when namespace is empty', () => {
    const { result } = renderHook(
      () => useGetTemplateGrant('', NAME),
      { wrapper: makeWrapper(queryClient) },
    )

    expect(result.current.fetchStatus).toBe('idle')
    expect(mockClient.getTemplateGrant).not.toHaveBeenCalled()
  })

  it('is disabled when name is empty', () => {
    const { result } = renderHook(
      () => useGetTemplateGrant(NS, ''),
      { wrapper: makeWrapper(queryClient) },
    )

    expect(result.current.fetchStatus).toBe('idle')
    expect(mockClient.getTemplateGrant).not.toHaveBeenCalled()
  })

  it('is disabled when user is not authenticated', () => {
    ;(useAuth as Mock).mockReturnValue({ isAuthenticated: false })

    const { result } = renderHook(
      () => useGetTemplateGrant(NS, NAME),
      { wrapper: makeWrapper(queryClient) },
    )

    expect(result.current.fetchStatus).toBe('idle')
    expect(mockClient.getTemplateGrant).not.toHaveBeenCalled()
  })

  it('surfaces errors on RPC failure', async () => {
    const err = new Error('rpc error')
    mockClient.getTemplateGrant.mockRejectedValue(err)

    const { result } = renderHook(
      () => useGetTemplateGrant(NS, NAME),
      { wrapper: makeWrapper(queryClient) },
    )

    await waitFor(() => expect(result.current.isError).toBe(true))
    expect(result.current.error).toBe(err)
  })
})

// ---------------------------------------------------------------------------
// useCreateTemplateGrant
// ---------------------------------------------------------------------------

describe('useCreateTemplateGrant', () => {
  let queryClient: QueryClient
  let mockClient: Record<string, Mock>
  let invalidateSpy: ReturnType<typeof vi.spyOn>

  beforeEach(() => {
    vi.clearAllMocks()
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mockClient = {
      createTemplateGrant: vi.fn().mockResolvedValue({ name: NAME }),
    }
    ;(createClient as Mock).mockReturnValue(mockClient)
    ;(useTransport as Mock).mockReturnValue({})
    invalidateSpy = vi.spyOn(queryClient, 'invalidateQueries')
  })

  it('calls createTemplateGrant RPC with correct params', async () => {
    const { result } = renderHook(
      () => useCreateTemplateGrant(NS),
      { wrapper: makeWrapper(queryClient) },
    )

    await act(async () => {
      await result.current.mutateAsync({ name: NAME })
    })

    expect(mockClient.createTemplateGrant).toHaveBeenCalledWith(
      expect.objectContaining({
        namespace: NS,
        grant: expect.objectContaining({ name: NAME, namespace: NS }),
      }),
    )
  })

  it('invalidates the list query on success', async () => {
    const { result } = renderHook(
      () => useCreateTemplateGrant(NS),
      { wrapper: makeWrapper(queryClient) },
    )

    await act(async () => {
      await result.current.mutateAsync({ name: NAME })
    })

    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: keys.templateGrants.list(NS),
    })
  })

  it('surfaces errors on RPC failure', async () => {
    const err = new Error('create failed')
    mockClient.createTemplateGrant.mockRejectedValue(err)

    const { result } = renderHook(
      () => useCreateTemplateGrant(NS),
      { wrapper: makeWrapper(queryClient) },
    )

    await act(async () => {
      await expect(result.current.mutateAsync({ name: NAME })).rejects.toThrow('create failed')
    })
  })
})

// ---------------------------------------------------------------------------
// useUpdateTemplateGrant
// ---------------------------------------------------------------------------

describe('useUpdateTemplateGrant', () => {
  let queryClient: QueryClient
  let mockClient: Record<string, Mock>
  let invalidateSpy: ReturnType<typeof vi.spyOn>

  beforeEach(() => {
    vi.clearAllMocks()
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mockClient = {
      updateTemplateGrant: vi.fn().mockResolvedValue({}),
    }
    ;(createClient as Mock).mockReturnValue(mockClient)
    ;(useTransport as Mock).mockReturnValue({})
    invalidateSpy = vi.spyOn(queryClient, 'invalidateQueries')
  })

  it('calls updateTemplateGrant RPC with correct params', async () => {
    const { result } = renderHook(
      () => useUpdateTemplateGrant(NS, NAME),
      { wrapper: makeWrapper(queryClient) },
    )

    await act(async () => {
      await result.current.mutateAsync({ from: [{ namespace: 'proj-ns' }] })
    })

    expect(mockClient.updateTemplateGrant).toHaveBeenCalledWith(
      expect.objectContaining({
        namespace: NS,
        grant: expect.objectContaining({ name: NAME, namespace: NS }),
      }),
    )
  })

  it('invalidates list and get keys on success', async () => {
    const { result } = renderHook(
      () => useUpdateTemplateGrant(NS, NAME),
      { wrapper: makeWrapper(queryClient) },
    )

    await act(async () => {
      await result.current.mutateAsync({})
    })

    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: keys.templateGrants.list(NS),
    })
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: keys.templateGrants.get(NS, NAME),
    })
  })

  it('surfaces errors on RPC failure', async () => {
    const err = new Error('update failed')
    mockClient.updateTemplateGrant.mockRejectedValue(err)

    const { result } = renderHook(
      () => useUpdateTemplateGrant(NS, NAME),
      { wrapper: makeWrapper(queryClient) },
    )

    await act(async () => {
      await expect(result.current.mutateAsync({})).rejects.toThrow('update failed')
    })
  })
})

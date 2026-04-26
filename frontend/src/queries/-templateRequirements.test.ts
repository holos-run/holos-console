/**
 * Tests for templateRequirements query hooks (HOL-1019).
 *
 * Covers:
 *  - useGetTemplateRequirement — success, disabled when namespace/name empty, disabled unauthenticated
 *  - useCreateTemplateRequirement — calls RPC, invalidates list on success
 *  - useUpdateTemplateRequirement — calls RPC, invalidates list and get on success
 */

import { renderHook, waitFor, act } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import React from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Mock } from 'vitest'
import { create } from '@bufbuild/protobuf'
import {
  TemplateRequirementSchema,
} from '@/gen/holos/console/v1/template_requirements_pb.js'
import { keys } from '@/queries/keys'
import {
  useGetTemplateRequirement,
  useCreateTemplateRequirement,
  useUpdateTemplateRequirement,
} from '@/queries/templateRequirements'

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
const NAME = 'req-a'

function makeWrapper(queryClient: QueryClient) {
  return ({ children }: { children: React.ReactNode }) =>
    React.createElement(QueryClientProvider, { client: queryClient }, children)
}

// ---------------------------------------------------------------------------
// useGetTemplateRequirement
// ---------------------------------------------------------------------------

describe('useGetTemplateRequirement', () => {
  let queryClient: QueryClient
  let mockClient: Record<string, Mock>

  beforeEach(() => {
    vi.clearAllMocks()
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mockClient = {
      getTemplateRequirement: vi.fn().mockResolvedValue({
        requirement: create(TemplateRequirementSchema, { name: NAME, namespace: NS }),
      }),
    }
    ;(createClient as Mock).mockReturnValue(mockClient)
    ;(useTransport as Mock).mockReturnValue({})
    ;(useAuth as Mock).mockReturnValue({ isAuthenticated: true })
  })

  it('fetches the requirement and returns it', async () => {
    const { result } = renderHook(
      () => useGetTemplateRequirement(NS, NAME),
      { wrapper: makeWrapper(queryClient) },
    )

    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    expect(mockClient.getTemplateRequirement).toHaveBeenCalledWith({
      namespace: NS,
      name: NAME,
    })
    expect(result.current.data?.name).toBe(NAME)
    expect(result.current.data?.namespace).toBe(NS)
  })

  it('is disabled when namespace is empty', () => {
    const { result } = renderHook(
      () => useGetTemplateRequirement('', NAME),
      { wrapper: makeWrapper(queryClient) },
    )

    expect(result.current.fetchStatus).toBe('idle')
    expect(mockClient.getTemplateRequirement).not.toHaveBeenCalled()
  })

  it('is disabled when name is empty', () => {
    const { result } = renderHook(
      () => useGetTemplateRequirement(NS, ''),
      { wrapper: makeWrapper(queryClient) },
    )

    expect(result.current.fetchStatus).toBe('idle')
    expect(mockClient.getTemplateRequirement).not.toHaveBeenCalled()
  })

  it('is disabled when user is not authenticated', () => {
    ;(useAuth as Mock).mockReturnValue({ isAuthenticated: false })

    const { result } = renderHook(
      () => useGetTemplateRequirement(NS, NAME),
      { wrapper: makeWrapper(queryClient) },
    )

    expect(result.current.fetchStatus).toBe('idle')
    expect(mockClient.getTemplateRequirement).not.toHaveBeenCalled()
  })

  it('surfaces errors on RPC failure', async () => {
    const err = new Error('rpc error')
    mockClient.getTemplateRequirement.mockRejectedValue(err)

    const { result } = renderHook(
      () => useGetTemplateRequirement(NS, NAME),
      { wrapper: makeWrapper(queryClient) },
    )

    await waitFor(() => expect(result.current.isError).toBe(true))
    expect(result.current.error).toBe(err)
  })
})

// ---------------------------------------------------------------------------
// useCreateTemplateRequirement
// ---------------------------------------------------------------------------

describe('useCreateTemplateRequirement', () => {
  let queryClient: QueryClient
  let mockClient: Record<string, Mock>
  let invalidateSpy: ReturnType<typeof vi.spyOn>

  beforeEach(() => {
    vi.clearAllMocks()
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mockClient = {
      createTemplateRequirement: vi.fn().mockResolvedValue({ name: NAME }),
    }
    ;(createClient as Mock).mockReturnValue(mockClient)
    ;(useTransport as Mock).mockReturnValue({})
    invalidateSpy = vi.spyOn(queryClient, 'invalidateQueries')
  })

  it('calls createTemplateRequirement RPC with correct params', async () => {
    const { result } = renderHook(
      () => useCreateTemplateRequirement(NS),
      { wrapper: makeWrapper(queryClient) },
    )

    await act(async () => {
      await result.current.mutateAsync({ name: NAME })
    })

    expect(mockClient.createTemplateRequirement).toHaveBeenCalledWith(
      expect.objectContaining({
        namespace: NS,
        requirement: expect.objectContaining({ name: NAME, namespace: NS }),
      }),
    )
  })

  it('invalidates the list query on success', async () => {
    const { result } = renderHook(
      () => useCreateTemplateRequirement(NS),
      { wrapper: makeWrapper(queryClient) },
    )

    await act(async () => {
      await result.current.mutateAsync({ name: NAME })
    })

    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: keys.templateRequirements.list(NS),
    })
  })

  it('surfaces errors on RPC failure', async () => {
    const err = new Error('create failed')
    mockClient.createTemplateRequirement.mockRejectedValue(err)

    const { result } = renderHook(
      () => useCreateTemplateRequirement(NS),
      { wrapper: makeWrapper(queryClient) },
    )

    await act(async () => {
      await expect(result.current.mutateAsync({ name: NAME })).rejects.toThrow('create failed')
    })
  })
})

// ---------------------------------------------------------------------------
// useUpdateTemplateRequirement
// ---------------------------------------------------------------------------

describe('useUpdateTemplateRequirement', () => {
  let queryClient: QueryClient
  let mockClient: Record<string, Mock>
  let invalidateSpy: ReturnType<typeof vi.spyOn>

  beforeEach(() => {
    vi.clearAllMocks()
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mockClient = {
      updateTemplateRequirement: vi.fn().mockResolvedValue({}),
    }
    ;(createClient as Mock).mockReturnValue(mockClient)
    ;(useTransport as Mock).mockReturnValue({})
    invalidateSpy = vi.spyOn(queryClient, 'invalidateQueries')
  })

  it('calls updateTemplateRequirement RPC with correct params', async () => {
    const { result } = renderHook(
      () => useUpdateTemplateRequirement(NS, NAME),
      { wrapper: makeWrapper(queryClient) },
    )

    await act(async () => {
      await result.current.mutateAsync({ cascadeDelete: true })
    })

    expect(mockClient.updateTemplateRequirement).toHaveBeenCalledWith(
      expect.objectContaining({
        namespace: NS,
        requirement: expect.objectContaining({ name: NAME, namespace: NS }),
      }),
    )
  })

  it('invalidates list and get keys on success', async () => {
    const { result } = renderHook(
      () => useUpdateTemplateRequirement(NS, NAME),
      { wrapper: makeWrapper(queryClient) },
    )

    await act(async () => {
      await result.current.mutateAsync({})
    })

    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: keys.templateRequirements.list(NS),
    })
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: keys.templateRequirements.get(NS, NAME),
    })
  })

  it('surfaces errors on RPC failure', async () => {
    const err = new Error('update failed')
    mockClient.updateTemplateRequirement.mockRejectedValue(err)

    const { result } = renderHook(
      () => useUpdateTemplateRequirement(NS, NAME),
      { wrapper: makeWrapper(queryClient) },
    )

    await act(async () => {
      await expect(result.current.mutateAsync({})).rejects.toThrow('update failed')
    })
  })
})

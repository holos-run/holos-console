import { renderHook, waitFor, act } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import React from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Mock } from 'vitest'
import { keys } from '@/queries/keys'
import {
  useListTemplates,
  useCreateTemplate,
  useDeleteTemplate,
  useUpdateTemplate,
} from '@/queries/templates'

vi.mock('@connectrpc/connect', () => ({
  createClient: vi.fn(),
}))

vi.mock('@connectrpc/connect-query', () => ({
  useTransport: vi.fn(),
}))

vi.mock('@/lib/auth', () => ({
  useAuth: vi.fn(),
}))

// useAllTemplatesForOrg depends on folder and project list hooks; mock them so
// tests that import the templates module do not need them.
vi.mock('@/queries/folders', () => ({
  useListFolders: vi.fn().mockReturnValue({ data: [], isPending: false, error: null }),
}))

vi.mock('@/queries/projects', () => ({
  useListProjectsByParent: vi.fn().mockReturnValue({ data: [], isPending: false, error: null }),
}))

import { createClient } from '@connectrpc/connect'
import { useTransport } from '@connectrpc/connect-query'
import { useAuth } from '@/lib/auth'

function makeWrapper(queryClient: QueryClient) {
  return ({ children }: { children: React.ReactNode }) =>
    React.createElement(QueryClientProvider, { client: queryClient }, children)
}

function template(name: string, namespace = 'project-demo') {
  return {
    name,
    namespace,
    displayName: name,
    description: '',
    cueTemplate: '',
    enabled: false,
  }
}

// ---------------------------------------------------------------------------
// Query key shape
// ---------------------------------------------------------------------------

describe('template query keys', () => {
  let queryClient: QueryClient
  let mockClient: Record<string, Mock>

  beforeEach(() => {
    vi.clearAllMocks()
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mockClient = {
      listTemplates: vi.fn().mockResolvedValue({ templates: [template('my-template')] }),
    }
    ;(createClient as Mock).mockReturnValue(mockClient)
    ;(useTransport as Mock).mockReturnValue({})
    ;(useAuth as Mock).mockReturnValue({ isAuthenticated: true })
  })

  it('uses the canonical list key factory', async () => {
    const { result } = renderHook(() => useListTemplates('project-demo'), {
      wrapper: makeWrapper(queryClient),
    })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    const matches = queryClient.getQueryCache().findAll({
      queryKey: keys.templates.list('project-demo'),
    })
    expect(matches).toHaveLength(1)
    expect(matches[0]?.queryKey).toEqual(keys.templates.list('project-demo'))
  })

  it('is disabled when namespace is empty', () => {
    const { result } = renderHook(() => useListTemplates(''), {
      wrapper: makeWrapper(queryClient),
    })

    expect(result.current.fetchStatus).toBe('idle')
    expect(mockClient.listTemplates).not.toHaveBeenCalled()
  })
})

// ---------------------------------------------------------------------------
// Mutation invalidation
// ---------------------------------------------------------------------------

describe('template mutation invalidation', () => {
  let queryClient: QueryClient
  let mockClient: Record<string, Mock>

  beforeEach(() => {
    vi.clearAllMocks()
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mockClient = {
      createTemplate: vi.fn().mockResolvedValue({}),
      deleteTemplate: vi.fn().mockResolvedValue({}),
      updateTemplate: vi.fn().mockResolvedValue({}),
    }
    ;(createClient as Mock).mockReturnValue(mockClient)
    ;(useTransport as Mock).mockReturnValue({})
  })

  it('invalidates template list after create', async () => {
    const invalidateSpy = vi.spyOn(queryClient, 'invalidateQueries')
    const { result } = renderHook(() => useCreateTemplate('project-demo'), {
      wrapper: makeWrapper(queryClient),
    })

    await act(async () => {
      await result.current.mutateAsync({
        name: 'my-template',
        displayName: 'My Template',
        description: 'desc',
        cueTemplate: 'platform: #PlatformInput',
      })
    })

    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: keys.templates.list('project-demo'),
    })
  })

  it('invalidates template list after delete', async () => {
    const invalidateSpy = vi.spyOn(queryClient, 'invalidateQueries')
    const { result } = renderHook(() => useDeleteTemplate('project-demo'), {
      wrapper: makeWrapper(queryClient),
    })

    await act(async () => {
      await result.current.mutateAsync({ name: 'my-template' })
    })

    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: keys.templates.list('project-demo'),
    })
  })

  it('invalidates list, detail, and policyStateScope after update', async () => {
    const invalidateSpy = vi.spyOn(queryClient, 'invalidateQueries')
    const { result } = renderHook(
      () => useUpdateTemplate('project-demo', 'my-template'),
      { wrapper: makeWrapper(queryClient) },
    )

    await act(async () => {
      await result.current.mutateAsync({ displayName: 'Updated', cueTemplate: 'platform: #PlatformInput' })
    })

    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: keys.templates.list('project-demo'),
    })
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: keys.templates.get('project-demo', 'my-template'),
    })
    // HOL-559: policyState scope invalidated so drift badge refreshes
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: keys.templates.policyStateScope('project-demo'),
    })
  })
})

// ---------------------------------------------------------------------------
// Keep-previous-data across namespace param changes
// ---------------------------------------------------------------------------

describe('template list keep-previous-data', () => {
  let queryClient: QueryClient
  let mockClient: Record<string, Mock>

  beforeEach(() => {
    vi.clearAllMocks()
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mockClient = {
      listTemplates: vi.fn(),
    }
    ;(createClient as Mock).mockReturnValue(mockClient)
    ;(useTransport as Mock).mockReturnValue({})
    ;(useAuth as Mock).mockReturnValue({ isAuthenticated: true })
  })

  it('keeps previous list data while the next namespace list is loading', async () => {
    let resolveAlpha!: (value: { templates: ReturnType<typeof template>[] }) => void
    let resolveBeta!: (value: { templates: ReturnType<typeof template>[] }) => void
    const alphaPromise = new Promise<{ templates: ReturnType<typeof template>[] }>((res) => { resolveAlpha = res })
    const betaPromise = new Promise<{ templates: ReturnType<typeof template>[] }>((res) => { resolveBeta = res })

    mockClient.listTemplates.mockImplementation(({ namespace }: { namespace: string }) => {
      if (namespace === 'project-alpha') return alphaPromise
      if (namespace === 'project-beta') return betaPromise
      throw new Error(`unexpected namespace ${namespace}`)
    })

    const { result, rerender } = renderHook(
      ({ namespace }) => useListTemplates(namespace),
      {
        initialProps: { namespace: 'project-alpha' },
        wrapper: makeWrapper(queryClient),
      },
    )

    await act(async () => {
      resolveAlpha({ templates: [template('alpha-tpl', 'project-alpha')] })
      await alphaPromise
    })
    await waitFor(() => expect(result.current.data?.[0]?.name).toBe('alpha-tpl'))

    rerender({ namespace: 'project-beta' })

    await waitFor(() =>
      expect(mockClient.listTemplates).toHaveBeenCalledWith({ namespace: 'project-beta' }),
    )
    expect(result.current.data?.[0]?.name).toBe('alpha-tpl')
    expect(result.current.isPlaceholderData).toBe(true)

    await act(async () => {
      resolveBeta({ templates: [template('beta-tpl', 'project-beta')] })
      await betaPromise
    })
    await waitFor(() => expect(result.current.data?.[0]?.name).toBe('beta-tpl'))
    expect(result.current.isPlaceholderData).toBe(false)
  })
})

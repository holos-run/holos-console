import { renderHook, waitFor, act } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import React from 'react'

vi.mock('@connectrpc/connect', () => ({
  createClient: vi.fn(),
}))

vi.mock('@connectrpc/connect-query', () => ({
  useQuery: vi.fn(),
  useTransport: vi.fn(),
}))

vi.mock('@/lib/auth', () => ({
  useAuth: vi.fn(),
}))

import { createClient } from '@connectrpc/connect'
import { useTransport } from '@connectrpc/connect-query'
import { useAuth } from '@/lib/auth'
import {
  useGetProject,
  useUpdateProject,
  useUpdateProjectSharing,
  useDeleteProject,
} from './projects'

const mockProject = {
  name: 'my-project',
  displayName: 'My Project',
  description: 'Test project',
  organization: 'my-org',
  userGrants: [],
  roleGrants: [],
}

function makeWrapper(queryClient: QueryClient) {
  return ({ children }: { children: React.ReactNode }) =>
    React.createElement(QueryClientProvider, { client: queryClient }, children)
}

describe('useGetProject', () => {
  let queryClient: QueryClient
  let mockClient: Record<string, Mock>

  beforeEach(() => {
    vi.clearAllMocks()
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mockClient = {
      getProject: vi.fn().mockResolvedValue({ project: mockProject }),
    }
    ;(createClient as Mock).mockReturnValue(mockClient)
    ;(useTransport as Mock).mockReturnValue({})
    ;(useAuth as Mock).mockReturnValue({ isAuthenticated: true })
  })

  it('calls getProject RPC with the given name', async () => {
    const { result } = renderHook(() => useGetProject('my-project'), {
      wrapper: makeWrapper(queryClient),
    })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    expect(mockClient.getProject).toHaveBeenCalledWith({ name: 'my-project' })
  })

  it('returns project data from the response', async () => {
    const { result } = renderHook(() => useGetProject('my-project'), {
      wrapper: makeWrapper(queryClient),
    })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    expect(result.current.data).toEqual(mockProject)
  })

  it('is disabled when name is empty', () => {
    const { result } = renderHook(() => useGetProject(''), {
      wrapper: makeWrapper(queryClient),
    })

    expect(result.current.fetchStatus).toBe('idle')
    expect(mockClient.getProject).not.toHaveBeenCalled()
  })
})

describe('useUpdateProject', () => {
  let queryClient: QueryClient
  let mockClient: Record<string, Mock>

  beforeEach(() => {
    vi.clearAllMocks()
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mockClient = {
      updateProject: vi.fn().mockResolvedValue({}),
    }
    ;(createClient as Mock).mockReturnValue(mockClient)
    ;(useTransport as Mock).mockReturnValue({})
  })

  it('calls updateProject RPC with correct params', async () => {
    const { result } = renderHook(() => useUpdateProject(), {
      wrapper: makeWrapper(queryClient),
    })

    await act(async () => {
      await result.current.mutateAsync({ name: 'my-project', displayName: 'Updated', description: 'New desc' })
    })

    expect(mockClient.updateProject).toHaveBeenCalledWith({
      name: 'my-project',
      displayName: 'Updated',
      description: 'New desc',
    })
  })

  it('invalidates connect-query cache on success', async () => {
    const invalidateSpy = vi.spyOn(queryClient, 'invalidateQueries')

    const { result } = renderHook(() => useUpdateProject(), {
      wrapper: makeWrapper(queryClient),
    })

    await act(async () => {
      await result.current.mutateAsync({ name: 'my-project' })
    })

    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['connect-query'] })
  })
})

describe('useUpdateProjectSharing', () => {
  let queryClient: QueryClient
  let mockClient: Record<string, Mock>

  beforeEach(() => {
    vi.clearAllMocks()
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mockClient = {
      updateProjectSharing: vi.fn().mockResolvedValue({}),
    }
    ;(createClient as Mock).mockReturnValue(mockClient)
    ;(useTransport as Mock).mockReturnValue({})
  })

  it('calls updateProjectSharing RPC with correct params', async () => {
    const { result } = renderHook(() => useUpdateProjectSharing(), {
      wrapper: makeWrapper(queryClient),
    })

    const grants = { name: 'my-project', userGrants: [{ principal: 'a@b.com', role: 2 }], roleGrants: [] }

    await act(async () => {
      await result.current.mutateAsync(grants)
    })

    expect(mockClient.updateProjectSharing).toHaveBeenCalledWith(grants)
  })

  it('invalidates connect-query cache on success', async () => {
    const invalidateSpy = vi.spyOn(queryClient, 'invalidateQueries')

    const { result } = renderHook(() => useUpdateProjectSharing(), {
      wrapper: makeWrapper(queryClient),
    })

    await act(async () => {
      await result.current.mutateAsync({ name: 'my-project', userGrants: [], roleGrants: [] })
    })

    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['connect-query'] })
  })
})

describe('useDeleteProject', () => {
  let queryClient: QueryClient
  let mockClient: Record<string, Mock>

  beforeEach(() => {
    vi.clearAllMocks()
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mockClient = {
      deleteProject: vi.fn().mockResolvedValue({}),
    }
    ;(createClient as Mock).mockReturnValue(mockClient)
    ;(useTransport as Mock).mockReturnValue({})
  })

  it('calls deleteProject RPC with correct name', async () => {
    const { result } = renderHook(() => useDeleteProject(), {
      wrapper: makeWrapper(queryClient),
    })

    await act(async () => {
      await result.current.mutateAsync({ name: 'my-project' })
    })

    expect(mockClient.deleteProject).toHaveBeenCalledWith({ name: 'my-project' })
  })

  it('invalidates connect-query cache on success', async () => {
    const invalidateSpy = vi.spyOn(queryClient, 'invalidateQueries')

    const { result } = renderHook(() => useDeleteProject(), {
      wrapper: makeWrapper(queryClient),
    })

    await act(async () => {
      await result.current.mutateAsync({ name: 'my-project' })
    })

    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['connect-query'] })
  })
})

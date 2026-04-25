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
import { keys } from '@/queries/keys'
import {
  useGetOrganization,
  useUpdateOrganization,
  useUpdateOrganizationSharing,
  useUpdateOrganizationDefaultSharing,
  useDeleteOrganization,
} from './organizations'

const mockOrg = {
  name: 'my-org',
  displayName: 'My Org',
  description: 'Test org',
  userGrants: [],
  roleGrants: [],
}

function makeWrapper(queryClient: QueryClient) {
  return ({ children }: { children: React.ReactNode }) =>
    React.createElement(QueryClientProvider, { client: queryClient }, children)
}

describe('useGetOrganization', () => {
  let queryClient: QueryClient
  let mockClient: Record<string, Mock>

  beforeEach(() => {
    vi.clearAllMocks()
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mockClient = {
      getOrganization: vi.fn().mockResolvedValue({ organization: mockOrg }),
    }
    ;(createClient as Mock).mockReturnValue(mockClient)
    ;(useTransport as Mock).mockReturnValue({})
    ;(useAuth as Mock).mockReturnValue({ isAuthenticated: true })
  })

  it('calls getOrganization RPC with the given name', async () => {
    const { result } = renderHook(() => useGetOrganization('my-org'), {
      wrapper: makeWrapper(queryClient),
    })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    expect(mockClient.getOrganization).toHaveBeenCalledWith({ name: 'my-org' })
  })

  it('returns organization data from the response', async () => {
    const { result } = renderHook(() => useGetOrganization('my-org'), {
      wrapper: makeWrapper(queryClient),
    })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    expect(result.current.data).toEqual(mockOrg)
  })

  it('is disabled when name is empty', () => {
    const { result } = renderHook(() => useGetOrganization(''), {
      wrapper: makeWrapper(queryClient),
    })

    expect(result.current.fetchStatus).toBe('idle')
    expect(mockClient.getOrganization).not.toHaveBeenCalled()
  })
})

describe('useUpdateOrganization', () => {
  let queryClient: QueryClient
  let mockClient: Record<string, Mock>

  beforeEach(() => {
    vi.clearAllMocks()
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mockClient = {
      updateOrganization: vi.fn().mockResolvedValue({}),
    }
    ;(createClient as Mock).mockReturnValue(mockClient)
    ;(useTransport as Mock).mockReturnValue({})
  })

  it('calls updateOrganization RPC with correct params', async () => {
    const { result } = renderHook(() => useUpdateOrganization(), {
      wrapper: makeWrapper(queryClient),
    })

    await act(async () => {
      await result.current.mutateAsync({ name: 'my-org', displayName: 'Updated', description: 'New desc' })
    })

    expect(mockClient.updateOrganization).toHaveBeenCalledWith({
      name: 'my-org',
      displayName: 'Updated',
      description: 'New desc',
    })
  })

  it('invalidates connect-query cache on success', async () => {
    const invalidateSpy = vi.spyOn(queryClient, 'invalidateQueries')

    const { result } = renderHook(() => useUpdateOrganization(), {
      wrapper: makeWrapper(queryClient),
    })

    await act(async () => {
      await result.current.mutateAsync({ name: 'my-org' })
    })

    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: keys.connect.all() })
  })
})

describe('useUpdateOrganizationSharing', () => {
  let queryClient: QueryClient
  let mockClient: Record<string, Mock>

  beforeEach(() => {
    vi.clearAllMocks()
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mockClient = {
      updateOrganizationSharing: vi.fn().mockResolvedValue({}),
    }
    ;(createClient as Mock).mockReturnValue(mockClient)
    ;(useTransport as Mock).mockReturnValue({})
  })

  it('calls updateOrganizationSharing RPC with correct params', async () => {
    const { result } = renderHook(() => useUpdateOrganizationSharing(), {
      wrapper: makeWrapper(queryClient),
    })

    const grants = { name: 'my-org', userGrants: [{ principal: 'a@b.com', role: 3 }], roleGrants: [] }

    await act(async () => {
      await result.current.mutateAsync(grants)
    })

    expect(mockClient.updateOrganizationSharing).toHaveBeenCalledWith(grants)
  })

  it('invalidates connect-query cache on success', async () => {
    const invalidateSpy = vi.spyOn(queryClient, 'invalidateQueries')

    const { result } = renderHook(() => useUpdateOrganizationSharing(), {
      wrapper: makeWrapper(queryClient),
    })

    await act(async () => {
      await result.current.mutateAsync({ name: 'my-org', userGrants: [], roleGrants: [] })
    })

    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: keys.connect.all() })
  })
})

describe('useUpdateOrganizationDefaultSharing', () => {
  let queryClient: QueryClient
  let mockClient: Record<string, Mock>

  beforeEach(() => {
    vi.clearAllMocks()
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mockClient = {
      updateOrganizationDefaultSharing: vi.fn().mockResolvedValue({}),
    }
    ;(createClient as Mock).mockReturnValue(mockClient)
    ;(useTransport as Mock).mockReturnValue({})
  })

  it('calls updateOrganizationDefaultSharing RPC with correct params', async () => {
    const { result } = renderHook(() => useUpdateOrganizationDefaultSharing(), {
      wrapper: makeWrapper(queryClient),
    })

    const params = {
      name: 'my-org',
      defaultUserGrants: [{ principal: 'a@b.com', role: 3 }],
      defaultRoleGrants: [],
    }

    await act(async () => {
      await result.current.mutateAsync(params)
    })

    expect(mockClient.updateOrganizationDefaultSharing).toHaveBeenCalledWith(params)
  })

  it('invalidates connect-query cache on success', async () => {
    const invalidateSpy = vi.spyOn(queryClient, 'invalidateQueries')

    const { result } = renderHook(() => useUpdateOrganizationDefaultSharing(), {
      wrapper: makeWrapper(queryClient),
    })

    await act(async () => {
      await result.current.mutateAsync({ name: 'my-org', defaultUserGrants: [], defaultRoleGrants: [] })
    })

    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: keys.connect.all() })
  })
})

describe('useDeleteOrganization', () => {
  let queryClient: QueryClient
  let mockClient: Record<string, Mock>

  beforeEach(() => {
    vi.clearAllMocks()
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mockClient = {
      deleteOrganization: vi.fn().mockResolvedValue({}),
    }
    ;(createClient as Mock).mockReturnValue(mockClient)
    ;(useTransport as Mock).mockReturnValue({})
  })

  it('calls deleteOrganization RPC with correct name', async () => {
    const { result } = renderHook(() => useDeleteOrganization(), {
      wrapper: makeWrapper(queryClient),
    })

    await act(async () => {
      await result.current.mutateAsync({ name: 'my-org' })
    })

    expect(mockClient.deleteOrganization).toHaveBeenCalledWith({ name: 'my-org' })
  })

  it('invalidates connect-query cache on success', async () => {
    const invalidateSpy = vi.spyOn(queryClient, 'invalidateQueries')

    const { result } = renderHook(() => useDeleteOrganization(), {
      wrapper: makeWrapper(queryClient),
    })

    await act(async () => {
      await result.current.mutateAsync({ name: 'my-org' })
    })

    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: keys.connect.all() })
  })
})

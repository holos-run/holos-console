import { renderHook, waitFor } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import type { Mock } from 'vitest'
import { create } from '@bufbuild/protobuf'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import React from 'react'
import {
  TemplatePolicySchema,
  type TemplatePolicy,
} from '@/gen/holos/console/v1/template_policies_pb.js'
import { namespaceForFolder, namespaceForOrg } from '@/lib/scope-labels'
import { aggregateFanOut, type FanOutQueryState, useListLinkableTemplatePolicies } from './templatePolicies'

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

function policy(name: string, namespace: string): TemplatePolicy {
  return create(TemplatePolicySchema, { name, namespace })
}

function state(
  overrides: Partial<FanOutQueryState<TemplatePolicy[]>> = {},
): FanOutQueryState<TemplatePolicy[]> {
  return {
    data: undefined,
    error: null,
    isPending: false,
    fetchStatus: 'idle',
    ...overrides,
  }
}

describe('aggregateFanOut', () => {
  it('returns empty list when all queries are disabled (idle)', () => {
    const result = aggregateFanOut<TemplatePolicy>([
      state({ isPending: true, fetchStatus: 'idle' }),
      state({ isPending: true, fetchStatus: 'idle' }),
    ])
    expect(result.isPending).toBe(false)
    expect(result.error).toBeNull()
    expect(result.data).toEqual([])
  })

  it('reports pending while any active query is still fetching on first load', () => {
    const result = aggregateFanOut<TemplatePolicy>([
      state({ isPending: true, fetchStatus: 'fetching' }),
    ])
    expect(result.isPending).toBe(true)
    expect(result.data).toBeUndefined()
  })

  it('returns org-scoped policies when only org resolves', () => {
    const p = policy('org-policy', namespaceForOrg('test-org'))
    const result = aggregateFanOut<TemplatePolicy>([
      state({ data: [p], fetchStatus: 'idle' }),
    ])
    expect(result.isPending).toBe(false)
    expect(result.data).toEqual([p])
  })

  it('concatenates org + folder results', () => {
    const org = policy('org-policy', namespaceForOrg('test-org'))
    const folder = policy('fld-policy', namespaceForFolder('team-alpha'))
    const result = aggregateFanOut<TemplatePolicy>([
      state({ data: [org], fetchStatus: 'idle' }),
      state({ data: [folder], fetchStatus: 'idle' }),
    ])
    expect(result.data).toEqual([org, folder])
  })

  it('keeps partial data when one query fails', () => {
    const org = policy('org-policy', namespaceForOrg('test-org'))
    const err = new Error('folder fetch failed')
    const result = aggregateFanOut<TemplatePolicy>([
      state({ data: [org], fetchStatus: 'idle' }),
      state({ error: err, fetchStatus: 'idle' }),
    ])
    expect(result.error).toBe(err)
    expect(result.data).toEqual([org])
  })

  it('wraps non-Error errors into Error', () => {
    const result = aggregateFanOut<TemplatePolicy>([
      state({ error: 'string error', fetchStatus: 'idle' }),
    ])
    expect(result.error).toBeInstanceOf(Error)
    expect(result.error?.message).toBe('string error')
  })

  it('does not report pending when data is already materialized', () => {
    const p = policy('org-policy', namespaceForOrg('test-org'))
    const result = aggregateFanOut<TemplatePolicy>([
      state({ data: [p], fetchStatus: 'idle' }),
      state({ isPending: true, fetchStatus: 'fetching' }),
    ])
    expect(result.isPending).toBe(false)
    expect(result.data).toEqual([p])
  })

  it('handles empty input as resolved-empty', () => {
    const result = aggregateFanOut<TemplatePolicy>([])
    expect(result.isPending).toBe(false)
    expect(result.error).toBeNull()
    expect(result.data).toEqual([])
  })
})

describe('useListLinkableTemplatePolicies', () => {
  const ORG_NS = namespaceForOrg('test-org')
  const FOLDER_NS = namespaceForFolder('team-alpha')

  const folderPolicy = create(TemplatePolicySchema, {
    name: 'folder-policy',
    namespace: FOLDER_NS,
  })
  const orgPolicy = create(TemplatePolicySchema, {
    name: 'org-policy',
    namespace: ORG_NS,
  })

  let queryClient: QueryClient
  let mockClient: Record<string, Mock>

  beforeEach(() => {
    vi.clearAllMocks()
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mockClient = {
      listLinkableTemplatePolicies: vi.fn().mockResolvedValue({
        policies: [
          { policy: folderPolicy },
          { policy: orgPolicy },
        ],
      }),
    }
    ;(createClient as Mock).mockReturnValue(mockClient)
    ;(useTransport as Mock).mockReturnValue({})
    ;(useAuth as Mock).mockReturnValue({ isAuthenticated: true })
  })

  it('calls ListLinkableTemplatePolicies RPC with includeSelfScope: true', async () => {
    const { result } = renderHook(() => useListLinkableTemplatePolicies(FOLDER_NS), {
      wrapper: makeWrapper(queryClient),
    })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    expect(mockClient.listLinkableTemplatePolicies).toHaveBeenCalledWith({
      namespace: FOLDER_NS,
      includeSelfScope: true,
    })
  })

  it('returns LinkableTemplatePolicy[] from the response', async () => {
    const { result } = renderHook(() => useListLinkableTemplatePolicies(FOLDER_NS), {
      wrapper: makeWrapper(queryClient),
    })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    expect(result.current.data).toHaveLength(2)
    expect(result.current.data?.[0].policy?.name).toBe('folder-policy')
    expect(result.current.data?.[1].policy?.name).toBe('org-policy')
    // Each item carries the owning namespace for scope badge rendering.
    expect(result.current.data?.[0].policy?.namespace).toBe(FOLDER_NS)
    expect(result.current.data?.[1].policy?.namespace).toBe(ORG_NS)
  })

  it('is disabled when namespace is empty', () => {
    const { result } = renderHook(() => useListLinkableTemplatePolicies(''), {
      wrapper: makeWrapper(queryClient),
    })

    expect(result.current.fetchStatus).toBe('idle')
    expect(mockClient.listLinkableTemplatePolicies).not.toHaveBeenCalled()
  })

  it('is disabled when user is not authenticated', () => {
    ;(useAuth as Mock).mockReturnValue({ isAuthenticated: false })

    const { result } = renderHook(() => useListLinkableTemplatePolicies(FOLDER_NS), {
      wrapper: makeWrapper(queryClient),
    })

    expect(result.current.fetchStatus).toBe('idle')
    expect(mockClient.listLinkableTemplatePolicies).not.toHaveBeenCalled()
  })
})

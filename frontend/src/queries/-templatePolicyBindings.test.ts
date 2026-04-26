/**
 * Tests for templatePolicyBindings query hooks (HOL-972).
 *
 * Covers:
 *  - invalidateDeploymentPreviews helper — wildcard and specific-ref cases
 *  - useCreateTemplatePolicyBinding — invalidates bindings list AND deployment previews
 *  - useUpdateTemplatePolicyBinding — invalidates bindings list, get, AND deployment previews
 */

import { renderHook, act } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { create } from '@bufbuild/protobuf'
import React from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Mock } from 'vitest'
import {
  TemplatePolicyBindingTargetRefSchema,
  TemplatePolicyBindingTargetKind,
  LinkedTemplatePolicyRefSchema,
} from '@/gen/holos/console/v1/template_policy_bindings_pb.js'
import { keys } from '@/queries/keys'
import {
  invalidateDeploymentPreviews,
  useCreateTemplatePolicyBinding,
  useUpdateTemplatePolicyBinding,
} from '@/queries/templatePolicyBindings'

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

function makeWrapper(queryClient: QueryClient) {
  return ({ children }: { children: React.ReactNode }) =>
    React.createElement(QueryClientProvider, { client: queryClient }, children)
}

function makeTargetRef(
  kind: TemplatePolicyBindingTargetKind,
  projectName: string,
  name: string,
) {
  return create(TemplatePolicyBindingTargetRefSchema, { kind, projectName, name })
}

function makePolicyRef(namespace: string, name: string) {
  return create(LinkedTemplatePolicyRefSchema, { namespace, name })
}

// ---------------------------------------------------------------------------
// invalidateDeploymentPreviews unit tests
// ---------------------------------------------------------------------------

describe('invalidateDeploymentPreviews', () => {
  let queryClient: QueryClient
  let invalidateSpy: ReturnType<typeof vi.spyOn>

  beforeEach(() => {
    vi.clearAllMocks()
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    invalidateSpy = vi.spyOn(queryClient, 'invalidateQueries')
  })

  it('invalidates the full render-preview subtree when a wildcard projectName is present', () => {
    const refs = [
      makeTargetRef(TemplatePolicyBindingTargetKind.DEPLOYMENT, '*', 'api'),
    ]
    invalidateDeploymentPreviews(queryClient, refs)
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: ['deployments', 'render-preview'],
    })
  })

  it('invalidates the full render-preview subtree when a wildcard name is present', () => {
    const refs = [
      makeTargetRef(TemplatePolicyBindingTargetKind.DEPLOYMENT, 'proj-a', '*'),
    ]
    invalidateDeploymentPreviews(queryClient, refs)
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: ['deployments', 'render-preview'],
    })
  })

  it('invalidates the full render-preview subtree when both fields are wildcards', () => {
    const refs = [
      makeTargetRef(TemplatePolicyBindingTargetKind.DEPLOYMENT, '*', '*'),
    ]
    invalidateDeploymentPreviews(queryClient, refs)
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: ['deployments', 'render-preview'],
    })
  })

  it('invalidates the full render-preview subtree when targetRefs is empty', () => {
    invalidateDeploymentPreviews(queryClient, [])
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: ['deployments', 'render-preview'],
    })
  })

  it('invalidates a specific deployment preview for a DEPLOYMENT-kind ref', () => {
    const refs = [
      makeTargetRef(TemplatePolicyBindingTargetKind.DEPLOYMENT, 'proj-a', 'api'),
    ]
    invalidateDeploymentPreviews(queryClient, refs)
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: keys.deployments.renderPreview('proj-a', 'api'),
    })
    // Must NOT blow away the entire subtree.
    expect(invalidateSpy).not.toHaveBeenCalledWith({
      queryKey: ['deployments', 'render-preview'],
    })
  })

  it('invalidates per-project preview subtree for PROJECT_TEMPLATE-kind ref', () => {
    const refs = [
      makeTargetRef(TemplatePolicyBindingTargetKind.PROJECT_TEMPLATE, 'proj-a', 'web-app'),
    ]
    invalidateDeploymentPreviews(queryClient, refs)
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: ['deployments', 'render-preview', 'proj-a'],
    })
  })

  it('invalidates multiple specific deployment previews when multiple concrete refs present', () => {
    const refs = [
      makeTargetRef(TemplatePolicyBindingTargetKind.DEPLOYMENT, 'proj-a', 'api'),
      makeTargetRef(TemplatePolicyBindingTargetKind.DEPLOYMENT, 'proj-b', 'worker'),
    ]
    invalidateDeploymentPreviews(queryClient, refs)
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: keys.deployments.renderPreview('proj-a', 'api'),
    })
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: keys.deployments.renderPreview('proj-b', 'worker'),
    })
  })

  it('falls back to full subtree invalidation when any ref in a mixed list has a wildcard', () => {
    const refs = [
      makeTargetRef(TemplatePolicyBindingTargetKind.DEPLOYMENT, 'proj-a', 'api'),
      makeTargetRef(TemplatePolicyBindingTargetKind.DEPLOYMENT, '*', 'worker'),
    ]
    invalidateDeploymentPreviews(queryClient, refs)
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: ['deployments', 'render-preview'],
    })
  })
})

// ---------------------------------------------------------------------------
// Mutation hook invalidation tests
// ---------------------------------------------------------------------------

describe('useCreateTemplatePolicyBinding mutation invalidation', () => {
  let queryClient: QueryClient
  let mockClient: Record<string, Mock>
  let invalidateSpy: ReturnType<typeof vi.spyOn>

  beforeEach(() => {
    vi.clearAllMocks()
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mockClient = {
      createTemplatePolicyBinding: vi.fn().mockResolvedValue({}),
    }
    ;(createClient as Mock).mockReturnValue(mockClient)
    ;(useTransport as Mock).mockReturnValue({})
    invalidateSpy = vi.spyOn(queryClient, 'invalidateQueries')
  })

  it('invalidates the bindings list on successful create', async () => {
    const NS = 'holos-org-test-org'
    const { result } = renderHook(
      () => useCreateTemplatePolicyBinding(NS),
      { wrapper: makeWrapper(queryClient) },
    )

    await act(async () => {
      await result.current.mutateAsync({
        name: 'bind-a',
        displayName: 'Bind A',
        description: '',
        policyRef: makePolicyRef(NS, 'require-http'),
        targetRefs: [
          makeTargetRef(TemplatePolicyBindingTargetKind.DEPLOYMENT, 'proj-a', 'api'),
        ],
      })
    })

    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: keys.templatePolicyBindings.list(NS),
    })
  })

  it('invalidates the specific deployment preview on create with concrete DEPLOYMENT ref', async () => {
    const NS = 'holos-org-test-org'
    const { result } = renderHook(
      () => useCreateTemplatePolicyBinding(NS),
      { wrapper: makeWrapper(queryClient) },
    )

    await act(async () => {
      await result.current.mutateAsync({
        name: 'bind-a',
        displayName: 'Bind A',
        description: '',
        policyRef: makePolicyRef(NS, 'require-http'),
        targetRefs: [
          makeTargetRef(TemplatePolicyBindingTargetKind.DEPLOYMENT, 'proj-a', 'api'),
        ],
      })
    })

    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: keys.deployments.renderPreview('proj-a', 'api'),
    })
  })

  it('invalidates the full render-preview subtree on create with a wildcard DEPLOYMENT ref', async () => {
    const NS = 'holos-org-test-org'
    const { result } = renderHook(
      () => useCreateTemplatePolicyBinding(NS),
      { wrapper: makeWrapper(queryClient) },
    )

    await act(async () => {
      await result.current.mutateAsync({
        name: 'bind-wildcard',
        displayName: 'Wildcard Bind',
        description: '',
        policyRef: makePolicyRef(NS, 'require-http'),
        targetRefs: [
          makeTargetRef(TemplatePolicyBindingTargetKind.DEPLOYMENT, '*', '*'),
        ],
      })
    })

    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: ['deployments', 'render-preview'],
    })
  })
})

describe('useUpdateTemplatePolicyBinding mutation invalidation', () => {
  let queryClient: QueryClient
  let mockClient: Record<string, Mock>
  let invalidateSpy: ReturnType<typeof vi.spyOn>

  beforeEach(() => {
    vi.clearAllMocks()
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mockClient = {
      updateTemplatePolicyBinding: vi.fn().mockResolvedValue({}),
    }
    ;(createClient as Mock).mockReturnValue(mockClient)
    ;(useTransport as Mock).mockReturnValue({})
    invalidateSpy = vi.spyOn(queryClient, 'invalidateQueries')
  })

  it('invalidates list and get keys after update', async () => {
    const NS = 'holos-org-test-org'
    const { result } = renderHook(
      () => useUpdateTemplatePolicyBinding(NS, 'bind-a'),
      { wrapper: makeWrapper(queryClient) },
    )

    await act(async () => {
      await result.current.mutateAsync({
        policyRef: makePolicyRef(NS, 'require-http'),
        targetRefs: [
          makeTargetRef(TemplatePolicyBindingTargetKind.DEPLOYMENT, 'proj-a', 'api'),
        ],
      })
    })

    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: keys.templatePolicyBindings.list(NS),
    })
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: keys.templatePolicyBindings.get(NS, 'bind-a'),
    })
  })

  it('invalidates deployment preview after update with specific DEPLOYMENT ref', async () => {
    const NS = 'holos-org-test-org'
    const { result } = renderHook(
      () => useUpdateTemplatePolicyBinding(NS, 'bind-a'),
      { wrapper: makeWrapper(queryClient) },
    )

    await act(async () => {
      await result.current.mutateAsync({
        policyRef: makePolicyRef(NS, 'require-http'),
        targetRefs: [
          makeTargetRef(TemplatePolicyBindingTargetKind.DEPLOYMENT, 'proj-a', 'api'),
        ],
      })
    })

    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: keys.deployments.renderPreview('proj-a', 'api'),
    })
  })

  it('invalidates full render-preview subtree after update with wildcard ref', async () => {
    const NS = 'holos-org-test-org'
    const { result } = renderHook(
      () => useUpdateTemplatePolicyBinding(NS, 'bind-a'),
      { wrapper: makeWrapper(queryClient) },
    )

    await act(async () => {
      await result.current.mutateAsync({
        policyRef: makePolicyRef(NS, 'require-http'),
        targetRefs: [
          makeTargetRef(TemplatePolicyBindingTargetKind.DEPLOYMENT, '*', '*'),
        ],
      })
    })

    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: ['deployments', 'render-preview'],
    })
  })
})

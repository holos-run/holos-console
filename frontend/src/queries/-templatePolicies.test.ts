import { describe, it, expect } from 'vitest'
import { create } from '@bufbuild/protobuf'
import {
  TemplatePolicySchema,
  type TemplatePolicy,
} from '@/gen/holos/console/v1/template_policies_pb.js'
import { namespaceForFolder, namespaceForOrg } from '@/lib/scope-labels'
import { aggregateFanOut, type FanOutQueryState } from './templatePolicies'

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

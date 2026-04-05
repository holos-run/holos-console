import { describe, it, expect } from 'vitest'
import { ConnectError, Code } from '@connectrpc/connect'
import { shouldRetry } from '@/lib/query-client'

describe('shouldRetry', () => {
  it('returns false for ConnectError with Unauthenticated code', () => {
    const err = new ConnectError('unauthenticated', Code.Unauthenticated)
    expect(shouldRetry(0, err)).toBe(false)
    expect(shouldRetry(1, err)).toBe(false)
    expect(shouldRetry(2, err)).toBe(false)
  })

  it('returns true for ConnectError with non-auth codes when failureCount < 3', () => {
    const err = new ConnectError('internal error', Code.Internal)
    expect(shouldRetry(0, err)).toBe(true)
    expect(shouldRetry(1, err)).toBe(true)
    expect(shouldRetry(2, err)).toBe(true)
  })

  it('returns false for ConnectError with non-auth codes when failureCount >= 3', () => {
    const err = new ConnectError('internal error', Code.Internal)
    expect(shouldRetry(3, err)).toBe(false)
  })

  it('returns true for regular Error when failureCount < 3', () => {
    const err = new Error('network error')
    expect(shouldRetry(0, err)).toBe(true)
    expect(shouldRetry(1, err)).toBe(true)
    expect(shouldRetry(2, err)).toBe(true)
  })

  it('returns false for regular Error when failureCount >= 3', () => {
    const err = new Error('network error')
    expect(shouldRetry(3, err)).toBe(false)
  })
})

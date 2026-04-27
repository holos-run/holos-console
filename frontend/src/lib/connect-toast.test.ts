import { describe, it, expect } from 'vitest'
import { ConnectError, Code } from '@connectrpc/connect'

import { connectErrorMessage, isPermissionDenied } from './connect-toast'

describe('connectErrorMessage', () => {
  it('returns the access-denied message for CodePermissionDenied', () => {
    const err = new ConnectError('forbidden: secrets.holos.run', Code.PermissionDenied)
    expect(connectErrorMessage(err)).toMatch(/access denied/i)
  })

  it('returns a re-login hint for CodeUnauthenticated', () => {
    const err = new ConnectError('unauthorized', Code.Unauthenticated)
    expect(connectErrorMessage(err)).toMatch(/sign in/i)
  })

  it('returns the raw message for other connect errors', () => {
    const err = new ConnectError('not found', Code.NotFound)
    expect(connectErrorMessage(err)).toBe('not found')
  })

  it('returns the message field for plain Error instances', () => {
    expect(connectErrorMessage(new Error('boom'))).toBe('boom')
  })

  it('coerces non-Error values via String()', () => {
    expect(connectErrorMessage('weird')).toBe('weird')
  })
})

describe('isPermissionDenied', () => {
  it('returns true for CodePermissionDenied', () => {
    expect(isPermissionDenied(new ConnectError('x', Code.PermissionDenied))).toBe(true)
  })
  it('returns false for other codes', () => {
    expect(isPermissionDenied(new ConnectError('x', Code.NotFound))).toBe(false)
  })
  it('returns false for non-ConnectError', () => {
    expect(isPermissionDenied(new Error('x'))).toBe(false)
  })
})

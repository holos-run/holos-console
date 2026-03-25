import { tokenRef, readStoredToken } from './transport'

// We can't easily test the interceptor as it's not exported, but we can
// test the tokenRef which is the shared mutable state for auth tokens.
describe('tokenRef', () => {
  afterEach(() => {
    tokenRef.current = null
  })

  it('starts with null', () => {
    expect(tokenRef.current).toBeNull()
  })

  it('can be set to a token string', () => {
    tokenRef.current = 'test-token-123'
    expect(tokenRef.current).toBe('test-token-123')
  })

  it('can be reset to null', () => {
    tokenRef.current = 'test-token-123'
    tokenRef.current = null
    expect(tokenRef.current).toBeNull()
  })
})

describe('readStoredToken', () => {
  afterEach(() => {
    sessionStorage.clear()
  })

  it('returns null when sessionStorage is empty', () => {
    expect(readStoredToken()).toBeNull()
  })

  it('returns the access_token from a valid oidc.user entry', () => {
    const futureExp = Math.floor(Date.now() / 1000) + 3600
    sessionStorage.setItem(
      'oidc.user:https://localhost:8443/dex:holos-console',
      JSON.stringify({ access_token: 'valid-token-abc', expires_at: futureExp }),
    )
    expect(readStoredToken()).toBe('valid-token-abc')
  })

  it('returns null when the stored token is expired', () => {
    const pastExp = Math.floor(Date.now() / 1000) - 60
    sessionStorage.setItem(
      'oidc.user:https://localhost:8443/dex:holos-console',
      JSON.stringify({ access_token: 'expired-token', expires_at: pastExp }),
    )
    expect(readStoredToken()).toBeNull()
  })

  it('returns null when access_token is missing from the stored entry', () => {
    sessionStorage.setItem(
      'oidc.user:https://localhost:8443/dex:holos-console',
      JSON.stringify({ expires_at: Math.floor(Date.now() / 1000) + 3600 }),
    )
    expect(readStoredToken()).toBeNull()
  })

  it('returns null when the stored JSON is malformed', () => {
    sessionStorage.setItem('oidc.user:https://localhost:8443/dex:holos-console', 'not-json')
    expect(readStoredToken()).toBeNull()
  })
})

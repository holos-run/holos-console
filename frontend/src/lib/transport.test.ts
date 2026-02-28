import { tokenRef } from './transport'

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

import { ConnectError, Code } from '@connectrpc/connect'
import type { UnaryRequest, UnaryResponse } from '@connectrpc/connect'
import { tokenRef, readStoredToken, createAuthInterceptor } from './transport'

// Mock getUserManager so tests don't need a real OIDC provider.
vi.mock('@/lib/auth/userManager', () => ({
  getUserManager: vi.fn(),
}))

import { getUserManager } from '@/lib/auth/userManager'

// Minimal stub types for ConnectRPC interceptor testing.
type MockRequest = UnaryRequest & { header: Headers }
type MockResponse = UnaryResponse

function makeMockRequest(headers?: Record<string, string>): MockRequest {
  const h = new Headers(headers)
  return { header: h } as unknown as MockRequest
}

function makeMockResponse(token?: string): MockResponse {
  return {} as unknown as MockResponse
}

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

  it('returns the id_token from a valid oidc.user entry', () => {
    const futureExp = Math.floor(Date.now() / 1000) + 3600
    sessionStorage.setItem(
      'oidc.user:https://localhost:8443/dex:holos-console',
      JSON.stringify({
        id_token: 'valid-id-token-abc',
        access_token: 'valid-access-token-xyz',
        expires_at: futureExp,
      }),
    )
    expect(readStoredToken()).toBe('valid-id-token-abc')
  })

  it('returns null when the stored token is expired', () => {
    const pastExp = Math.floor(Date.now() / 1000) - 60
    sessionStorage.setItem(
      'oidc.user:https://localhost:8443/dex:holos-console',
      JSON.stringify({ id_token: 'expired-token', expires_at: pastExp }),
    )
    expect(readStoredToken()).toBeNull()
  })

  it('returns null when id_token is missing from the stored entry', () => {
    sessionStorage.setItem(
      'oidc.user:https://localhost:8443/dex:holos-console',
      JSON.stringify({
        access_token: 'only-access',
        expires_at: Math.floor(Date.now() / 1000) + 3600,
      }),
    )
    expect(readStoredToken()).toBeNull()
  })

  it('returns null when the stored JSON is malformed', () => {
    sessionStorage.setItem('oidc.user:https://localhost:8443/dex:holos-console', 'not-json')
    expect(readStoredToken()).toBeNull()
  })
})

describe('createAuthInterceptor', () => {
  beforeEach(() => {
    tokenRef.current = null
    vi.resetAllMocks()
  })

  afterEach(() => {
    tokenRef.current = null
  })

  it('sets Authorization header when tokenRef has a token', async () => {
    tokenRef.current = 'initial-token'
    const interceptor = createAuthInterceptor()
    const req = makeMockRequest()
    const next = vi.fn().mockResolvedValue(makeMockResponse())

    await interceptor(next)(req)

    expect(req.header.get('Authorization')).toBe('Bearer initial-token')
    expect(next).toHaveBeenCalledTimes(1)
  })

  it('attaches id_token (not access_token) as Bearer when sessionStorage has both', async () => {
    // Seed sessionStorage with both an id_token and a different access_token,
    // then pick up what readStoredToken selects. The Bearer header must carry
    // the ID token, because the backend verifies aud == client_id (an
    // ID-token property) and would reject the access token.
    sessionStorage.setItem(
      'oidc.user:https://localhost:8443/dex:holos-console',
      JSON.stringify({
        id_token: 'the-id-token',
        access_token: 'the-access-token',
        expires_at: Math.floor(Date.now() / 1000) + 3600,
      }),
    )
    try {
      tokenRef.current = readStoredToken()
      const interceptor = createAuthInterceptor()
      const req = makeMockRequest()
      const next = vi.fn().mockResolvedValue(makeMockResponse())

      await interceptor(next)(req)

      expect(req.header.get('Authorization')).toBe('Bearer the-id-token')
    } finally {
      sessionStorage.clear()
    }
  })

  it('does not set Authorization header when tokenRef is null', async () => {
    tokenRef.current = null
    const interceptor = createAuthInterceptor()
    const req = makeMockRequest()
    const next = vi.fn().mockResolvedValue(makeMockResponse())

    await interceptor(next)(req)

    expect(req.header.get('Authorization')).toBeNull()
    expect(next).toHaveBeenCalledTimes(1)
  })

  it('retries request after successful signinSilent on 401', async () => {
    tokenRef.current = 'old-token'
    const freshToken = 'fresh-token'
    const mockSigninSilent = vi.fn().mockResolvedValue({ id_token: freshToken })
    vi.mocked(getUserManager).mockReturnValue({ signinSilent: mockSigninSilent } as never)

    const interceptor = createAuthInterceptor()
    const req = makeMockRequest()
    const response = makeMockResponse()

    // First call throws 401, second call succeeds.
    const next = vi
      .fn()
      .mockRejectedValueOnce(new ConnectError('unauthenticated', Code.Unauthenticated))
      .mockResolvedValueOnce(response)

    const result = await interceptor(next)(req)

    expect(mockSigninSilent).toHaveBeenCalledTimes(1)
    expect(next).toHaveBeenCalledTimes(2)
    expect(result).toBe(response)
    // tokenRef must be updated with the fresh token.
    expect(tokenRef.current).toBe(freshToken)
    // The retried request must carry the fresh token.
    expect(req.header.get('Authorization')).toBe(`Bearer ${freshToken}`)
  })

  it('renewToken refreshes the ID token on 401 (not the access token)', async () => {
    // When signinSilent returns a fresh User with both tokens, the retry
    // must carry the id_token, matching what the backend verifier expects.
    tokenRef.current = 'old-token'
    const mockSigninSilent = vi
      .fn()
      .mockResolvedValue({ id_token: 'new-id', access_token: 'new-access' })
    vi.mocked(getUserManager).mockReturnValue({ signinSilent: mockSigninSilent } as never)

    const interceptor = createAuthInterceptor()
    const req = makeMockRequest()
    const response = makeMockResponse()

    const next = vi
      .fn()
      .mockRejectedValueOnce(new ConnectError('unauthenticated', Code.Unauthenticated))
      .mockResolvedValueOnce(response)

    await interceptor(next)(req)

    expect(tokenRef.current).toBe('new-id')
    expect(req.header.get('Authorization')).toBe('Bearer new-id')
  })

  it('propagates renewal error when signinSilent fails', async () => {
    tokenRef.current = 'old-token'
    const renewalError = new Error('renewal failed')
    const mockSigninSilent = vi.fn().mockRejectedValue(renewalError)
    vi.mocked(getUserManager).mockReturnValue({ signinSilent: mockSigninSilent } as never)

    const interceptor = createAuthInterceptor()
    const req = makeMockRequest()

    const next = vi
      .fn()
      .mockRejectedValueOnce(new ConnectError('unauthenticated', Code.Unauthenticated))

    await expect(interceptor(next)(req)).rejects.toBe(renewalError)
    expect(mockSigninSilent).toHaveBeenCalledTimes(1)
    // Should not retry after failed renewal.
    expect(next).toHaveBeenCalledTimes(1)
  })

  it('does not retry a second 401 (prevents retry loops)', async () => {
    tokenRef.current = 'old-token'
    const freshToken = 'fresh-token'
    const mockSigninSilent = vi.fn().mockResolvedValue({ id_token: freshToken })
    vi.mocked(getUserManager).mockReturnValue({ signinSilent: mockSigninSilent } as never)

    const interceptor = createAuthInterceptor()
    const req = makeMockRequest()

    // Both calls throw 401 — the retry itself returns 401.
    const next = vi
      .fn()
      .mockRejectedValue(new ConnectError('unauthenticated', Code.Unauthenticated))

    await expect(interceptor(next)(req)).rejects.toBeInstanceOf(ConnectError)
    // signinSilent called once, next called twice (original + one retry), then gives up.
    expect(mockSigninSilent).toHaveBeenCalledTimes(1)
    expect(next).toHaveBeenCalledTimes(2)
  })

  it('passes through non-401 errors without attempting renewal', async () => {
    tokenRef.current = 'some-token'
    const mockSigninSilent = vi.fn()
    vi.mocked(getUserManager).mockReturnValue({ signinSilent: mockSigninSilent } as never)

    const interceptor = createAuthInterceptor()
    const req = makeMockRequest()
    const permissionError = new ConnectError('permission denied', Code.PermissionDenied)

    const next = vi.fn().mockRejectedValue(permissionError)

    await expect(interceptor(next)(req)).rejects.toBe(permissionError)
    expect(mockSigninSilent).not.toHaveBeenCalled()
    expect(next).toHaveBeenCalledTimes(1)
  })

  it('coalesces concurrent 401s into a single signinSilent call', async () => {
    tokenRef.current = 'old-token'
    const freshToken = 'fresh-token'

    // signinSilent returns a promise that resolves after a tick so concurrent
    // calls overlap.
    const mockSigninSilent = vi.fn().mockImplementation(
      () => new Promise<{ id_token: string }>((resolve) => setTimeout(() => resolve({ id_token: freshToken }), 0))
    )
    vi.mocked(getUserManager).mockReturnValue({ signinSilent: mockSigninSilent } as never)

    const interceptor = createAuthInterceptor()
    const req1 = makeMockRequest()
    const req2 = makeMockRequest()
    const response1 = makeMockResponse()
    const response2 = makeMockResponse()

    // Both requests fail with 401 on first call, succeed on second.
    const next1 = vi
      .fn()
      .mockRejectedValueOnce(new ConnectError('unauthenticated', Code.Unauthenticated))
      .mockResolvedValueOnce(response1)
    const next2 = vi
      .fn()
      .mockRejectedValueOnce(new ConnectError('unauthenticated', Code.Unauthenticated))
      .mockResolvedValueOnce(response2)

    // Fire both requests concurrently.
    const [r1, r2] = await Promise.all([interceptor(next1)(req1), interceptor(next2)(req2)])

    expect(r1).toBe(response1)
    expect(r2).toBe(response2)
    // Despite two concurrent 401s, signinSilent must only be called once.
    expect(mockSigninSilent).toHaveBeenCalledTimes(1)
  })
})

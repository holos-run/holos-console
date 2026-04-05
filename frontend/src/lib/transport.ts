import { createConnectTransport } from '@connectrpc/connect-web'
import { ConnectError, Code, type Interceptor } from '@connectrpc/connect'
import { getUserManager } from '@/lib/auth/userManager'

// readStoredToken reads the access token from sessionStorage synchronously at
// module load time. oidc-client-ts stores the user object under a key matching
// "oidc.user:<authority>:<client_id>". Reading it here ensures tokenRef is
// populated before any React effect runs, eliminating the timing race where
// child query hooks fire before AuthProvider's useEffect sets the token.
export function readStoredToken(): string | null {
  try {
    const key = Object.keys(sessionStorage).find((k) => k.startsWith('oidc.user:'))
    if (!key) return null
    const raw = sessionStorage.getItem(key)
    if (!raw) return null
    const data = JSON.parse(raw) as { access_token?: string; expires_at?: number }
    if (!data?.access_token) return null
    // Treat expired tokens as absent; silent renew in AuthProvider will fix them.
    if (data.expires_at && data.expires_at * 1000 < Date.now()) return null
    return data.access_token
  } catch {
    return null
  }
}

// Shared mutable ref for the current access token.
// Initialized synchronously from sessionStorage so that the very first RPC
// request (before any React effect runs) already carries the correct token for
// returning authenticated users. AuthProvider keeps this current on
// login/logout/token refresh via a useEffect.
export const tokenRef: { current: string | null } = { current: readStoredToken() }

// Shared promise for in-flight silent token renewal. When multiple concurrent
// requests all receive a 401, they coalesce onto this single promise so only
// one signinSilent() OIDC flow is initiated.
let renewalPromise: Promise<string> | null = null

// renewToken performs a single serialized silent token renewal via
// oidc-client-ts. Concurrent callers all share the same promise, ensuring only
// one OIDC silent-renew flow runs at a time.
async function renewToken(): Promise<string> {
  if (renewalPromise) return renewalPromise
  renewalPromise = (async () => {
    const userManager = getUserManager()
    const user = await userManager.signinSilent()
    if (!user?.access_token) {
      throw new Error('signinSilent returned no access token')
    }
    tokenRef.current = user.access_token
    return user.access_token
  })()
  try {
    return await renewalPromise
  } finally {
    renewalPromise = null
  }
}

// createAuthInterceptor returns a ConnectRPC interceptor that:
//   1. Attaches the current Bearer token to every outgoing request.
//   2. On a 401/Unauthenticated response, performs a single serialized silent
//      token renewal via oidc-client-ts and retries the request once.
//   3. Coalesces concurrent 401s so only one signinSilent() call is made.
//   4. Does not retry a second 401 to prevent infinite loops.
export function createAuthInterceptor(): Interceptor {
  return (next) => async (req) => {
    // Attach current token.
    if (tokenRef.current) {
      req.header.set('Authorization', `Bearer ${tokenRef.current}`)
    }

    try {
      return await next(req)
    } catch (err) {
      // On Unauthenticated, renew the token and retry once.
      // The retry is outside the try-catch so a second 401 propagates
      // directly without looping back here.
      if (err instanceof ConnectError && err.code === Code.Unauthenticated) {
        // Renew token (coalesced with other concurrent 401s).
        const freshToken = await renewToken()
        // Update the request header with the fresh token for the retry.
        req.header.set('Authorization', `Bearer ${freshToken}`)
        return await next(req)
      }
      throw err
    }
  }
}

export const transport = createConnectTransport({
  baseUrl: '/',
  interceptors: [createAuthInterceptor()],
})

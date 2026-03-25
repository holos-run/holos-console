import { createConnectTransport } from '@connectrpc/connect-web'
import type { Interceptor } from '@connectrpc/connect'

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

const authInterceptor: Interceptor = (next) => async (req) => {
  if (tokenRef.current) {
    req.header.set('Authorization', `Bearer ${tokenRef.current}`)
  }
  return next(req)
}

export const transport = createConnectTransport({
  baseUrl: '/',
  interceptors: [authInterceptor],
})

import { createConnectTransport } from '@connectrpc/connect-web'
import type { Interceptor } from '@connectrpc/connect'

// Shared mutable ref for the current access token.
// Set by AuthProvider whenever the user changes.
export const tokenRef: { current: string | null } = { current: null }

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

import { createClient, type Interceptor } from '@connectrpc/connect'
import { createConnectTransport } from '@connectrpc/connect-web'
import { VersionService } from './gen/holos/console/v1/version_pb.js'
import { SecretsService } from './gen/holos/console/v1/secrets_pb.js'

// Shared ref for the current access token. Set by AuthProvider when the user
// changes so the transport interceptor can inject Authorization headers.
export const tokenRef: { current: string | null } = { current: null }

// Interceptor that injects the Bearer token on every outgoing request.
export const authInterceptor: Interceptor = (next) => async (req) => {
  if (tokenRef.current) {
    req.header.set('Authorization', `Bearer ${tokenRef.current}`)
  }
  return next(req)
}

// In development with Vite proxy, use relative path (proxied to backend).
// In production, the frontend is served by the Go backend at the same origin.
export const transport = createConnectTransport({
  baseUrl: '/',
  interceptors: [authInterceptor],
})

export const versionClient = createClient(VersionService, transport)
export const secretsClient = createClient(SecretsService, transport)

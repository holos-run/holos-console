import { createConnectTransport } from '@connectrpc/connect-web'
import { tokenRef, authInterceptor } from './client'

describe('authInterceptor', () => {
  it('sets Authorization header when tokenRef has a value', async () => {
    tokenRef.current = 'test-token-123'

    const transport = createConnectTransport({
      baseUrl: 'https://example.com',
      interceptors: [authInterceptor],
    })

    // Use the transport's internal unary method to verify headers.
    // We create a minimal request and check the interceptor modifies it.
    const headers = new Headers()
    const req = {
      header: headers,
      url: 'https://example.com/test',
      method: 'POST' as const,
      stream: false as const,
      service: { typeName: 'test.Service' },
      init: {},
      signal: AbortSignal.abort(),
      contextValues: undefined,
    }

    // Call the interceptor directly
    let capturedHeaders: Headers | undefined
    const next = async (r: typeof req) => {
      capturedHeaders = r.header
      return { header: new Headers(), trailer: new Headers(), message: {} }
    }
    const interceptorFn = authInterceptor(next as any)
    try {
      await interceptorFn(req as any)
    } catch {
      // Signal is aborted, ignore
    }

    expect(capturedHeaders?.get('Authorization')).toBe('Bearer test-token-123')
  })

  it('does not set Authorization header when tokenRef is null', async () => {
    tokenRef.current = null

    let capturedHeaders: Headers | undefined
    const next = async (r: any) => {
      capturedHeaders = r.header
      return { header: new Headers(), trailer: new Headers(), message: {} }
    }
    const interceptorFn = authInterceptor(next as any)
    try {
      await interceptorFn({
        header: new Headers(),
        url: 'https://example.com/test',
        method: 'POST',
        stream: false,
        service: { typeName: 'test.Service' },
        init: {},
        signal: AbortSignal.abort(),
        contextValues: undefined,
      } as any)
    } catch {
      // Signal is aborted, ignore
    }

    expect(capturedHeaders?.get('Authorization')).toBeNull()
  })
})

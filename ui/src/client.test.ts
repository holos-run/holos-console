import { tokenRef, authInterceptor } from './client'

describe('authInterceptor', () => {
  afterEach(() => {
    tokenRef.current = null
  })

  it('sets Authorization header when tokenRef has a value', async () => {
    tokenRef.current = 'test-token-123'

    let capturedHeaders: Headers | undefined
    const next = async (r: any) => {
      capturedHeaders = r.header
      return { header: new Headers(), trailer: new Headers(), message: {} }
    }
    const interceptorFn = authInterceptor(next as any)
    await interceptorFn({ header: new Headers() } as any)

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
    await interceptorFn({ header: new Headers() } as any)

    expect(capturedHeaders?.get('Authorization')).toBeNull()
  })
})

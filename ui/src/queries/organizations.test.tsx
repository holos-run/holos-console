import { renderHook, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { TransportProvider } from '@connectrpc/connect-query'
import { createRouterTransport } from '@connectrpc/connect'
import { create } from '@bufbuild/protobuf'
import { useListOrganizations } from './organizations'
import { tokenRef, authInterceptor } from '../client'
import {
  ListOrganizationsResponseSchema,
  OrganizationSchema,
  OrganizationService,
} from '../gen/holos/console/v1/organizations_pb.js'
import { Role } from '../gen/holos/console/v1/rbac_pb.js'
import type { ReactNode } from 'react'

function createWrapper(transport: ReturnType<typeof createRouterTransport>) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  })
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <TransportProvider transport={transport}>
        <QueryClientProvider client={queryClient}>
          {children}
        </QueryClientProvider>
      </TransportProvider>
    )
  }
}

describe('useListOrganizations', () => {
  afterEach(() => {
    tokenRef.current = null
  })

  it('returns organization data from the RPC', async () => {
    const transport = createRouterTransport(({ service }) => {
      service(OrganizationService, {
        listOrganizations: () =>
          create(ListOrganizationsResponseSchema, {
            organizations: [
              create(OrganizationSchema, {
                name: 'acme',
                displayName: 'ACME Corp',
                description: 'A test organization',
                userRole: Role.OWNER,
              }),
            ],
          }),
      })
    })

    const { result } = renderHook(() => useListOrganizations(), {
      wrapper: createWrapper(transport),
    })

    await waitFor(() => {
      expect(result.current.isSuccess).toBe(true)
    })

    expect(result.current.data?.organizations).toHaveLength(1)
    expect(result.current.data?.organizations[0].name).toBe('acme')
    expect(result.current.data?.organizations[0].displayName).toBe('ACME Corp')
  })

  it('includes Authorization header via auth interceptor', async () => {
    let capturedAuth: string | null = null

    tokenRef.current = 'test-token-abc'

    const transport = createRouterTransport(({ service }) => {
      service(OrganizationService, {
        listOrganizations: (_req, context) => {
          capturedAuth = context.requestHeader.get('Authorization')
          return create(ListOrganizationsResponseSchema, { organizations: [] })
        },
      })
    }, { transport: { interceptors: [authInterceptor] } })

    const { result } = renderHook(() => useListOrganizations(), {
      wrapper: createWrapper(transport),
    })

    await waitFor(() => {
      expect(result.current.isSuccess).toBe(true)
    })

    expect(capturedAuth).toBe('Bearer test-token-abc')
  })
})

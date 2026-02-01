import { renderHook, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { TransportProvider } from '@connectrpc/connect-query'
import { createRouterTransport } from '@connectrpc/connect'
import { create } from '@bufbuild/protobuf'
import { useListProjects } from './projects'
import { tokenRef, authInterceptor } from '../client'
import {
  ListProjectsResponseSchema,
  ProjectSchema,
  ProjectService,
} from '../gen/holos/console/v1/projects_pb.js'
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

describe('useListProjects', () => {
  afterEach(() => {
    tokenRef.current = null
  })

  it('returns project data from the RPC', async () => {
    const transport = createRouterTransport(({ service }) => {
      service(ProjectService, {
        listProjects: () =>
          create(ListProjectsResponseSchema, {
            projects: [
              create(ProjectSchema, {
                name: 'my-project',
                displayName: 'My Project',
                description: 'A test project',
                userRole: Role.OWNER,
              }),
            ],
          }),
      })
    })

    const { result } = renderHook(() => useListProjects(''), {
      wrapper: createWrapper(transport),
    })

    await waitFor(() => {
      expect(result.current.isSuccess).toBe(true)
    })

    expect(result.current.data?.projects).toHaveLength(1)
    expect(result.current.data?.projects[0].name).toBe('my-project')
    expect(result.current.data?.projects[0].displayName).toBe('My Project')
  })

  it('includes Authorization header via auth interceptor', async () => {
    let capturedAuth: string | null = null

    tokenRef.current = 'test-token-abc'

    const transport = createRouterTransport(({ service }) => {
      service(ProjectService, {
        listProjects: (_req, context) => {
          capturedAuth = context.requestHeader.get('Authorization')
          return create(ListProjectsResponseSchema, { projects: [] })
        },
      })
    }, { transport: { interceptors: [authInterceptor] } })

    const { result } = renderHook(() => useListProjects(''), {
      wrapper: createWrapper(transport),
    })

    await waitFor(() => {
      expect(result.current.isSuccess).toBe(true)
    })

    expect(capturedAuth).toBe('Bearer test-token-abc')
  })
})

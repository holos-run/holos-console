import { render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { TransportProvider } from '@connectrpc/connect-query'
import { createRouterTransport } from '@connectrpc/connect'
import { create } from '@bufbuild/protobuf'
import { OrgProvider, useOrg } from './OrgProvider'
import { AuthContext, type AuthContextValue } from './auth'
import { vi } from 'vitest'
import { Role } from './gen/holos/console/v1/rbac_pb'
import {
  ListOrganizationsResponseSchema,
  OrganizationSchema,
  OrganizationService,
} from './gen/holos/console/v1/organizations_pb.js'

function createAuthContext(overrides: Partial<AuthContextValue> = {}): AuthContextValue {
  return {
    user: null,
    bffUser: null,
    isBFF: false,
    isLoading: false,
    error: null,
    isAuthenticated: false,
    login: vi.fn(),
    logout: vi.fn(),
    getAccessToken: vi.fn(() => 'mock-access-token'),
    refreshTokens: vi.fn(),
    lastRefreshStatus: 'idle',
    lastRefreshTime: null,
    lastRefreshError: null,
    ...overrides,
  }
}

function OrgConsumer() {
  const { organizations, isLoading } = useOrg()
  if (isLoading) return <div>Loading...</div>
  return (
    <div>
      {organizations.map((org) => (
        <div key={org.name}>{org.displayName || org.name}</div>
      ))}
      {organizations.length === 0 && <div>No orgs</div>}
    </div>
  )
}

describe('OrgProvider', () => {
  it('provides organization data from TanStack Query', async () => {
    const authValue = createAuthContext({ isAuthenticated: true })

    const transport = createRouterTransport(({ service }) => {
      service(OrganizationService, {
        listOrganizations: () =>
          create(ListOrganizationsResponseSchema, {
            organizations: [
              create(OrganizationSchema, {
                name: 'acme',
                displayName: 'ACME Corp',
                description: '',
                userRole: Role.OWNER,
              }),
              create(OrganizationSchema, {
                name: 'globex',
                displayName: 'Globex',
                description: '',
                userRole: Role.VIEWER,
              }),
            ],
          }),
      })
    })

    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })

    render(
      <TransportProvider transport={transport}>
        <QueryClientProvider client={queryClient}>
          <AuthContext.Provider value={authValue}>
            <OrgProvider>
              <OrgConsumer />
            </OrgProvider>
          </AuthContext.Provider>
        </QueryClientProvider>
      </TransportProvider>,
    )

    await waitFor(() => {
      expect(screen.getByText('ACME Corp')).toBeInTheDocument()
      expect(screen.getByText('Globex')).toBeInTheDocument()
    })
  })

  it('shows empty state when not authenticated', async () => {
    const authValue = createAuthContext({ isAuthenticated: false })

    const transport = createRouterTransport(({ service }) => {
      service(OrganizationService, {
        listOrganizations: () =>
          create(ListOrganizationsResponseSchema, { organizations: [] }),
      })
    })

    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })

    render(
      <TransportProvider transport={transport}>
        <QueryClientProvider client={queryClient}>
          <AuthContext.Provider value={authValue}>
            <OrgProvider>
              <OrgConsumer />
            </OrgProvider>
          </AuthContext.Provider>
        </QueryClientProvider>
      </TransportProvider>,
    )

    await waitFor(() => {
      expect(screen.getByText('No orgs')).toBeInTheDocument()
    })
  })
})

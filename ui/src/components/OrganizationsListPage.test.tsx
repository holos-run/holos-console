import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { TransportProvider } from '@connectrpc/connect-query'
import { createRouterTransport } from '@connectrpc/connect'
import { create } from '@bufbuild/protobuf'
import { OrganizationsListPage } from './OrganizationsListPage'
import { AuthContext, type AuthContextValue } from '../auth'
import type { User } from 'oidc-client-ts'
import { vi } from 'vitest'
import { Role } from '../gen/holos/console/v1/rbac_pb'
import {
  CreateOrganizationResponseSchema,
  DeleteOrganizationResponseSchema,
  ListOrganizationsResponseSchema,
  OrganizationSchema,
  OrganizationService,
} from '../gen/holos/console/v1/organizations_pb.js'
import type { Transport } from '@connectrpc/connect'

function createMockUser(profile: Record<string, unknown>): User {
  return {
    profile: {
      sub: 'test-subject',
      name: 'Test User',
      email: 'test@example.com',
      email_verified: true,
      ...profile,
    },
    id_token: 'mock-id-token',
    access_token: 'mock-access-token',
    token_type: 'Bearer',
    expired: false,
  } as User
}

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

function renderOrganizationsListPage(authValue: AuthContextValue, transport?: Transport) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  })
  const t = transport ?? createRouterTransport(({ service }) => {
    service(OrganizationService, {
      listOrganizations: () => create(ListOrganizationsResponseSchema, { organizations: [] }),
      deleteOrganization: () => create(DeleteOrganizationResponseSchema),
      createOrganization: () => create(CreateOrganizationResponseSchema, { name: 'test' }),
    })
  })
  return render(
    <TransportProvider transport={t}>
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/organizations']}>
          <AuthContext.Provider value={authValue}>
            <Routes>
              <Route path="/organizations" element={<OrganizationsListPage />} />
              <Route path="/organizations/:organizationName" element={<div>Org Detail</div>} />
            </Routes>
          </AuthContext.Provider>
        </MemoryRouter>
      </QueryClientProvider>
    </TransportProvider>,
  )
}

function createListTransport(orgs: Array<{
  name: string
  displayName: string
  description: string
  userRole: Role
}>) {
  return createRouterTransport(({ service }) => {
    service(OrganizationService, {
      listOrganizations: () =>
        create(ListOrganizationsResponseSchema, {
          organizations: orgs.map((o) => create(OrganizationSchema, o)),
        }),
      deleteOrganization: () => create(DeleteOrganizationResponseSchema),
      createOrganization: (req) => create(CreateOrganizationResponseSchema, { name: req.name }),
    })
  })
}

describe('OrganizationsListPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('organization list', () => {
    it('renders organization list from query data', async () => {
      const authValue = createAuthContext({
        user: createMockUser({}),
        isAuthenticated: true,
      })

      const transport = createListTransport([
        { name: 'acme', displayName: 'ACME Corp', description: 'The ACME org', userRole: Role.OWNER },
        { name: 'globex', displayName: 'Globex', description: 'Globex Corp', userRole: Role.VIEWER },
      ])

      renderOrganizationsListPage(authValue, transport)

      await waitFor(() => {
        expect(screen.getByText('ACME Corp')).toBeInTheDocument()
        expect(screen.getByText('Globex')).toBeInTheDocument()
      })
    })

    it('shows empty state when no organizations exist', async () => {
      const authValue = createAuthContext({
        user: createMockUser({}),
        isAuthenticated: true,
      })

      renderOrganizationsListPage(authValue)

      await waitFor(() => {
        expect(screen.getByText(/no organizations available/i)).toBeInTheDocument()
      })
    })
  })

  describe('delete organization', () => {
    it('removes organization from list after delete (optimistic update)', async () => {
      const authValue = createAuthContext({
        user: createMockUser({}),
        isAuthenticated: true,
      })

      let deleted = false
      const transport = createRouterTransport(({ service }) => {
        service(OrganizationService, {
          listOrganizations: () => {
            const organizations = deleted
              ? [create(OrganizationSchema, { name: 'globex', displayName: 'Globex', description: '', userRole: Role.OWNER })]
              : [
                  create(OrganizationSchema, { name: 'acme', displayName: 'ACME Corp', description: '', userRole: Role.OWNER }),
                  create(OrganizationSchema, { name: 'globex', displayName: 'Globex', description: '', userRole: Role.OWNER }),
                ]
            return create(ListOrganizationsResponseSchema, { organizations })
          },
          deleteOrganization: () => {
            deleted = true
            return create(DeleteOrganizationResponseSchema)
          },
          createOrganization: (req) => create(CreateOrganizationResponseSchema, { name: req.name }),
        })
      })

      renderOrganizationsListPage(authValue, transport)

      await waitFor(() => {
        expect(screen.getByText('ACME Corp')).toBeInTheDocument()
        expect(screen.getByText('Globex')).toBeInTheDocument()
      })

      fireEvent.click(screen.getByLabelText(/delete acme/i))
      const dialogDeleteButton = screen.getAllByRole('button', { name: /delete/i }).find(
        (btn) => btn.closest('[role="dialog"]'),
      )
      fireEvent.click(dialogDeleteButton!)

      await waitFor(() => {
        expect(screen.queryByText('ACME Corp')).not.toBeInTheDocument()
      })
      expect(screen.getByText('Globex')).toBeInTheDocument()
    })
  })
})

import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { TransportProvider } from '@connectrpc/connect-query'
import { createRouterTransport } from '@connectrpc/connect'
import { create } from '@bufbuild/protobuf'
import { OrganizationPage } from './OrganizationPage'
import { AuthContext, type AuthContextValue } from '../auth'
import type { User } from 'oidc-client-ts'
import { vi } from 'vitest'
import { Role } from '../gen/holos/console/v1/rbac_pb'
import {
  DeleteOrganizationResponseSchema,
  GetOrganizationRawResponseSchema,
  GetOrganizationResponseSchema,
  OrganizationSchema,
  OrganizationService,
  UpdateOrganizationResponseSchema,
  UpdateOrganizationSharingResponseSchema,
} from '../gen/holos/console/v1/organizations_pb.js'
import type { Transport } from '@connectrpc/connect'

// Mock OrgProvider
vi.mock('../OrgProvider', () => ({
  useOrg: () => ({ selectedOrg: null, setSelectedOrg: vi.fn() }),
}))

// Mock the client module (only used for getOrganizationRaw which is not a query hook)
vi.mock('../client', () => ({
  organizationsClient: {
    getOrganizationRaw: vi.fn(),
  },
  tokenRef: { current: null },
  authInterceptor: (next: unknown) => next,
  transport: {},
}))

import { organizationsClient } from '../client'
const mockGetOrganizationRaw = vi.mocked(
  (organizationsClient as unknown as { getOrganizationRaw: ReturnType<typeof vi.fn> }).getOrganizationRaw,
)

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

function createOrgTransport(org: {
  name: string
  displayName: string
  description: string
  userRole: Role
  userGrants?: Array<{ principal: string; role: Role }>
  groupGrants?: Array<{ principal: string; role: Role }>
}, rawJson?: string) {
  return createRouterTransport(({ service }) => {
    service(OrganizationService, {
      listOrganizations: () => create(GetOrganizationResponseSchema, {}),
      getOrganization: () =>
        create(GetOrganizationResponseSchema, {
          organization: create(OrganizationSchema, {
            ...org,
            userGrants: org.userGrants ?? [],
            groupGrants: org.groupGrants ?? [],
          }),
        }),
      deleteOrganization: () => create(DeleteOrganizationResponseSchema),
      updateOrganization: () => create(UpdateOrganizationResponseSchema),
      updateOrganizationSharing: () => create(UpdateOrganizationSharingResponseSchema),
      getOrganizationRaw: () => create(GetOrganizationRawResponseSchema, { raw: rawJson ?? '' }),
    })
  })
}

function renderOrganizationPage(authValue: AuthContextValue, orgName = 'test-org', transport?: Transport) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  })
  const t = transport ?? createOrgTransport({
    name: orgName,
    displayName: '',
    description: '',
    userRole: Role.VIEWER,
  })
  return render(
    <TransportProvider transport={t}>
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={[`/organizations/${orgName}`]}>
          <AuthContext.Provider value={authValue}>
            <Routes>
              <Route path="/organizations/:organizationName" element={<OrganizationPage />} />
              <Route path="/organizations" element={<div>Organizations List</div>} />
              <Route path="/projects" element={<div>Projects List</div>} />
            </Routes>
          </AuthContext.Provider>
        </MemoryRouter>
      </QueryClientProvider>
    </TransportProvider>,
  )
}

describe('OrganizationPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('raw view toggle', () => {
    it('shows Editor/Raw toggle buttons', async () => {
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      const transport = createOrgTransport({
        name: 'acme',
        displayName: 'ACME Corp',
        description: 'Test org',
        userGrants: [{ principal: 'test@example.com', role: Role.OWNER }],
        groupGrants: [],
        userRole: Role.OWNER,
      })

      renderOrganizationPage(authValue, 'acme', transport)

      await waitFor(() => {
        expect(screen.getByText('ACME Corp')).toBeInTheDocument()
      })

      expect(screen.getByRole('button', { name: /editor/i })).toBeInTheDocument()
      expect(screen.getByRole('button', { name: /raw/i })).toBeInTheDocument()
    })

    it('fetches and displays raw JSON when Raw toggle is clicked', async () => {
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      const rawJson = JSON.stringify({
        apiVersion: 'v1',
        kind: 'Namespace',
        metadata: { name: 'holos-o-acme' },
      })
      mockGetOrganizationRaw.mockResolvedValue({ raw: rawJson })

      const transport = createOrgTransport({
        name: 'acme',
        displayName: 'ACME Corp',
        description: 'Test org',
        userGrants: [],
        groupGrants: [],
        userRole: Role.VIEWER,
      })

      renderOrganizationPage(authValue, 'acme', transport)

      await waitFor(() => {
        expect(screen.getByText('ACME Corp')).toBeInTheDocument()
      })

      fireEvent.click(screen.getByRole('button', { name: /raw/i }))

      await waitFor(() => {
        const pre = screen.getByRole('code')
        expect(pre).toBeInTheDocument()
        const parsed = JSON.parse(pre.textContent || '')
        expect(parsed.kind).toBe('Namespace')
      })
    })

    it('switches back to editor view when Editor toggle is clicked', async () => {
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      const rawJson = JSON.stringify({
        apiVersion: 'v1',
        kind: 'Namespace',
        metadata: { name: 'holos-o-acme' },
      })
      mockGetOrganizationRaw.mockResolvedValue({ raw: rawJson })

      const transport = createOrgTransport({
        name: 'acme',
        displayName: 'ACME Corp',
        description: 'Test org',
        userGrants: [],
        groupGrants: [],
        userRole: Role.EDITOR,
      })

      renderOrganizationPage(authValue, 'acme', transport)

      await waitFor(() => {
        expect(screen.getByText('ACME Corp')).toBeInTheDocument()
      })

      // Switch to raw
      fireEvent.click(screen.getByRole('button', { name: /raw/i }))

      await waitFor(() => {
        expect(screen.getByRole('code')).toBeInTheDocument()
      })

      // Switch back to editor
      fireEvent.click(screen.getByRole('button', { name: /editor/i }))

      await waitFor(() => {
        // Raw view should be gone, editor content should be visible
        expect(screen.queryByRole('code')).not.toBeInTheDocument()
        expect(screen.getByText('ACME Corp')).toBeInTheDocument()
      })
    })
  })
})

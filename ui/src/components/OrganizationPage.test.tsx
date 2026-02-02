import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { OrganizationPage } from './OrganizationPage'
import { AuthContext, type AuthContextValue } from '../auth'
import type { User } from 'oidc-client-ts'
import { vi } from 'vitest'
import { Role } from '../gen/holos/console/v1/rbac_pb'

// Mock the client module
vi.mock('../client', () => ({
  organizationsClient: {
    getOrganization: vi.fn(),
    updateOrganization: vi.fn(),
    updateOrganizationSharing: vi.fn(),
    deleteOrganization: vi.fn(),
    getOrganizationRaw: vi.fn(),
  },
}))

// Mock OrgProvider
vi.mock('../OrgProvider', () => ({
  useOrg: () => ({ selectedOrg: null, setSelectedOrg: vi.fn() }),
}))

import { organizationsClient } from '../client'
const mockGetOrganization = vi.mocked(organizationsClient.getOrganization)
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

function renderOrganizationPage(authValue: AuthContextValue, orgName = 'test-org') {
  return render(
    <MemoryRouter initialEntries={[`/organizations/${orgName}`]}>
      <AuthContext.Provider value={authValue}>
        <Routes>
          <Route path="/organizations/:organizationName" element={<OrganizationPage />} />
          <Route path="/organizations" element={<div>Organizations List</div>} />
          <Route path="/projects" element={<div>Projects List</div>} />
        </Routes>
      </AuthContext.Provider>
    </MemoryRouter>,
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

      mockGetOrganization.mockResolvedValue({
        organization: {
          name: 'acme',
          displayName: 'ACME Corp',
          description: 'Test org',
          userGrants: [{ principal: 'test@example.com', role: Role.OWNER }],
          groupGrants: [],
          userRole: Role.OWNER,
        },
      } as unknown as Awaited<ReturnType<typeof organizationsClient.getOrganization>>)

      renderOrganizationPage(authValue, 'acme')

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

      mockGetOrganization.mockResolvedValue({
        organization: {
          name: 'acme',
          displayName: 'ACME Corp',
          description: 'Test org',
          userGrants: [],
          groupGrants: [],
          userRole: Role.VIEWER,
        },
      } as unknown as Awaited<ReturnType<typeof organizationsClient.getOrganization>>)

      const rawJson = JSON.stringify({
        apiVersion: 'v1',
        kind: 'Namespace',
        metadata: { name: 'holos-o-acme' },
      })
      mockGetOrganizationRaw.mockResolvedValue({ raw: rawJson })

      renderOrganizationPage(authValue, 'acme')

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

      mockGetOrganization.mockResolvedValue({
        organization: {
          name: 'acme',
          displayName: 'ACME Corp',
          description: 'Test org',
          userGrants: [],
          groupGrants: [],
          userRole: Role.EDITOR,
        },
      } as unknown as Awaited<ReturnType<typeof organizationsClient.getOrganization>>)

      const rawJson = JSON.stringify({
        apiVersion: 'v1',
        kind: 'Namespace',
        metadata: { name: 'holos-o-acme' },
      })
      mockGetOrganizationRaw.mockResolvedValue({ raw: rawJson })

      renderOrganizationPage(authValue, 'acme')

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

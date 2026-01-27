import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { ProfilePage } from './ProfilePage'
import { AuthContext, type AuthContextValue } from '../auth'
import type { User } from 'oidc-client-ts'

// Helper to create a mock User with profile
function createMockUser(profile: Record<string, unknown>): User {
  return {
    profile: {
      sub: 'test-subject',
      name: 'Test User',
      email: 'test@example.com',
      email_verified: true,
      ...profile,
    },
    access_token: 'mock-access-token',
    token_type: 'Bearer',
    expired: false,
  } as User
}

// Helper to create auth context value
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
    getAccessToken: vi.fn(),
    refreshTokens: vi.fn(),
    lastRefreshStatus: 'idle',
    lastRefreshTime: null,
    lastRefreshError: null,
    ...overrides,
  }
}

function renderProfilePage(authValue: AuthContextValue) {
  return render(
    <MemoryRouter>
      <AuthContext.Provider value={authValue}>
        <ProfilePage />
      </AuthContext.Provider>
    </MemoryRouter>,
  )
}

describe('ProfilePage', () => {
  describe('groups display', () => {
    it('displays groups when present', () => {
      const mockUser = createMockUser({
        groups: ['admin', 'developers'],
      })
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      renderProfilePage(authValue)

      expect(screen.getByText('Groups')).toBeInTheDocument()
      expect(screen.getByText('admin, developers')).toBeInTheDocument()
    })

    it('displays "None" when groups array is empty', () => {
      const mockUser = createMockUser({
        groups: [],
      })
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      renderProfilePage(authValue)

      expect(screen.getByText('Groups')).toBeInTheDocument()
      expect(screen.getByText('None')).toBeInTheDocument()
    })

    it('displays "None" when groups claim is missing', () => {
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      renderProfilePage(authValue)

      expect(screen.getByText('Groups')).toBeInTheDocument()
      expect(screen.getByText('None')).toBeInTheDocument()
    })

    it('includes groups in ID Token Claims accordion', async () => {
      const user = userEvent.setup()
      const mockUser = createMockUser({
        groups: ['admin', 'developers'],
      })
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      renderProfilePage(authValue)

      // Expand the accordion
      const accordionButton = screen.getByRole('button', { name: /ID Token Claims/i })
      await user.click(accordionButton)

      // Check that groups appear in the JSON output
      const jsonOutput = screen.getByText(/"groups"/)
      expect(jsonOutput).toBeInTheDocument()
    })
  })
})

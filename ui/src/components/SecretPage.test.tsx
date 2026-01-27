import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { SecretPage } from './SecretPage'
import { AuthContext, type AuthContextValue } from '../auth'
import type { User } from 'oidc-client-ts'
import { vi } from 'vitest'

// Mock the client module
vi.mock('../client', () => ({
  secretsClient: {
    getSecret: vi.fn(),
  },
}))

import { secretsClient } from '../client'
const mockGetSecret = vi.mocked(secretsClient.getSecret)

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
    id_token: 'mock-id-token',
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
    getAccessToken: vi.fn(() => 'mock-access-token'),
    refreshTokens: vi.fn(),
    lastRefreshStatus: 'idle',
    lastRefreshTime: null,
    lastRefreshError: null,
    ...overrides,
  }
}

function renderSecretPage(authValue: AuthContextValue, secretName: string = 'test-secret') {
  return render(
    <MemoryRouter initialEntries={[`/secrets/${secretName}`]}>
      <AuthContext.Provider value={authValue}>
        <Routes>
          <Route path="/secrets/:name" element={<SecretPage />} />
        </Routes>
      </AuthContext.Provider>
    </MemoryRouter>,
  )
}

describe('SecretPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('loading state', () => {
    it('displays loading state while fetching', async () => {
      // Given: authenticated user and pending API call
      const mockUser = createMockUser({ groups: ['owner'] })
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      // API call never resolves during this test
      mockGetSecret.mockImplementation(() => new Promise(() => {}))

      renderSecretPage(authValue, 'dummy-secret')

      // Then: loading indicator is shown
      expect(screen.getByText(/loading/i)).toBeInTheDocument()
    })
  })

  describe('successful fetch', () => {
    it('displays secret data in env format when successful', async () => {
      // Given: authenticated user and successful API response
      const mockUser = createMockUser({ groups: ['owner'] })
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      // Mock successful response with secret data
      // Cast to unknown to satisfy protobuf Message type requirements in mocks
      mockGetSecret.mockResolvedValue({
        data: {
          username: new TextEncoder().encode('admin'),
          password: new TextEncoder().encode('secret123'),
          'api-key': new TextEncoder().encode('key-12345'),
        },
      } as unknown as Awaited<ReturnType<typeof secretsClient.getSecret>>)

      renderSecretPage(authValue, 'my-secret')

      // Then: secret data is displayed in env file format
      await waitFor(() => {
        expect(screen.getByRole('textbox')).toBeInTheDocument()
      })

      const textbox = screen.getByRole('textbox') as HTMLTextAreaElement
      expect(textbox.value).toContain('username=admin')
      expect(textbox.value).toContain('password=secret123')
      expect(textbox.value).toContain('api-key=key-12345')
    })

    it('handles empty secret data', async () => {
      // Given: authenticated user and secret with no data
      const mockUser = createMockUser({ groups: ['owner'] })
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      mockGetSecret.mockResolvedValue({
        data: {},
      } as unknown as Awaited<ReturnType<typeof secretsClient.getSecret>>)

      renderSecretPage(authValue, 'empty-secret')

      // Then: displays empty state message or empty textbox
      await waitFor(() => {
        expect(screen.getByRole('textbox')).toBeInTheDocument()
      })

      const textbox = screen.getByRole('textbox')
      expect(textbox).toHaveValue('')
    })
  })

  describe('error handling', () => {
    it('displays error message for NotFound', async () => {
      // Given: authenticated user and NotFound error
      const mockUser = createMockUser({ groups: ['owner'] })
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      // Mock NotFound error (ConnectRPC error with code)
      const notFoundError = new Error('secret not found')
      ;(notFoundError as Error & { code: string }).code = 'not_found'
      mockGetSecret.mockRejectedValue(notFoundError)

      renderSecretPage(authValue, 'missing-secret')

      // Then: error message is displayed
      await waitFor(() => {
        expect(screen.getByText(/not found/i)).toBeInTheDocument()
      })
    })

    it('displays error message for PermissionDenied', async () => {
      // Given: authenticated user and PermissionDenied error
      const mockUser = createMockUser({ groups: ['other-group'] })
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      // Mock PermissionDenied error
      const permissionError = new Error('permission denied')
      ;(permissionError as Error & { code: string }).code = 'permission_denied'
      mockGetSecret.mockRejectedValue(permissionError)

      renderSecretPage(authValue, 'restricted-secret')

      // Then: permission denied error is displayed
      await waitFor(() => {
        expect(screen.getByText(/permission denied|access denied|not authorized/i)).toBeInTheDocument()
      })
    })
  })

  describe('authentication', () => {
    it('redirects to login when not authenticated', async () => {
      // Given: unauthenticated user
      const loginMock = vi.fn()
      const authValue = createAuthContext({
        isAuthenticated: false,
        login: loginMock,
      })

      renderSecretPage(authValue, 'dummy-secret')

      // Then: login is called with return path
      await waitFor(() => {
        expect(loginMock).toHaveBeenCalledWith('/secrets/dummy-secret')
      })
    })

    it('shows loading while checking auth', () => {
      // Given: auth is still loading
      const authValue = createAuthContext({
        isLoading: true,
      })

      renderSecretPage(authValue, 'dummy-secret')

      // Then: loading indicator is shown
      expect(screen.getByText(/loading/i)).toBeInTheDocument()
    })
  })

  describe('API call', () => {
    it('passes secret name from URL to API', async () => {
      // Given: authenticated user
      const mockUser = createMockUser({ groups: ['owner'] })
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
        getAccessToken: vi.fn(() => 'test-token'),
      })

      mockGetSecret.mockResolvedValue({ data: {} } as unknown as Awaited<ReturnType<typeof secretsClient.getSecret>>)

      renderSecretPage(authValue, 'specific-secret-name')

      // Then: API is called with correct secret name
      await waitFor(() => {
        expect(mockGetSecret).toHaveBeenCalledWith(
          { name: 'specific-secret-name' },
          expect.objectContaining({
            headers: expect.any(Object),
          }),
        )
      })
    })

    it('includes auth header in API call', async () => {
      // Given: authenticated user with token
      const mockUser = createMockUser({ groups: ['owner'] })
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
        getAccessToken: vi.fn(() => 'my-test-token'),
      })

      mockGetSecret.mockResolvedValue({ data: {} } as unknown as Awaited<ReturnType<typeof secretsClient.getSecret>>)

      renderSecretPage(authValue, 'test-secret')

      // Then: API is called with Authorization header
      await waitFor(() => {
        expect(mockGetSecret).toHaveBeenCalledWith(
          expect.any(Object),
          expect.objectContaining({
            headers: expect.objectContaining({
              Authorization: 'Bearer my-test-token',
            }),
          }),
        )
      })
    })
  })
})

import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { SecretPage } from './SecretPage'
import { AuthContext, type AuthContextValue } from '../auth'
import type { User } from 'oidc-client-ts'
import { vi } from 'vitest'

// Mock the client module
vi.mock('../client', () => ({
  secretsClient: {
    getSecret: vi.fn(),
    getSecretRaw: vi.fn(),
    updateSecret: vi.fn(),
    deleteSecret: vi.fn(),
    listSecrets: vi.fn(),
    updateSharing: vi.fn(),
  },
}))

import { secretsClient } from '../client'
const mockGetSecret = vi.mocked(secretsClient.getSecret)
const mockGetSecretRaw = vi.mocked(secretsClient.getSecretRaw)
const mockUpdateSecret = vi.mocked(secretsClient.updateSecret)
const mockDeleteSecret = vi.mocked(secretsClient.deleteSecret)
const mockListSecrets = vi.mocked(secretsClient.listSecrets)
const mockUpdateSharing = vi.mocked(secretsClient.updateSharing)

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

/**
 * Override window.matchMedia so that queries matching the given pattern
 * return matches=true while all others return matches=false.
 */
function mockMatchMedia(matchPattern: RegExp): () => void {
  const original = window.matchMedia
  window.matchMedia = (query: string) => ({
    matches: matchPattern.test(query),
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  })
  return () => {
    window.matchMedia = original
  }
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
    it('displays secret data as filename and content fields', async () => {
      // Given: authenticated user and successful API response
      const mockUser = createMockUser({ groups: ['owner'] })
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      // Mock successful response with secret data
      mockGetSecret.mockResolvedValue({
        data: {
          username: new TextEncoder().encode('admin'),
          password: new TextEncoder().encode('secret123'),
        },
      } as unknown as Awaited<ReturnType<typeof secretsClient.getSecret>>)

      renderSecretPage(authValue, 'my-secret')

      // Then: secret data is displayed as filename + content entries
      await waitFor(() => {
        expect(screen.getAllByPlaceholderText('key').length).toBeGreaterThan(0)
      })

      const filenames = screen.getAllByPlaceholderText('key').map((el) => (el as HTMLInputElement).value)
      expect(filenames).toContain('username')
      expect(filenames).toContain('password')

      const contents = screen.getAllByPlaceholderText('value').map((el) => (el as HTMLTextAreaElement).value)
      expect(contents).toContain('admin')
      expect(contents).toContain('secret123')
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

      // Then: displays Add File button and no entries
      await waitFor(() => {
        expect(screen.getByRole('button', { name: /add key/i })).toBeInTheDocument()
      })

      expect(screen.queryAllByPlaceholderText('filename')).toHaveLength(0)
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

  describe('save functionality', () => {
    it('shows Save button when secret is loaded', async () => {
      const mockUser = createMockUser({ groups: ['editor'] })
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      mockGetSecret.mockResolvedValue({
        data: {
          key: new TextEncoder().encode('value'),
        },
      } as unknown as Awaited<ReturnType<typeof secretsClient.getSecret>>)

      renderSecretPage(authValue, 'my-secret')

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /save/i })).toBeInTheDocument()
      })
    })

    it('disables Save button when content is unchanged', async () => {
      const mockUser = createMockUser({ groups: ['editor'] })
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      mockGetSecret.mockResolvedValue({
        data: {
          key: new TextEncoder().encode('value'),
        },
      } as unknown as Awaited<ReturnType<typeof secretsClient.getSecret>>)

      renderSecretPage(authValue, 'my-secret')

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /save/i })).toBeDisabled()
      })
    })

    it('enables Save button when content is changed', async () => {
      const mockUser = createMockUser({ groups: ['editor'] })
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      mockGetSecret.mockResolvedValue({
        data: {
          key: new TextEncoder().encode('value'),
        },
      } as unknown as Awaited<ReturnType<typeof secretsClient.getSecret>>)

      renderSecretPage(authValue, 'my-secret')

      await waitFor(() => {
        expect(screen.getByPlaceholderText('value')).toBeInTheDocument()
      })

      // Change the content
      fireEvent.change(screen.getByPlaceholderText('value'), { target: { value: 'new-value' } })

      expect(screen.getByRole('button', { name: /save/i })).toBeEnabled()
    })

    it('calls updateSecret RPC on save', async () => {
      const mockUser = createMockUser({ groups: ['editor'] })
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
        getAccessToken: vi.fn(() => 'test-token'),
      })

      mockGetSecret.mockResolvedValue({
        data: {
          key: new TextEncoder().encode('value'),
        },
      } as unknown as Awaited<ReturnType<typeof secretsClient.getSecret>>)

      mockUpdateSecret.mockResolvedValue({} as unknown as Awaited<ReturnType<typeof secretsClient.updateSecret>>)

      renderSecretPage(authValue, 'my-secret')

      await waitFor(() => {
        expect(screen.getByPlaceholderText('value')).toBeInTheDocument()
      })

      // Change and save
      fireEvent.change(screen.getByPlaceholderText('value'), { target: { value: 'new-value' } })
      fireEvent.click(screen.getByRole('button', { name: /save/i }))

      await waitFor(() => {
        expect(mockUpdateSecret).toHaveBeenCalledWith(
          expect.objectContaining({ name: 'my-secret' }),
          expect.objectContaining({
            headers: expect.objectContaining({
              Authorization: 'Bearer test-token',
            }),
          }),
        )
      })
    })

    it('shows success message after save', async () => {
      const mockUser = createMockUser({ groups: ['editor'] })
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      mockGetSecret.mockResolvedValue({
        data: { key: new TextEncoder().encode('value') },
      } as unknown as Awaited<ReturnType<typeof secretsClient.getSecret>>)

      mockUpdateSecret.mockResolvedValue({} as unknown as Awaited<ReturnType<typeof secretsClient.updateSecret>>)

      renderSecretPage(authValue, 'my-secret')

      await waitFor(() => {
        expect(screen.getByPlaceholderText('value')).toBeInTheDocument()
      })

      fireEvent.change(screen.getByPlaceholderText('value'), { target: { value: 'new-value' } })
      fireEvent.click(screen.getByRole('button', { name: /save/i }))

      await waitFor(() => {
        expect(screen.getByText(/saved successfully/i)).toBeInTheDocument()
      })
    })

    it('shows error message on save failure', async () => {
      const mockUser = createMockUser({ groups: ['editor'] })
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      mockGetSecret.mockResolvedValue({
        data: { key: new TextEncoder().encode('value') },
      } as unknown as Awaited<ReturnType<typeof secretsClient.getSecret>>)

      mockUpdateSecret.mockRejectedValue(new Error('permission denied'))

      renderSecretPage(authValue, 'my-secret')

      await waitFor(() => {
        expect(screen.getByPlaceholderText('value')).toBeInTheDocument()
      })

      fireEvent.change(screen.getByPlaceholderText('value'), { target: { value: 'new-value' } })
      fireEvent.click(screen.getByRole('button', { name: /save/i }))

      await waitFor(() => {
        expect(screen.getByText(/permission denied/i)).toBeInTheDocument()
      })
    })
  })

  describe('delete functionality', () => {
    it('shows Delete button when secret is loaded', async () => {
      const mockUser = createMockUser({ groups: ['owner'] })
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      mockGetSecret.mockResolvedValue({
        data: { key: new TextEncoder().encode('value') },
      } as unknown as Awaited<ReturnType<typeof secretsClient.getSecret>>)

      renderSecretPage(authValue, 'my-secret')

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /delete/i })).toBeInTheDocument()
      })
    })

    it('opens confirmation dialog on Delete click', async () => {
      const mockUser = createMockUser({ groups: ['owner'] })
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      mockGetSecret.mockResolvedValue({
        data: { key: new TextEncoder().encode('value') },
      } as unknown as Awaited<ReturnType<typeof secretsClient.getSecret>>)

      renderSecretPage(authValue, 'my-secret')

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /delete/i })).toBeInTheDocument()
      })

      fireEvent.click(screen.getByRole('button', { name: /^delete$/i }))

      expect(screen.getByText(/are you sure/i)).toBeInTheDocument()
    })

    it('calls deleteSecret RPC on confirm', async () => {
      const mockUser = createMockUser({ groups: ['owner'] })
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
        getAccessToken: vi.fn(() => 'test-token'),
      })

      mockGetSecret.mockResolvedValue({
        data: { key: new TextEncoder().encode('value') },
      } as unknown as Awaited<ReturnType<typeof secretsClient.getSecret>>)

      mockDeleteSecret.mockResolvedValue({} as unknown as Awaited<ReturnType<typeof secretsClient.deleteSecret>>)

      renderSecretPage(authValue, 'my-secret')

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /^delete$/i })).toBeInTheDocument()
      })

      // Click delete to open dialog
      fireEvent.click(screen.getByRole('button', { name: /^delete$/i }))

      // Confirm in dialog - the dialog has its own Delete button
      const dialogDeleteButton = screen.getAllByRole('button', { name: /delete/i }).find(
        (btn) => btn.closest('[role="dialog"]'),
      )
      expect(dialogDeleteButton).toBeDefined()
      fireEvent.click(dialogDeleteButton!)

      await waitFor(() => {
        expect(mockDeleteSecret).toHaveBeenCalledWith(
          expect.objectContaining({ name: 'my-secret' }),
          expect.objectContaining({
            headers: expect.objectContaining({
              Authorization: 'Bearer test-token',
            }),
          }),
        )
      })
    })

    it('shows error on delete failure', async () => {
      const mockUser = createMockUser({ groups: ['owner'] })
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      mockGetSecret.mockResolvedValue({
        data: { key: new TextEncoder().encode('value') },
      } as unknown as Awaited<ReturnType<typeof secretsClient.getSecret>>)

      mockDeleteSecret.mockRejectedValue(new Error('permission denied'))

      renderSecretPage(authValue, 'my-secret')

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /^delete$/i })).toBeInTheDocument()
      })

      fireEvent.click(screen.getByRole('button', { name: /^delete$/i }))

      const dialogDeleteButton = screen.getAllByRole('button', { name: /delete/i }).find(
        (btn) => btn.closest('[role="dialog"]'),
      )
      fireEvent.click(dialogDeleteButton!)

      await waitFor(() => {
        expect(screen.getByText(/permission denied/i)).toBeInTheDocument()
      })
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

  describe('sharing panel', () => {
    it('renders sharing panel with grants from metadata', async () => {
      const mockUser = createMockUser({ email: 'alice@example.com', groups: ['dev-team'] })
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      mockGetSecret.mockResolvedValue({
        data: { key: new TextEncoder().encode('value') },
      } as unknown as Awaited<ReturnType<typeof secretsClient.getSecret>>)

      mockListSecrets.mockResolvedValue({
        secrets: [
          {
            name: 'my-secret',
            accessible: true,
            userGrants: [{ principal: 'alice@example.com', role: 3 }],
            groupGrants: [{ principal: 'dev-team', role: 1 }],
          },
        ],
      } as unknown as Awaited<ReturnType<typeof secretsClient.listSecrets>>)

      renderSecretPage(authValue, 'my-secret')

      await waitFor(() => {
        expect(screen.getByText('Sharing')).toBeInTheDocument()
      })

      expect(screen.getByText('alice@example.com')).toBeInTheDocument()
      expect(screen.getByText('dev-team')).toBeInTheDocument()
    })

    it('shows edit button when user is owner', async () => {
      const mockUser = createMockUser({ email: 'alice@example.com', groups: [] })
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      mockGetSecret.mockResolvedValue({
        data: { key: new TextEncoder().encode('value') },
      } as unknown as Awaited<ReturnType<typeof secretsClient.getSecret>>)

      mockListSecrets.mockResolvedValue({
        secrets: [
          {
            name: 'my-secret',
            accessible: true,
            userGrants: [{ principal: 'alice@example.com', role: 3 }],
            groupGrants: [],
          },
        ],
      } as unknown as Awaited<ReturnType<typeof secretsClient.listSecrets>>)

      renderSecretPage(authValue, 'my-secret')

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /^edit$/i })).toBeInTheDocument()
      })
    })

    it('calls updateSharing on save', async () => {
      const mockUser = createMockUser({ email: 'alice@example.com', groups: [] })
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
        getAccessToken: vi.fn(() => 'test-token'),
      })

      mockGetSecret.mockResolvedValue({
        data: { key: new TextEncoder().encode('value') },
      } as unknown as Awaited<ReturnType<typeof secretsClient.getSecret>>)

      mockListSecrets.mockResolvedValue({
        secrets: [
          {
            name: 'my-secret',
            accessible: true,
            userGrants: [{ principal: 'alice@example.com', role: 3 }],
            groupGrants: [],
          },
        ],
      } as unknown as Awaited<ReturnType<typeof secretsClient.listSecrets>>)

      mockUpdateSharing.mockResolvedValue({
        metadata: {
          name: 'my-secret',
          accessible: true,
          userGrants: [{ principal: 'alice@example.com', role: 3 }],
          groupGrants: [],
        },
      } as unknown as Awaited<ReturnType<typeof secretsClient.updateSharing>>)

      renderSecretPage(authValue, 'my-secret')

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /^edit$/i })).toBeInTheDocument()
      })

      fireEvent.click(screen.getByRole('button', { name: /^edit$/i }))

      // Find the sharing panel's Save button (the data Save is disabled since content is unchanged)
      const saveButtons = screen.getAllByRole('button', { name: /^save$/i })
      const enabledSave = saveButtons.find((btn) => !btn.hasAttribute('disabled'))
      fireEvent.click(enabledSave!)

      await waitFor(() => {
        expect(mockUpdateSharing).toHaveBeenCalledWith(
          expect.objectContaining({ name: 'my-secret' }),
          expect.objectContaining({
            headers: expect.objectContaining({
              Authorization: 'Bearer test-token',
            }),
          }),
        )
      })
    })
  })

  describe('description and URL fields', () => {
    it('displays description and URL from metadata', async () => {
      const mockUser = createMockUser({ email: 'alice@example.com', groups: [] })
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      mockGetSecret.mockResolvedValue({
        data: { key: new TextEncoder().encode('value') },
      } as unknown as Awaited<ReturnType<typeof secretsClient.getSecret>>)

      mockListSecrets.mockResolvedValue({
        secrets: [
          {
            name: 'my-secret',
            accessible: true,
            userGrants: [{ principal: 'alice@example.com', role: 3 }],
            groupGrants: [],
            description: 'Database credentials',
            url: 'https://db.example.com',
          },
        ],
      } as unknown as Awaited<ReturnType<typeof secretsClient.listSecrets>>)

      renderSecretPage(authValue, 'my-secret')

      await waitFor(() => {
        const descField = screen.getByLabelText('Description') as HTMLInputElement
        expect(descField.value).toBe('Database credentials')
      })

      const urlField = screen.getByLabelText('URL') as HTMLInputElement
      expect(urlField.value).toBe('https://db.example.com')
    })

    it('enables Save when description changes', async () => {
      const mockUser = createMockUser({ email: 'alice@example.com', groups: [] })
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      mockGetSecret.mockResolvedValue({
        data: { key: new TextEncoder().encode('value') },
      } as unknown as Awaited<ReturnType<typeof secretsClient.getSecret>>)

      mockListSecrets.mockResolvedValue({
        secrets: [
          {
            name: 'my-secret',
            accessible: true,
            userGrants: [{ principal: 'alice@example.com', role: 3 }],
            groupGrants: [],
            description: 'Old description',
          },
        ],
      } as unknown as Awaited<ReturnType<typeof secretsClient.listSecrets>>)

      renderSecretPage(authValue, 'my-secret')

      await waitFor(() => {
        const descField = screen.getByLabelText('Description') as HTMLInputElement
        expect(descField.value).toBe('Old description')
      })

      // Save should be disabled initially
      expect(screen.getByRole('button', { name: /^save$/i })).toBeDisabled()

      // Change description
      fireEvent.change(screen.getByLabelText('Description'), { target: { value: 'New description' } })

      // Save should now be enabled
      expect(screen.getByRole('button', { name: /^save$/i })).toBeEnabled()
    })

    it('includes description and URL in update request', async () => {
      const mockUser = createMockUser({ email: 'alice@example.com', groups: [] })
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
        getAccessToken: vi.fn(() => 'test-token'),
      })

      mockGetSecret.mockResolvedValue({
        data: { key: new TextEncoder().encode('value') },
      } as unknown as Awaited<ReturnType<typeof secretsClient.getSecret>>)

      mockListSecrets.mockResolvedValue({
        secrets: [
          {
            name: 'my-secret',
            accessible: true,
            userGrants: [{ principal: 'alice@example.com', role: 3 }],
            groupGrants: [],
          },
        ],
      } as unknown as Awaited<ReturnType<typeof secretsClient.listSecrets>>)

      mockUpdateSecret.mockResolvedValue({} as unknown as Awaited<ReturnType<typeof secretsClient.updateSecret>>)

      renderSecretPage(authValue, 'my-secret')

      await waitFor(() => {
        expect(screen.getByLabelText('Description')).toBeInTheDocument()
      })

      // Change description to trigger dirty state
      fireEvent.change(screen.getByLabelText('Description'), { target: { value: 'New desc' } })
      fireEvent.click(screen.getByRole('button', { name: /^save$/i }))

      await waitFor(() => {
        expect(mockUpdateSecret).toHaveBeenCalledWith(
          expect.objectContaining({
            name: 'my-secret',
            description: 'New desc',
            url: '',
          }),
          expect.any(Object),
        )
      })
    })

    it('shows Open link when URL is set', async () => {
      const mockUser = createMockUser({ email: 'alice@example.com', groups: [] })
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      mockGetSecret.mockResolvedValue({
        data: { key: new TextEncoder().encode('value') },
      } as unknown as Awaited<ReturnType<typeof secretsClient.getSecret>>)

      mockListSecrets.mockResolvedValue({
        secrets: [
          {
            name: 'my-secret',
            accessible: true,
            userGrants: [{ principal: 'alice@example.com', role: 3 }],
            groupGrants: [],
            url: 'https://example.com/service',
          },
        ],
      } as unknown as Awaited<ReturnType<typeof secretsClient.listSecrets>>)

      renderSecretPage(authValue, 'my-secret')

      await waitFor(() => {
        const openLink = screen.getByText('Open')
        expect(openLink).toBeInTheDocument()
        expect(openLink.closest('a')).toHaveAttribute('href', 'https://example.com/service')
      })
    })
  })

  describe('view mode toggle', () => {
    it('displays Editor and Raw toggle buttons', async () => {
      const mockUser = createMockUser({ groups: ['owner'] })
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      mockGetSecret.mockResolvedValue({
        data: {
          username: new TextEncoder().encode('admin'),
        },
      } as unknown as Awaited<ReturnType<typeof secretsClient.getSecret>>)

      renderSecretPage(authValue, 'my-secret')

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /^editor$/i })).toBeInTheDocument()
        expect(screen.getByRole('button', { name: /^raw$/i })).toBeInTheDocument()
      })
    })

    it('selecting Raw hides SecretDataEditor and shows SecretRawView', async () => {
      const mockUser = createMockUser({ groups: ['owner'] })
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      mockGetSecret.mockResolvedValue({
        data: {
          username: new TextEncoder().encode('admin'),
        },
      } as unknown as Awaited<ReturnType<typeof secretsClient.getSecret>>)

      const rawJson = JSON.stringify({
        apiVersion: 'v1',
        kind: 'Secret',
        metadata: { name: 'my-secret', namespace: 'default' },
        data: { username: btoa('admin') },
        type: 'Opaque',
      })
      mockGetSecretRaw.mockResolvedValue({
        raw: rawJson,
      } as unknown as Awaited<ReturnType<typeof secretsClient.getSecretRaw>>)

      renderSecretPage(authValue, 'my-secret')

      // Wait for editor to load
      await waitFor(() => {
        expect(screen.getAllByPlaceholderText('key').length).toBeGreaterThan(0)
      })

      // Click Raw tab
      fireEvent.click(screen.getByRole('button', { name: /^raw$/i }))

      // SecretDataEditor fields should be hidden, raw view should be visible
      await waitFor(() => {
        expect(screen.getByRole('code')).toBeInTheDocument()
      })
      expect(screen.queryAllByPlaceholderText('key')).toHaveLength(0)
    })

    it('selecting Editor shows the existing editor', async () => {
      const mockUser = createMockUser({ groups: ['owner'] })
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      mockGetSecret.mockResolvedValue({
        data: {
          username: new TextEncoder().encode('admin'),
        },
      } as unknown as Awaited<ReturnType<typeof secretsClient.getSecret>>)

      const rawJson = JSON.stringify({
        apiVersion: 'v1',
        kind: 'Secret',
        metadata: { name: 'my-secret', namespace: 'default' },
        data: { username: btoa('admin') },
        type: 'Opaque',
      })
      mockGetSecretRaw.mockResolvedValue({
        raw: rawJson,
      } as unknown as Awaited<ReturnType<typeof secretsClient.getSecretRaw>>)

      renderSecretPage(authValue, 'my-secret')

      await waitFor(() => {
        expect(screen.getAllByPlaceholderText('key').length).toBeGreaterThan(0)
      })

      // Switch to Raw
      fireEvent.click(screen.getByRole('button', { name: /^raw$/i }))

      await waitFor(() => {
        expect(screen.getByRole('code')).toBeInTheDocument()
      })

      // Switch back to Editor
      fireEvent.click(screen.getByRole('button', { name: /^editor$/i }))

      await waitFor(() => {
        expect(screen.getAllByPlaceholderText('key').length).toBeGreaterThan(0)
      })
      expect(screen.queryByRole('code')).not.toBeInTheDocument()
    })

    it('Save button is disabled when raw view is active', async () => {
      const mockUser = createMockUser({ groups: ['owner'] })
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      mockGetSecret.mockResolvedValue({
        data: {
          username: new TextEncoder().encode('admin'),
        },
      } as unknown as Awaited<ReturnType<typeof secretsClient.getSecret>>)

      const rawJson = JSON.stringify({
        apiVersion: 'v1',
        kind: 'Secret',
        metadata: { name: 'my-secret', namespace: 'default' },
        data: { username: btoa('admin') },
        type: 'Opaque',
      })
      mockGetSecretRaw.mockResolvedValue({
        raw: rawJson,
      } as unknown as Awaited<ReturnType<typeof secretsClient.getSecretRaw>>)

      renderSecretPage(authValue, 'my-secret')

      await waitFor(() => {
        expect(screen.getAllByPlaceholderText('key').length).toBeGreaterThan(0)
      })

      // Switch to Raw
      fireEvent.click(screen.getByRole('button', { name: /^raw$/i }))

      await waitFor(() => {
        expect(screen.getByRole('code')).toBeInTheDocument()
      })

      // Save button should be disabled in raw view
      const saveButton = screen.getByRole('button', { name: /^save$/i })
      expect(saveButton).toBeDisabled()
    })
  })

  describe('responsive dialogs', () => {
    it('renders delete dialog fullScreen on mobile', async () => {
      const cleanup = mockMatchMedia(/max-width/)
      try {
        const mockUser = createMockUser({ groups: ['owner'] })
        const authValue = createAuthContext({
          user: mockUser,
          isAuthenticated: true,
        })

        mockGetSecret.mockResolvedValue({
          data: { key: new TextEncoder().encode('value') },
        } as unknown as Awaited<ReturnType<typeof secretsClient.getSecret>>)

        renderSecretPage(authValue, 'my-secret')

        await waitFor(() => {
          expect(screen.getByRole('button', { name: /^delete$/i })).toBeInTheDocument()
        })

        fireEvent.click(screen.getByRole('button', { name: /^delete$/i }))

        const dialog = screen.getByRole('dialog')
        expect(dialog).toHaveClass('MuiDialog-paperFullScreen')
      } finally {
        cleanup()
      }
    })
  })
})

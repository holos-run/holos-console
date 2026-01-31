import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { SecretsListPage } from './SecretsListPage'
import { AuthContext, type AuthContextValue } from '../auth'
import type { User } from 'oidc-client-ts'
import { vi } from 'vitest'

// Mock the client module
vi.mock('../client', () => ({
  secretsClient: {
    listSecrets: vi.fn(),
    createSecret: vi.fn(),
    deleteSecret: vi.fn(),
  },
}))

import { secretsClient } from '../client'
const mockListSecrets = vi.mocked(secretsClient.listSecrets)
const mockCreateSecret = vi.mocked(secretsClient.createSecret)
const mockDeleteSecret = vi.mocked(secretsClient.deleteSecret)

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

function renderSecretsListPage(authValue: AuthContextValue) {
  return render(
    <MemoryRouter initialEntries={['/secrets']}>
      <AuthContext.Provider value={authValue}>
        <Routes>
          <Route path="/secrets" element={<SecretsListPage />} />
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

describe('SecretsListPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('create secret', () => {
    it('shows Create Secret button', async () => {
      const mockUser = createMockUser({ groups: ['editor'] })
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      mockListSecrets.mockResolvedValue({
        secrets: [],
      } as unknown as Awaited<ReturnType<typeof secretsClient.listSecrets>>)

      renderSecretsListPage(authValue)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /create secret/i })).toBeInTheDocument()
      })
    })

    it('opens dialog when Create Secret is clicked', async () => {
      const mockUser = createMockUser({ groups: ['editor'] })
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      mockListSecrets.mockResolvedValue({
        secrets: [],
      } as unknown as Awaited<ReturnType<typeof secretsClient.listSecrets>>)

      renderSecretsListPage(authValue)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /create secret/i })).toBeInTheDocument()
      })

      fireEvent.click(screen.getByRole('button', { name: /create secret/i }))

      expect(screen.getByText('Create Secret', { selector: 'h2' })).toBeInTheDocument()
      expect(screen.getByLabelText('Name')).toBeInTheDocument()
    })

    it('calls createSecret RPC on submit', async () => {
      const mockUser = createMockUser({ groups: ['editor'] })
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
        getAccessToken: vi.fn(() => 'test-token'),
      })

      mockListSecrets.mockResolvedValue({
        secrets: [],
      } as unknown as Awaited<ReturnType<typeof secretsClient.listSecrets>>)

      mockCreateSecret.mockResolvedValue({
        name: 'new-secret',
      } as unknown as Awaited<ReturnType<typeof secretsClient.createSecret>>)

      renderSecretsListPage(authValue)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /create secret/i })).toBeInTheDocument()
      })

      fireEvent.click(screen.getByRole('button', { name: /create secret/i }))

      // Fill in the name
      fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'new-secret' } })

      // Add a file entry via the SecretDataEditor
      fireEvent.click(screen.getByRole('button', { name: /add key/i }))
      fireEvent.change(screen.getByPlaceholderText('key'), { target: { value: '.env' } })
      fireEvent.change(screen.getByPlaceholderText('value'), { target: { value: 'KEY=value' } })

      // Submit
      fireEvent.click(screen.getByRole('button', { name: /^create$/i }))

      await waitFor(() => {
        expect(mockCreateSecret).toHaveBeenCalledWith(
          expect.objectContaining({ name: 'new-secret' }),
          expect.objectContaining({
            headers: expect.objectContaining({
              Authorization: 'Bearer test-token',
            }),
          }),
        )
      })
    })

    it('shows error on create failure', async () => {
      const mockUser = createMockUser({ groups: ['editor'] })
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      mockListSecrets.mockResolvedValue({
        secrets: [],
      } as unknown as Awaited<ReturnType<typeof secretsClient.listSecrets>>)

      mockCreateSecret.mockRejectedValue(new Error('already exists'))

      renderSecretsListPage(authValue)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /create secret/i })).toBeInTheDocument()
      })

      fireEvent.click(screen.getByRole('button', { name: /create secret/i }))
      fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'existing' } })

      // Add a file entry
      fireEvent.click(screen.getByRole('button', { name: /add key/i }))
      fireEvent.change(screen.getByPlaceholderText('key'), { target: { value: '.env' } })
      fireEvent.change(screen.getByPlaceholderText('value'), { target: { value: 'KEY=value' } })

      fireEvent.click(screen.getByRole('button', { name: /^create$/i }))

      await waitFor(() => {
        expect(screen.getByText(/already exists/i)).toBeInTheDocument()
      })
    })

    it('validates required name field', async () => {
      const mockUser = createMockUser({ groups: ['editor'] })
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      mockListSecrets.mockResolvedValue({
        secrets: [],
      } as unknown as Awaited<ReturnType<typeof secretsClient.listSecrets>>)

      renderSecretsListPage(authValue)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /create secret/i })).toBeInTheDocument()
      })

      fireEvent.click(screen.getByRole('button', { name: /create secret/i }))

      // Submit without name
      fireEvent.click(screen.getByRole('button', { name: /^create$/i }))

      await waitFor(() => {
        expect(screen.getByText(/name is required/i)).toBeInTheDocument()
      })

      // createSecret should not be called
      expect(mockCreateSecret).not.toHaveBeenCalled()
    })
  })

  describe('delete secret', () => {
    it('shows delete icon on accessible secrets', async () => {
      const mockUser = createMockUser({ groups: ['owner'] })
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      mockListSecrets.mockResolvedValue({
        secrets: [
          { name: 'my-secret', accessible: true, userGrants: [], groupGrants: [] },
        ],
      } as unknown as Awaited<ReturnType<typeof secretsClient.listSecrets>>)

      renderSecretsListPage(authValue)

      await waitFor(() => {
        expect(screen.getByLabelText(/delete my-secret/i)).toBeInTheDocument()
      })
    })

    it('opens confirmation dialog on delete click', async () => {
      const mockUser = createMockUser({ groups: ['owner'] })
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      mockListSecrets.mockResolvedValue({
        secrets: [
          { name: 'my-secret', accessible: true, userGrants: [], groupGrants: [] },
        ],
      } as unknown as Awaited<ReturnType<typeof secretsClient.listSecrets>>)

      renderSecretsListPage(authValue)

      await waitFor(() => {
        expect(screen.getByLabelText(/delete my-secret/i)).toBeInTheDocument()
      })

      fireEvent.click(screen.getByLabelText(/delete my-secret/i))

      expect(screen.getByText(/are you sure/i)).toBeInTheDocument()
    })

    it('calls deleteSecret RPC on confirm', async () => {
      const mockUser = createMockUser({ groups: ['owner'] })
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
        getAccessToken: vi.fn(() => 'test-token'),
      })

      mockListSecrets.mockResolvedValue({
        secrets: [
          { name: 'my-secret', accessible: true, userGrants: [], groupGrants: [] },
        ],
      } as unknown as Awaited<ReturnType<typeof secretsClient.listSecrets>>)

      mockDeleteSecret.mockResolvedValue({} as unknown as Awaited<ReturnType<typeof secretsClient.deleteSecret>>)

      renderSecretsListPage(authValue)

      await waitFor(() => {
        expect(screen.getByLabelText(/delete my-secret/i)).toBeInTheDocument()
      })

      fireEvent.click(screen.getByLabelText(/delete my-secret/i))

      // Find the Delete button in the dialog
      const dialogDeleteButton = screen.getAllByRole('button', { name: /delete/i }).find(
        (btn) => btn.closest('[role="dialog"]'),
      )
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
  })

  describe('create dialog sharing', () => {
    it('uses actual user email as owner grant', async () => {
      const mockUser = createMockUser({ email: 'alice@example.com', groups: ['editor'] })
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
        getAccessToken: vi.fn(() => 'test-token'),
      })

      mockListSecrets.mockResolvedValue({
        secrets: [],
      } as unknown as Awaited<ReturnType<typeof secretsClient.listSecrets>>)

      mockCreateSecret.mockResolvedValue({
        name: 'new-secret',
      } as unknown as Awaited<ReturnType<typeof secretsClient.createSecret>>)

      renderSecretsListPage(authValue)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /create secret/i })).toBeInTheDocument()
      })

      fireEvent.click(screen.getByRole('button', { name: /create secret/i }))
      fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'new-secret' } })
      fireEvent.click(screen.getByRole('button', { name: /^create$/i }))

      await waitFor(() => {
        expect(mockCreateSecret).toHaveBeenCalledWith(
          expect.objectContaining({
            name: 'new-secret',
            userGrants: expect.arrayContaining([
              expect.objectContaining({ principal: 'alice@example.com', role: 3 }),
            ]),
          }),
          expect.any(Object),
        )
      })
    })
  })

  describe('responsive dialogs', () => {
    it('renders create dialog fullScreen on mobile', async () => {
      const cleanup = mockMatchMedia(/max-width/)
      try {
        const mockUser = createMockUser({ groups: ['editor'] })
        const authValue = createAuthContext({
          user: mockUser,
          isAuthenticated: true,
        })

        mockListSecrets.mockResolvedValue({
          secrets: [],
        } as unknown as Awaited<ReturnType<typeof secretsClient.listSecrets>>)

        renderSecretsListPage(authValue)

        await waitFor(() => {
          expect(screen.getByRole('button', { name: /create secret/i })).toBeInTheDocument()
        })

        fireEvent.click(screen.getByRole('button', { name: /create secret/i }))

        // The dialog should have the fullScreen class on mobile
        const dialog = screen.getByRole('dialog')
        expect(dialog).toHaveClass('MuiDialog-paperFullScreen')
      } finally {
        cleanup()
      }
    })

    it('renders delete dialog fullScreen on mobile', async () => {
      const cleanup = mockMatchMedia(/max-width/)
      try {
        const mockUser = createMockUser({ groups: ['owner'] })
        const authValue = createAuthContext({
          user: mockUser,
          isAuthenticated: true,
        })

        mockListSecrets.mockResolvedValue({
          secrets: [
            { name: 'my-secret', accessible: true, userGrants: [], groupGrants: [] },
          ],
        } as unknown as Awaited<ReturnType<typeof secretsClient.listSecrets>>)

        renderSecretsListPage(authValue)

        await waitFor(() => {
          expect(screen.getByLabelText(/delete my-secret/i)).toBeInTheDocument()
        })

        fireEvent.click(screen.getByLabelText(/delete my-secret/i))

        const dialog = screen.getByRole('dialog')
        expect(dialog).toHaveClass('MuiDialog-paperFullScreen')
      } finally {
        cleanup()
      }
    })
  })

  describe('list sharing summary', () => {
    it('shows sharing grant counts in list items', async () => {
      const mockUser = createMockUser({ groups: ['owner'] })
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      mockListSecrets.mockResolvedValue({
        secrets: [
          {
            name: 'shared-secret',
            accessible: true,
            userGrants: [
              { principal: 'alice@example.com', role: 3 },
              { principal: 'bob@example.com', role: 1 },
            ],
            groupGrants: [{ principal: 'dev-team', role: 2 }],
          },
        ],
      } as unknown as Awaited<ReturnType<typeof secretsClient.listSecrets>>)

      renderSecretsListPage(authValue)

      await waitFor(() => {
        expect(screen.getByText(/2 users/i)).toBeInTheDocument()
        expect(screen.getByText(/1 group/i)).toBeInTheDocument()
      })
    })

    it('shows no sharing text when no grants', async () => {
      const mockUser = createMockUser({ groups: ['owner'] })
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      mockListSecrets.mockResolvedValue({
        secrets: [
          {
            name: 'private-secret',
            accessible: true,
            userGrants: [],
            groupGrants: [],
          },
        ],
      } as unknown as Awaited<ReturnType<typeof secretsClient.listSecrets>>)

      renderSecretsListPage(authValue)

      await waitFor(() => {
        expect(screen.getByText('private-secret')).toBeInTheDocument()
      })

      // No sharing summary should appear when there are no grants
      expect(screen.queryByText(/users/i)).not.toBeInTheDocument()
      expect(screen.queryByText(/groups/i)).not.toBeInTheDocument()
    })
  })
})

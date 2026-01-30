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
  },
}))

import { secretsClient } from '../client'
const mockListSecrets = vi.mocked(secretsClient.listSecrets)
const mockCreateSecret = vi.mocked(secretsClient.createSecret)

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
      expect(screen.getByLabelText(/name/i)).toBeInTheDocument()
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

      // Fill in the form
      fireEvent.change(screen.getByLabelText(/name/i), { target: { value: 'new-secret' } })
      fireEvent.change(screen.getByLabelText(/data/i), { target: { value: 'KEY=value' } })

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
      fireEvent.change(screen.getByLabelText(/name/i), { target: { value: 'existing' } })
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
})

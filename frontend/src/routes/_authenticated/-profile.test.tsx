import { render, screen } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({}),
  }
})

vi.mock('@/lib/auth', () => ({ useAuth: vi.fn() }))

import { useAuth } from '@/lib/auth'
import { ProfilePage } from './profile'

function setAuthState(overrides = {}) {
  ;(useAuth as Mock).mockReturnValue({
    isAuthenticated: true,
    isLoading: false,
    user: {
      expires_at: Math.floor(Date.now() / 1000) + 900,
      expires_in: 900,
      expired: false,
      scope: 'openid profile email',
      token_type: 'Bearer',
      profile: {
        sub: 'test-user-id',
        email: 'test@example.com',
        iss: 'https://dex.example.com',
        aud: 'holos-console',
        groups: [],
      },
    },
    refreshTokens: vi.fn(),
    lastRefreshStatus: 'idle',
    lastRefreshTime: null,
    lastRefreshError: null,
    login: vi.fn(),
    ...overrides,
  })
}

describe('ProfilePage token claims', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('displays iss claim from ID token', () => {
    setAuthState()
    render(<ProfilePage />)
    expect(screen.getByText('https://dex.example.com')).toBeInTheDocument()
  })

  it('displays aud claim as string', () => {
    setAuthState()
    render(<ProfilePage />)
    expect(screen.getByText('holos-console')).toBeInTheDocument()
  })

  it('displays aud claim as array of strings', () => {
    setAuthState({
      user: {
        expires_at: Math.floor(Date.now() / 1000) + 900,
        expires_in: 900,
        expired: false,
        scope: 'openid profile email',
        token_type: 'Bearer',
        profile: {
          sub: 'test-user-id',
          email: 'test@example.com',
          iss: 'https://dex.example.com',
          aud: ['holos-console', 'other-client'],
          groups: [],
        },
      },
    })
    render(<ProfilePage />)
    expect(screen.getByText('holos-console, other-client')).toBeInTheDocument()
  })

  it('shows a section labeled for token claims or debugging', () => {
    setAuthState()
    render(<ProfilePage />)
    expect(screen.getByText(/token claims/i)).toBeInTheDocument()
  })
})

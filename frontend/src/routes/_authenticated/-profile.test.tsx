import { render, screen, fireEvent } from '@testing-library/react'
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

vi.mock('sonner', () => ({ toast: { success: vi.fn() } }))

import { useAuth } from '@/lib/auth'
import { ProfilePage } from './profile'

function makeUser(profileOverrides = {}) {
  return {
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
      iat: 1700000000,
      exp: 1700003600,
      ...profileOverrides,
    },
  }
}

function setAuthState(overrides = {}) {
  ;(useAuth as Mock).mockReturnValue({
    isAuthenticated: true,
    isLoading: false,
    user: makeUser(),
    refreshTokens: vi.fn(),
    lastRefreshStatus: 'idle',
    lastRefreshTime: null,
    lastRefreshError: null,
    login: vi.fn(),
    ...overrides,
  })
}

describe('ProfilePage token claims — Claims view (default)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('shows the Token Claims card heading', () => {
    setAuthState()
    render(<ProfilePage />)
    expect(screen.getByText('Token Claims')).toBeInTheDocument()
  })

  it('shows Claims and Raw segmented control buttons', () => {
    setAuthState()
    render(<ProfilePage />)
    expect(screen.getByRole('button', { name: /claims/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /raw/i })).toBeInTheDocument()
  })

  it('displays iss claim label and value', () => {
    setAuthState()
    render(<ProfilePage />)
    expect(screen.getByText('Issuer (iss)')).toBeInTheDocument()
    expect(screen.getByText('https://dex.example.com')).toBeInTheDocument()
  })

  it('displays aud claim as string', () => {
    setAuthState()
    render(<ProfilePage />)
    expect(screen.getByText('Audience (aud)')).toBeInTheDocument()
    expect(screen.getByText('holos-console')).toBeInTheDocument()
  })

  it('displays aud claim as array joined with comma', () => {
    setAuthState({ user: makeUser({ aud: ['holos-console', 'other-client'] }) })
    render(<ProfilePage />)
    expect(screen.getByText('holos-console, other-client')).toBeInTheDocument()
  })

  it('displays sub, email, iat, exp, scopes, token type', () => {
    setAuthState()
    render(<ProfilePage />)
    expect(screen.getByText('Subject (sub)')).toBeInTheDocument()
    expect(screen.getByText('test-user-id')).toBeInTheDocument()
    expect(screen.getByText('Email')).toBeInTheDocument()
    expect(screen.getByText('test@example.com')).toBeInTheDocument()
    expect(screen.getByText('Issued At (iat)')).toBeInTheDocument()
    expect(screen.getByText('Expires (exp)')).toBeInTheDocument()
    expect(screen.getByText('Scopes')).toBeInTheDocument()
    expect(screen.getByText('Token Type')).toBeInTheDocument()
  })
})

describe('ProfilePage token claims — Raw view', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('switches to raw view and shows JSON', () => {
    setAuthState()
    render(<ProfilePage />)

    fireEvent.click(screen.getByRole('button', { name: /raw/i }))

    const pre = screen.getByRole('code')
    expect(pre).toBeInTheDocument()
    expect(pre.textContent).toContain('"iss"')
    expect(pre.textContent).toContain('"aud"')
    expect(pre.textContent).toContain('"sub"')
  })

  it('shows Copy to Clipboard button in raw view', () => {
    setAuthState()
    render(<ProfilePage />)

    fireEvent.click(screen.getByRole('button', { name: /raw/i }))

    expect(screen.getByRole('button', { name: /copy to clipboard/i })).toBeInTheDocument()
  })
})

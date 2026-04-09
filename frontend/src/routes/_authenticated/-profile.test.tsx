import { render, screen, fireEvent } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({}),
    Link: ({ to, children }: { to: string; children: React.ReactNode }) =>
      React.createElement('a', { href: to }, children),
  }
})

vi.mock('@/lib/auth', () => ({ useAuth: vi.fn() }))

vi.mock('sonner', () => ({ toast: { success: vi.fn() } }))

import { useAuth } from '@/lib/auth'
import { ProfilePage } from './profile'

function makeUser(
  overrides: Record<string, unknown> = {},
  profileOverrides: Record<string, unknown> = {},
) {
  return {
    expires_at: Math.floor(Date.now() / 1000) + 900,
    expires_in: 900,
    expired: false,
    scope: 'openid profile email',
    token_type: 'Bearer',
    id_token: 'id.token.value',
    access_token: 'access.token.value',
    refresh_token: 'refresh.token.value',
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
    ...overrides,
  }
}

function setAuthState(overrides: Record<string, unknown> = {}) {
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

describe('ProfilePage API Access section', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    Object.assign(navigator, { clipboard: { writeText: vi.fn().mockResolvedValue(undefined) } })
  })

  it('renders the API Access card heading', () => {
    setAuthState()
    render(<ProfilePage />)
    expect(screen.getByText('API Access')).toBeInTheDocument()
  })

  it('does not show the raw token by default (masked)', () => {
    setAuthState()
    render(<ProfilePage />)
    expect(screen.queryByText('id.token.value')).not.toBeInTheDocument()
  })

  it('reveals the id_token when Reveal is clicked', () => {
    setAuthState()
    render(<ProfilePage />)
    fireEvent.click(screen.getByRole('button', { name: /reveal/i }))
    expect(screen.getByText(/id\.token\.value/)).toBeInTheDocument()
  })

  it('masks the token again when Hide is clicked after reveal', () => {
    setAuthState()
    render(<ProfilePage />)
    fireEvent.click(screen.getByRole('button', { name: /reveal/i }))
    expect(screen.getByText(/id\.token\.value/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /hide/i }))
    expect(screen.queryByText(/id\.token\.value/)).not.toBeInTheDocument()
  })

  it('copies the set +o history / export / set -o history recipe on copy', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.assign(navigator, { clipboard: { writeText } })
    setAuthState()
    render(<ProfilePage />)
    fireEvent.click(screen.getByRole('button', { name: /copy export snippet/i }))
    expect(writeText).toHaveBeenCalledOnce()
    const copied = writeText.mock.calls[0][0] as string
    expect(copied).toContain('set +o history')
    expect(copied).toContain('export HOLOS_ID_TOKEN="id.token.value"')
    expect(copied).toContain('set -o history')
  })

  it('never shows the refresh_token even when present', () => {
    setAuthState({ user: makeUser({ refresh_token: 'refresh.token.value' }) })
    render(<ProfilePage />)
    fireEvent.click(screen.getByRole('button', { name: /reveal/i }))
    expect(screen.queryByText(/refresh\.token\.value/)).not.toBeInTheDocument()
  })

  it('never shows the access_token', () => {
    setAuthState({ user: makeUser({ access_token: 'access.token.value' }) })
    render(<ProfilePage />)
    fireEvent.click(screen.getByRole('button', { name: /reveal/i }))
    expect(screen.queryByText(/access\.token\.value/)).not.toBeInTheDocument()
  })

  it('renders a curl example using $HOLOS_ID_TOKEN', () => {
    setAuthState()
    render(<ProfilePage />)
    const curl = screen.getByText(/curl -sk/)
    expect(curl.textContent).toContain('Connect-Protocol-Version: 1')
    expect(curl.textContent).toContain('$HOLOS_ID_TOKEN')
    expect(curl.textContent).toContain('OrganizationService/ListOrganizations')
  })

  it('renders a grpcurl -insecure example using $HOLOS_ID_TOKEN', () => {
    setAuthState()
    render(<ProfilePage />)
    const pre = screen.getByText(/grpcurl -insecure/)
    expect(pre.textContent).toContain('$HOLOS_ID_TOKEN')
    expect(pre.textContent).toContain('OrganizationService/ListOrganizations')
  })

  it('does not render grpcurl -plaintext', () => {
    setAuthState()
    render(<ProfilePage />)
    expect(screen.queryByText(/grpcurl -plaintext/)).not.toBeInTheDocument()
  })
})

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
    setAuthState({ user: makeUser({}, { aud: ['holos-console', 'other-client'] }) })
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

    // The raw-JSON pre has role="code"; there may be multiple if the API Access
    // section is rendered, so pick the one that contains JSON keys.
    const allCode = screen.getAllByRole('code')
    const jsonPre = allCode.find((el) => el.textContent?.includes('"iss"'))
    expect(jsonPre).toBeDefined()
    expect(jsonPre?.textContent).toContain('"aud"')
    expect(jsonPre?.textContent).toContain('"sub"')
  })

  it('shows Copy to Clipboard button in raw view', () => {
    setAuthState()
    render(<ProfilePage />)

    fireEvent.click(screen.getByRole('button', { name: /raw/i }))

    expect(screen.getByRole('button', { name: /copy to clipboard/i })).toBeInTheDocument()
  })
})

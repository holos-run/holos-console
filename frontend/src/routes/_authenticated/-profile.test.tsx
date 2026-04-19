import { render, screen, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
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

function mockClipboard() {
  const writeText = vi.fn().mockResolvedValue(undefined)
  Object.defineProperty(navigator, 'clipboard', {
    value: { writeText },
    writable: true,
    configurable: true,
  })
  return writeText
}

describe('ProfilePage API Access section', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockClipboard()
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

  it('copies a clean export line with the id_token on copy', async () => {
    const writeText = mockClipboard()
    setAuthState()
    render(<ProfilePage />)
    fireEvent.click(screen.getByRole('button', { name: /copy export snippet/i }))
    expect(writeText).toHaveBeenCalledOnce()
    const copied = writeText.mock.calls[0][0] as string
    expect(copied).toBe('export HOLOS_ID_TOKEN="id.token.value"')
    expect(copied).not.toContain('set +o history')
    expect(copied).not.toContain('set -o history')
  })

  it('renders shell history tabs with zsh and bash triggers', () => {
    setAuthState()
    render(<ProfilePage />)
    expect(screen.getByRole('tab', { name: /zsh/i })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: /bash/i })).toBeInTheDocument()
  })

  it('shows zsh tab content by default', () => {
    setAuthState()
    render(<ProfilePage />)
    // zsh tab is active by default — its content should be visible
    expect(screen.getByText(/setopt/i)).toBeInTheDocument()
  })

  it('switches to bash tab and shows bash-specific instructions', async () => {
    const user = userEvent.setup()
    setAuthState()
    render(<ProfilePage />)
    await user.click(screen.getByRole('tab', { name: /bash/i }))
    // The bash tab panel should be active; check the tab panel content
    const bashPanel = screen.getByRole('tabpanel')
    expect(bashPanel.textContent).toContain('set +o history')
  })

  it('clicking zsh tab after bash restores zsh content', async () => {
    const user = userEvent.setup()
    setAuthState()
    render(<ProfilePage />)

    // Switch to bash
    await user.click(screen.getByRole('tab', { name: /bash/i }))
    const bashPanel = screen.getByRole('tabpanel')
    expect(bashPanel.textContent).toContain('set +o history')

    // Switch back to zsh
    await user.click(screen.getByRole('tab', { name: /zsh/i }))
    const zshTab = screen.getByRole('tab', { name: /zsh/i })
    expect(zshTab).toHaveAttribute('data-state', 'active')

    // Active panel is now the zsh panel — content changes, no more bash-specific
    // instructions; the zsh panel includes setopt guidance.
    const zshPanel = screen.getByRole('tabpanel')
    expect(zshPanel.textContent).toContain('setopt')
    expect(zshPanel.textContent).not.toContain('set +o history')
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
    const curl = screen.getByText(/curl -s --cacert/)
    expect(curl.textContent).toContain('Connect-Protocol-Version: 1')
    expect(curl.textContent).toContain('$HOLOS_ID_TOKEN')
    expect(curl.textContent).toContain('OrganizationService/ListOrganizations')
    expect(curl.textContent).not.toContain('-k')
  })

  it('renders a grpcurl example using $HOLOS_ID_TOKEN', () => {
    setAuthState()
    render(<ProfilePage />)
    const pre = screen.getByText(/grpcurl -cacert/)
    expect(pre.textContent).toContain('$HOLOS_ID_TOKEN')
    expect(pre.textContent).toContain('OrganizationService/ListOrganizations')
    expect(pre.textContent).not.toContain('-insecure')
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

// HOL-656: per-persona email rendering. These tests replace the
// `Persona Switching > should login as platform engineer ...` and
// `Persona Switching > should switch from admin to SRE persona` cases from
// `frontend/e2e/multi-persona.spec.ts`. The E2E tests asserted that after
// swapping the OIDC `sessionStorage` entry for a persona's dev token, the
// profile page rendered the new email. At the component level we mock
// `useAuth` with the persona's profile directly — the sessionStorage/reload
// mechanics are covered by `transport.test.ts` and `-_authenticated.test.tsx`.
describe('ProfilePage persona email rendering', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it.each([
    ['platform engineer', 'platform@localhost'],
    ['product engineer', 'product@localhost'],
    ['SRE', 'sre@localhost'],
    ['admin', 'admin@localhost'],
  ])('renders %s email when useAuth returns the %s profile', (_persona, email) => {
    setAuthState({ user: makeUser({}, { email }) })
    render(<ProfilePage />)
    expect(screen.getByText(email)).toBeInTheDocument()
  })

  it('updates the displayed email when useAuth rerenders with a new persona', () => {
    setAuthState({ user: makeUser({}, { email: 'admin@localhost' }) })
    const { rerender } = render(<ProfilePage />)
    expect(screen.getByText('admin@localhost')).toBeInTheDocument()

    setAuthState({ user: makeUser({}, { email: 'sre@localhost' }) })
    rerender(<ProfilePage />)
    expect(screen.getByText('sre@localhost')).toBeInTheDocument()
    expect(screen.queryByText('admin@localhost')).not.toBeInTheDocument()
  })
})

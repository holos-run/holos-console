import { render, screen, within } from '@testing-library/react'
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

vi.mock('@/lib/auth', () => ({
  useAuth: vi.fn(),
  getUserManager: vi.fn().mockReturnValue({
    settings: { authority: 'https://localhost:8443/dex', client_id: 'holos-console' },
  }),
}))

vi.mock('@/lib/transport', () => ({
  tokenRef: { current: null },
}))

vi.mock('@/lib/console-config', () => ({
  getConsoleConfig: vi.fn(),
}))

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}))

import { useAuth } from '@/lib/auth'
import { getConsoleConfig } from '@/lib/console-config'
import { DevToolsPage } from './dev-tools'

function mockAuth(overrides: Partial<ReturnType<typeof useAuth>> = {}) {
  ;(useAuth as Mock).mockReturnValue({
    user: {
      access_token: 'test-access-token',
      id_token: 'test-id-token',
      profile: {
        email: 'platform@localhost',
        sub: 'test-platform-001',
        groups: ['owner'],
      },
    },
    isAuthenticated: true,
    isLoading: false,
    login: vi.fn(),
    logout: vi.fn(),
    getAccessToken: vi.fn(),
    refreshTokens: vi.fn(),
    lastRefreshStatus: 'idle' as const,
    lastRefreshTime: null,
    lastRefreshError: null,
    error: null,
    ...overrides,
  })
}

/** Helper to find a Card by its title text and return the card element. */
function getCardByTitle(title: string): HTMLElement {
  const heading = screen.getByText(title)
  // Walk up to the card element (the element with data-slot="card")
  let el: HTMLElement | null = heading
  while (el && el.getAttribute('data-slot') !== 'card') {
    el = el.parentElement
  }
  if (!el) throw new Error(`Could not find card element for title: ${title}`)
  return el
}

describe('DevToolsPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    ;(getConsoleConfig as Mock).mockReturnValue({ devToolsEnabled: true })
    mockAuth()
  })

  it('renders Current Identity card with email', () => {
    render(<DevToolsPage />)
    const card = getCardByTitle('Current Identity')
    expect(within(card).getByText('platform@localhost')).toBeInTheDocument()
  })

  it('renders groups from the current user profile', () => {
    render(<DevToolsPage />)
    const card = getCardByTitle('Current Identity')
    expect(within(card).getByText('owner')).toBeInTheDocument()
  })

  it('renders role badge for the current identity', () => {
    render(<DevToolsPage />)
    const card = getCardByTitle('Current Identity')
    expect(within(card).getByText('Owner')).toBeInTheDocument()
  })

  it('renders Persona Switcher card with all three personas', () => {
    render(<DevToolsPage />)
    expect(screen.getByText('Persona Switcher')).toBeInTheDocument()
    expect(screen.getByText('Platform Engineer')).toBeInTheDocument()
    expect(screen.getByText('Product Engineer')).toBeInTheDocument()
    expect(screen.getByText('SRE')).toBeInTheDocument()
  })

  it('renders Quick Token card', () => {
    render(<DevToolsPage />)
    expect(screen.getByText('Quick Token')).toBeInTheDocument()
  })

  it('shows not-available message when devToolsEnabled is false', () => {
    ;(getConsoleConfig as Mock).mockReturnValue({ devToolsEnabled: false })
    render(<DevToolsPage />)
    expect(screen.getByText(/dev tools are not enabled/i)).toBeInTheDocument()
  })

  it('shows sign-in prompt when not authenticated', () => {
    mockAuth({ isAuthenticated: false, user: null })
    render(<DevToolsPage />)
    expect(screen.getByRole('button', { name: /sign in/i })).toBeInTheDocument()
  })

  it('renders curl example in quick token card', () => {
    render(<DevToolsPage />)
    const card = getCardByTitle('Quick Token')
    expect(within(card).getByText(/curl example/i)).toBeInTheDocument()
  })

  it('renders the current user email and role for a different persona', () => {
    mockAuth({
      user: {
        access_token: 'tok',
        id_token: 'id-tok',
        profile: {
          email: 'sre@localhost',
          sub: 'test-sre-001',
          groups: ['viewer'],
        },
      } as ReturnType<typeof useAuth>['user'],
    })
    render(<DevToolsPage />)
    const card = getCardByTitle('Current Identity')
    expect(within(card).getByText('sre@localhost')).toBeInTheDocument()
    expect(within(card).getByText('Viewer')).toBeInTheDocument()
  })

  it('marks the current persona as active in the switcher', () => {
    render(<DevToolsPage />)
    // The Platform Engineer card should show "Current" text since profile email is platform@localhost
    expect(screen.getByText('Current')).toBeInTheDocument()
  })
})

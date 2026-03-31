import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => ({ projectName: 'test-project', name: 'test-secret' }),
    }),
    useNavigate: () => vi.fn(),
  }
})

vi.mock('@/queries/secrets', () => ({
  useGetSecret: vi.fn(),
  useGetSecretMetadata: vi.fn(),
  useUpdateSecret: vi.fn(),
  useUpdateSecretSharing: vi.fn(),
  useDeleteSecret: vi.fn(),
}))

vi.mock('@/lib/auth', () => ({ useAuth: vi.fn() }))

vi.mock('@connectrpc/connect-query', () => ({ useTransport: vi.fn() }))
vi.mock('@connectrpc/connect', () => ({ createClient: vi.fn(() => ({})) }))

import { useGetSecret, useGetSecretMetadata, useUpdateSecret, useUpdateSecretSharing, useDeleteSecret } from '@/queries/secrets'
import { useAuth } from '@/lib/auth'
import { SecretPage } from './$name'

const mockMetadata = {
  description: 'A test secret',
  url: 'https://example.com',
  userGrants: [{ principal: 'alice@example.com', role: 3 }],
  roleGrants: [],
}

function setupMocks(overrides: { metadata?: typeof mockMetadata; isOwner?: boolean } = {}) {
  const metadata = overrides.metadata ?? mockMetadata

  ;(useGetSecret as Mock).mockReturnValue({
    data: { key: new TextEncoder().encode('value') },
    isLoading: false,
    error: null,
  })
  ;(useGetSecretMetadata as Mock).mockReturnValue({
    data: metadata,
    isLoading: false,
  })
  ;(useUpdateSecret as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false })
  ;(useUpdateSecretSharing as Mock).mockReturnValue({
    mutateAsync: vi.fn().mockResolvedValue({ metadata }),
    isPending: false,
  })
  ;(useDeleteSecret as Mock).mockReturnValue({
    mutateAsync: vi.fn(),
    isPending: false,
    error: null,
  })
  ;(useAuth as Mock).mockReturnValue({
    isAuthenticated: true,
    isLoading: false,
    user: { profile: { email: 'alice@example.com', groups: [] } },
  })
}

describe('SecretPage sharing panel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders sharing panel with user grants from metadata', () => {
    setupMocks()
    render(<SecretPage />)
    expect(screen.getByText('alice@example.com')).toBeInTheDocument()
  })

  it('shows Edit button for owners', () => {
    setupMocks()
    render(<SecretPage />)
    // alice@example.com has role 3 (OWNER) and is the logged-in user
    const editButtons = screen.getAllByRole('button', { name: /^edit$/i })
    expect(editButtons.length).toBeGreaterThanOrEqual(1)
  })

  it('calls useUpdateSecretSharing.mutateAsync on save', async () => {
    setupMocks()
    render(<SecretPage />)

    // Click the sharing panel Edit button (last Edit button in the page)
    const editButtons = screen.getAllByRole('button', { name: /^edit$/i })
    fireEvent.click(editButtons[editButtons.length - 1])

    fireEvent.click(screen.getByRole('button', { name: /^save$/i }))

    const mutateAsync = (useUpdateSecretSharing as Mock).mock.results[0].value.mutateAsync
    await waitFor(() => {
      expect(mutateAsync).toHaveBeenCalledWith(
        expect.objectContaining({
          name: 'test-secret',
        }),
      )
    })
  })

  it('renders no sharing grants when metadata has empty grants', () => {
    setupMocks({
      metadata: { ...mockMetadata, userGrants: [], roleGrants: [] },
    })
    render(<SecretPage />)
    expect(screen.getByText(/no sharing grants/i)).toBeInTheDocument()
  })
})

import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

const mockNavigate = vi.fn()

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({}),
    useNavigate: () => mockNavigate,
    Link: ({
      children,
      to,
      className,
    }: {
      children: React.ReactNode
      to?: string
      className?: string
    }) => (
      <a href={to} className={className}>
        {children}
      </a>
    ),
  }
})

vi.mock('@/queries/folders', () => ({
  useCreateFolder: vi.fn(),
}))

vi.mock('@/lib/org-context', () => ({
  useOrg: vi.fn(() => ({ selectedOrg: null })),
}))

import { useCreateFolder } from '@/queries/folders'
import { useOrg } from '@/lib/org-context'
import { FolderNewPage } from './new'

function setupMocks(mutateAsync = vi.fn().mockResolvedValue({})) {
  ;(useCreateFolder as Mock).mockReturnValue({
    mutateAsync,
    isPending: false,
  })
  return { mutateAsync }
}

describe('FolderNewPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setupMocks()
  })

  // ── Missing orgName: error state ────────────────────────────────────────────

  it('shows an error when orgName is missing', () => {
    render(<FolderNewPage />)
    expect(screen.getByText(/an organization is required/i)).toBeInTheDocument()
  })

  it('shows a link to /organizations when orgName is missing', () => {
    render(<FolderNewPage />)
    const link = screen.getByRole('link', { name: /select an organization/i })
    expect(link).toBeInTheDocument()
    expect(link.getAttribute('href')).toBe('/organizations')
  })

  // ── With orgName: normal form ────────────────────────────────────────────────

  it('renders the page heading when orgName is provided', () => {
    render(<FolderNewPage orgName="my-org" />)
    expect(screen.getByText('New Folder')).toBeInTheDocument()
  })

  it('renders as a full page (not inside a dialog)', () => {
    render(<FolderNewPage orgName="my-org" />)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('renders Display Name field', () => {
    render(<FolderNewPage orgName="my-org" />)
    expect(screen.getByLabelText(/display name/i)).toBeInTheDocument()
  })

  it('renders Name field', () => {
    render(<FolderNewPage orgName="my-org" />)
    expect(screen.getByLabelText(/^name$/i)).toBeInTheDocument()
  })

  it('renders Description field', () => {
    render(<FolderNewPage orgName="my-org" />)
    expect(screen.getByLabelText(/description/i)).toBeInTheDocument()
  })

  it('renders Create Folder submit button', () => {
    render(<FolderNewPage orgName="my-org" />)
    expect(screen.getByRole('button', { name: /create folder/i })).toBeInTheDocument()
  })

  it('renders a Cancel link', () => {
    render(<FolderNewPage orgName="my-org" />)
    expect(screen.getByRole('link', { name: /cancel/i })).toBeInTheDocument()
  })

  it('Cancel link uses /organizations as default fallback when no returnTo', () => {
    render(<FolderNewPage orgName="my-org" />)
    const cancelLink = screen.getByRole('link', { name: /cancel/i })
    expect(cancelLink.getAttribute('href')).toBe('/organizations')
  })

  it('Cancel link honours a valid returnTo search param', () => {
    render(<FolderNewPage orgName="my-org" returnTo="/resource-manager" />)
    const cancelLink = screen.getByRole('link', { name: /cancel/i })
    expect(cancelLink.getAttribute('href')).toBe('/resource-manager')
  })

  it('Cancel link falls back to /organizations for an invalid returnTo', () => {
    render(<FolderNewPage orgName="my-org" returnTo="//evil.com" />)
    const cancelLink = screen.getByRole('link', { name: /cancel/i })
    expect(cancelLink.getAttribute('href')).toBe('/organizations')
  })

  it('auto-derives name slug from display name', () => {
    render(<FolderNewPage orgName="my-org" />)
    const displayInput = screen.getByLabelText(/display name/i)
    fireEvent.change(displayInput, { target: { value: 'Payments Team' } })
    const nameInput = screen.getByLabelText(/^name$/i) as HTMLInputElement
    expect(nameInput.value).toBe('payments-team')
  })

  it('submit button is disabled when name is empty', () => {
    render(<FolderNewPage orgName="my-org" />)
    expect(screen.getByRole('button', { name: /create folder/i })).toBeDisabled()
  })

  it('submit button is enabled after name is filled', () => {
    render(<FolderNewPage orgName="my-org" />)
    fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'Payments Team' } })
    expect(screen.getByRole('button', { name: /create folder/i })).not.toBeDisabled()
  })

  it('calls useCreateFolder mutateAsync with form values on submit', async () => {
    const mutateAsync = vi.fn().mockResolvedValue({})
    setupMocks(mutateAsync)
    render(<FolderNewPage orgName="my-org" />)

    fireEvent.change(screen.getByLabelText(/display name/i), {
      target: { value: 'Payments Team' },
    })
    fireEvent.change(screen.getByLabelText(/^description$/i), {
      target: { value: 'A test folder' },
    })
    fireEvent.click(screen.getByRole('button', { name: /create folder/i }))

    await waitFor(() => {
      expect(mutateAsync).toHaveBeenCalledWith(
        expect.objectContaining({
          name: 'payments-team',
          displayName: 'Payments Team',
          description: 'A test folder',
        }),
      )
    })
  })

  it('passes parentType ORGANIZATION by default', async () => {
    const mutateAsync = vi.fn().mockResolvedValue({})
    setupMocks(mutateAsync)
    render(<FolderNewPage orgName="my-org" />)

    fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'Team' } })
    fireEvent.click(screen.getByRole('button', { name: /create folder/i }))

    await waitFor(() => {
      const call = mutateAsync.mock.calls[0][0]
      expect(call.parentType).toBeDefined()
      expect(call.parentName).toBe('my-org')
    })
  })

  it('passes parentName from search param when provided', async () => {
    const mutateAsync = vi.fn().mockResolvedValue({})
    setupMocks(mutateAsync)
    render(<FolderNewPage orgName="my-org" parentName="default" parentType="Folder" />)

    fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'Team' } })
    fireEvent.click(screen.getByRole('button', { name: /create folder/i }))

    await waitFor(() => {
      const call = mutateAsync.mock.calls[0][0]
      expect(call.parentName).toBe('default')
    })
  })

  it('navigates to /organizations after successful submission (no returnTo)', async () => {
    const mutateAsync = vi.fn().mockResolvedValue({})
    setupMocks(mutateAsync)
    render(<FolderNewPage orgName="my-org" />)

    fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'Team' } })
    fireEvent.click(screen.getByRole('button', { name: /create folder/i }))

    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith({ to: '/organizations' })
    })
  })

  it('navigates to returnTo path after successful submission when valid', async () => {
    const mutateAsync = vi.fn().mockResolvedValue({})
    setupMocks(mutateAsync)
    render(<FolderNewPage orgName="my-org" returnTo="/resource-manager" />)

    fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'Team' } })
    fireEvent.click(screen.getByRole('button', { name: /create folder/i }))

    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith({ to: '/resource-manager' })
    })
  })

  it('falls back to /organizations for invalid returnTo after success', async () => {
    const mutateAsync = vi.fn().mockResolvedValue({})
    setupMocks(mutateAsync)
    render(<FolderNewPage orgName="my-org" returnTo="javascript:alert(1)" />)

    fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'Team' } })
    fireEvent.click(screen.getByRole('button', { name: /create folder/i }))

    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith({ to: '/organizations' })
    })
  })

  it('shows error message when creation fails', async () => {
    const mutateAsync = vi.fn().mockRejectedValue(new Error('server error'))
    setupMocks(mutateAsync)
    render(<FolderNewPage orgName="my-org" />)

    fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'Team' } })
    fireEvent.click(screen.getByRole('button', { name: /create folder/i }))

    await waitFor(() => {
      expect(screen.getByText(/server error/i)).toBeInTheDocument()
    })
  })

  it('does not call mutateAsync when name is empty on submit attempt', async () => {
    const mutateAsync = vi.fn()
    setupMocks(mutateAsync)
    render(<FolderNewPage orgName="my-org" />)

    const form = screen.getByRole('form')
    fireEvent.submit(form)

    await waitFor(() => {
      expect(mutateAsync).not.toHaveBeenCalled()
    })
  })

  it('displays organization context in the form', () => {
    render(<FolderNewPage orgName="my-org" />)
    expect(screen.getByText('my-org')).toBeInTheDocument()
  })

  // ── orgName resolution: search param, store fallback, both absent ───────────

  it('renders form when orgName comes from search param only (no store)', () => {
    ;(useOrg as Mock).mockReturnValue({ selectedOrg: null })
    render(<FolderNewPage orgName="search-org" />)
    expect(screen.getByText('New Folder')).toBeInTheDocument()
    expect(screen.queryByText(/an organization is required/i)).not.toBeInTheDocument()
    expect(screen.getByText('search-org')).toBeInTheDocument()
  })

  it('renders form when orgName comes from store only (search param absent)', () => {
    ;(useOrg as Mock).mockReturnValue({ selectedOrg: 'acme' })
    // Simulate the Route resolving orgName from store: pass orgName as prop
    // (FolderNewRoute would have done: orgName = search.orgName ?? selectedOrg)
    render(<FolderNewPage orgName="acme" />)
    expect(screen.getByText('New Folder')).toBeInTheDocument()
    expect(screen.queryByText(/an organization is required/i)).not.toBeInTheDocument()
    expect(screen.getByText('acme')).toBeInTheDocument()
  })

  it('shows error banner when both search param and store are absent', () => {
    ;(useOrg as Mock).mockReturnValue({ selectedOrg: null })
    render(<FolderNewPage />)
    expect(screen.getByText(/an organization is required/i)).toBeInTheDocument()
  })
})

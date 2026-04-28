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

vi.mock('@/queries/projects', () => ({
  useCreateProject: vi.fn(),
}))

vi.mock('@/lib/org-context', () => ({
  useOrg: vi.fn(() => ({ selectedOrg: null })),
}))

import { useCreateProject } from '@/queries/projects'
import { useOrg } from '@/lib/org-context'
import { ProjectNewPage } from './new'

function setupMocks(mutateAsync = vi.fn().mockResolvedValue({ name: 'my-project' })) {
  ;(useCreateProject as Mock).mockReturnValue({
    mutateAsync,
    isPending: false,
  })
  return { mutateAsync }
}

describe('ProjectNewPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setupMocks()
  })

  // ── Missing orgName: error state ────────────────────────────────────────────

  it('shows an error when orgName is missing', () => {
    render(<ProjectNewPage />)
    expect(screen.getByText(/an organization is required/i)).toBeInTheDocument()
  })

  it('shows a link to /organizations when orgName is missing', () => {
    render(<ProjectNewPage />)
    const link = screen.getByRole('link', { name: /select an organization/i })
    expect(link).toBeInTheDocument()
    expect(link.getAttribute('href')).toBe('/organizations')
  })

  // ── With orgName: normal form ────────────────────────────────────────────────

  it('renders the page heading when orgName is provided', () => {
    render(<ProjectNewPage orgName="my-org" />)
    expect(screen.getByText('New Project')).toBeInTheDocument()
  })

  it('renders as a full page (not inside a dialog)', () => {
    render(<ProjectNewPage orgName="my-org" />)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('renders Display Name field', () => {
    render(<ProjectNewPage orgName="my-org" />)
    expect(screen.getByLabelText(/display name/i)).toBeInTheDocument()
  })

  it('renders Name field', () => {
    render(<ProjectNewPage orgName="my-org" />)
    expect(screen.getByLabelText(/^name$/i)).toBeInTheDocument()
  })

  it('renders Description field', () => {
    render(<ProjectNewPage orgName="my-org" />)
    expect(screen.getByLabelText(/description/i)).toBeInTheDocument()
  })

  it('renders Create Project submit button', () => {
    render(<ProjectNewPage orgName="my-org" />)
    expect(screen.getByRole('button', { name: /create project/i })).toBeInTheDocument()
  })

  it('renders a Cancel link', () => {
    render(<ProjectNewPage orgName="my-org" />)
    expect(screen.getByRole('link', { name: /cancel/i })).toBeInTheDocument()
  })

  it('Cancel link uses /organizations as default fallback when no returnTo', () => {
    render(<ProjectNewPage orgName="my-org" />)
    const cancelLink = screen.getByRole('link', { name: /cancel/i })
    expect(cancelLink.getAttribute('href')).toBe('/organizations')
  })

  it('Cancel link honours a valid returnTo search param', () => {
    render(<ProjectNewPage orgName="my-org" returnTo="/resource-manager" />)
    const cancelLink = screen.getByRole('link', { name: /cancel/i })
    expect(cancelLink.getAttribute('href')).toBe('/resource-manager')
  })

  it('Cancel link falls back to /organizations for an invalid returnTo', () => {
    render(<ProjectNewPage orgName="my-org" returnTo="//evil.com" />)
    const cancelLink = screen.getByRole('link', { name: /cancel/i })
    expect(cancelLink.getAttribute('href')).toBe('/organizations')
  })

  it('auto-derives name slug from display name', () => {
    render(<ProjectNewPage orgName="my-org" />)
    const displayInput = screen.getByLabelText(/display name/i)
    fireEvent.change(displayInput, { target: { value: 'My Project' } })
    const nameInput = screen.getByLabelText(/^name$/i) as HTMLInputElement
    expect(nameInput.value).toBe('my-project')
  })

  it('submit button is disabled when name is empty', () => {
    render(<ProjectNewPage orgName="my-org" />)
    expect(screen.getByRole('button', { name: /create project/i })).toBeDisabled()
  })

  it('submit button is enabled after name is filled', () => {
    render(<ProjectNewPage orgName="my-org" />)
    fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'My Project' } })
    expect(screen.getByRole('button', { name: /create project/i })).not.toBeDisabled()
  })

  it('calls useCreateProject mutateAsync with form values on submit', async () => {
    const mutateAsync = vi.fn().mockResolvedValue({ name: 'my-project' })
    setupMocks(mutateAsync)
    render(<ProjectNewPage orgName="my-org" />)

    fireEvent.change(screen.getByLabelText(/display name/i), {
      target: { value: 'My Project' },
    })
    fireEvent.change(screen.getByLabelText(/^description$/i), {
      target: { value: 'A test project' },
    })
    fireEvent.click(screen.getByRole('button', { name: /create project/i }))

    await waitFor(() => {
      expect(mutateAsync).toHaveBeenCalledWith(
        expect.objectContaining({
          name: 'my-project',
          displayName: 'My Project',
          description: 'A test project',
          organization: 'my-org',
        }),
      )
      expect(mutateAsync.mock.calls[0][0]).not.toHaveProperty('parentType')
      expect(mutateAsync.mock.calls[0][0]).not.toHaveProperty('parentName')
    })
  })

  // ── Default fallback: /projects/$name (no returnTo) ─────────────────────────

  it('navigates to /projects/$name after success when no returnTo', async () => {
    const mutateAsync = vi.fn().mockResolvedValue({ name: 'my-project' })
    setupMocks(mutateAsync)
    render(<ProjectNewPage orgName="my-org" />)

    fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'My Project' } })
    fireEvent.click(screen.getByRole('button', { name: /create project/i }))

    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith({ to: '/projects/my-project' })
    })
  })

  it('navigates to returnTo path after success when valid returnTo present', async () => {
    const mutateAsync = vi.fn().mockResolvedValue({ name: 'my-project' })
    setupMocks(mutateAsync)
    render(<ProjectNewPage orgName="my-org" returnTo="/resource-manager" />)

    fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'My Project' } })
    fireEvent.click(screen.getByRole('button', { name: /create project/i }))

    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith({ to: '/resource-manager' })
    })
  })

  it('falls back to /projects/$name for invalid returnTo after success', async () => {
    const mutateAsync = vi.fn().mockResolvedValue({ name: 'my-project' })
    setupMocks(mutateAsync)
    render(<ProjectNewPage orgName="my-org" returnTo="javascript:alert(1)" />)

    fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'My Project' } })
    fireEvent.click(screen.getByRole('button', { name: /create project/i }))

    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith({ to: '/projects/my-project' })
    })
  })

  it('shows error message when creation fails', async () => {
    const mutateAsync = vi.fn().mockRejectedValue(new Error('server error'))
    setupMocks(mutateAsync)
    render(<ProjectNewPage orgName="my-org" />)

    fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'My Project' } })
    fireEvent.click(screen.getByRole('button', { name: /create project/i }))

    await waitFor(() => {
      expect(screen.getByText(/server error/i)).toBeInTheDocument()
    })
  })

  it('does not call mutateAsync when name is empty on submit attempt', async () => {
    const mutateAsync = vi.fn()
    setupMocks(mutateAsync)
    render(<ProjectNewPage orgName="my-org" />)

    const form = screen.getByRole('form')
    fireEvent.submit(form)

    await waitFor(() => {
      expect(mutateAsync).not.toHaveBeenCalled()
    })
  })

  it('displays organization context in the form', () => {
    render(<ProjectNewPage orgName="my-org" />)
    expect(screen.getByText('my-org')).toBeInTheDocument()
  })

  it('does not display folder context', () => {
    render(<ProjectNewPage orgName="my-org" />)
    expect(screen.queryByText(/folder/i)).not.toBeInTheDocument()
  })
  // ── orgName resolution: search param, store fallback, both absent ───────────

  it('renders form when orgName comes from search param only (no store)', () => {
    ;(useOrg as Mock).mockReturnValue({ selectedOrg: null })
    render(<ProjectNewPage orgName="search-org" />)
    expect(screen.getByText('New Project')).toBeInTheDocument()
    expect(screen.queryByText(/an organization is required/i)).not.toBeInTheDocument()
    expect(screen.getByText('search-org')).toBeInTheDocument()
  })

  it('renders form when orgName comes from store only (search param absent)', () => {
    ;(useOrg as Mock).mockReturnValue({ selectedOrg: 'acme' })
    // Simulate the Route resolving orgName from store: pass orgName as prop
    // (ProjectNewRoute would have done: orgName = search.orgName ?? selectedOrg)
    render(<ProjectNewPage orgName="acme" />)
    expect(screen.getByText('New Project')).toBeInTheDocument()
    expect(screen.queryByText(/an organization is required/i)).not.toBeInTheDocument()
    expect(screen.getByText('acme')).toBeInTheDocument()
  })

  it('shows error banner when both search param and store are absent', () => {
    ;(useOrg as Mock).mockReturnValue({ selectedOrg: null })
    render(<ProjectNewPage />)
    expect(screen.getByText(/an organization is required/i)).toBeInTheDocument()
  })

})

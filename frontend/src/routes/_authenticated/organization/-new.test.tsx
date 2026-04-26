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
    }: {
      children: React.ReactNode
      to?: string
    }) => <a href={to}>{children}</a>,
  }
})

vi.mock('@/queries/organizations', () => ({
  useCreateOrganization: vi.fn(),
}))

// Tooltip primitives rely on Radix portals not available in jsdom.
vi.mock('@/components/ui/tooltip', () => ({
  TooltipProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  Tooltip: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipContent: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}))

import { useCreateOrganization } from '@/queries/organizations'
import { OrganizationNewPage } from './new'

function setupMocks(mutateAsync = vi.fn().mockResolvedValue({})) {
  ;(useCreateOrganization as Mock).mockReturnValue({
    mutateAsync,
    isPending: false,
  })
  return { mutateAsync }
}

describe('OrganizationNewPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setupMocks()
  })

  it('renders the page heading', () => {
    render(<OrganizationNewPage />)
    expect(screen.getByText('New Organization')).toBeInTheDocument()
  })

  it('renders as a full page (not inside a dialog)', () => {
    render(<OrganizationNewPage />)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('renders Display Name field', () => {
    render(<OrganizationNewPage />)
    expect(screen.getByLabelText(/display name/i)).toBeInTheDocument()
  })

  it('renders Name field', () => {
    render(<OrganizationNewPage />)
    expect(screen.getByLabelText(/^name$/i)).toBeInTheDocument()
  })

  it('renders Description field', () => {
    render(<OrganizationNewPage />)
    expect(screen.getByLabelText(/description/i)).toBeInTheDocument()
  })

  it('renders Create Organization submit button', () => {
    render(<OrganizationNewPage />)
    expect(screen.getByRole('button', { name: /create organization/i })).toBeInTheDocument()
  })

  it('renders a Cancel link', () => {
    render(<OrganizationNewPage />)
    expect(screen.getByRole('link', { name: /cancel/i })).toBeInTheDocument()
  })

  it('Cancel link uses /organizations as default fallback when no returnTo', () => {
    render(<OrganizationNewPage />)
    const cancelLink = screen.getByRole('link', { name: /cancel/i })
    expect(cancelLink.getAttribute('href')).toBe('/organizations')
  })

  it('Cancel link honours a valid returnTo search param', () => {
    render(<OrganizationNewPage returnTo="/resource-manager" />)
    const cancelLink = screen.getByRole('link', { name: /cancel/i })
    expect(cancelLink.getAttribute('href')).toBe('/resource-manager')
  })

  it('Cancel link falls back to /organizations for an invalid returnTo', () => {
    render(<OrganizationNewPage returnTo="//evil.com" />)
    const cancelLink = screen.getByRole('link', { name: /cancel/i })
    expect(cancelLink.getAttribute('href')).toBe('/organizations')
  })

  it('auto-derives name slug from display name', () => {
    render(<OrganizationNewPage />)
    const displayInput = screen.getByLabelText(/display name/i)
    fireEvent.change(displayInput, { target: { value: 'My Org' } })
    const nameInput = screen.getByLabelText(/^name$/i) as HTMLInputElement
    expect(nameInput.value).toBe('my-org')
  })

  it('submit button is disabled when name is empty', () => {
    render(<OrganizationNewPage />)
    expect(screen.getByRole('button', { name: /create organization/i })).toBeDisabled()
  })

  it('submit button is enabled after name is filled', () => {
    render(<OrganizationNewPage />)
    fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'My Org' } })
    expect(screen.getByRole('button', { name: /create organization/i })).not.toBeDisabled()
  })

  it('calls useCreateOrganization mutateAsync with form values on submit', async () => {
    const mutateAsync = vi.fn().mockResolvedValue({})
    setupMocks(mutateAsync)
    render(<OrganizationNewPage />)

    fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'My Org' } })
    fireEvent.change(screen.getByLabelText(/^description$/i), {
      target: { value: 'A test org' },
    })
    fireEvent.click(screen.getByRole('button', { name: /create organization/i }))

    await waitFor(() => {
      expect(mutateAsync).toHaveBeenCalledWith(
        expect.objectContaining({
          name: 'my-org',
          displayName: 'My Org',
          description: 'A test org',
        }),
      )
    })
  })

  it('navigates to /organizations/$name/projects after successful submission (no returnTo)', async () => {
    const mutateAsync = vi.fn().mockResolvedValue({})
    setupMocks(mutateAsync)
    render(<OrganizationNewPage />)

    fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'My Org' } })
    fireEvent.click(screen.getByRole('button', { name: /create organization/i }))

    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith({ to: '/organizations/my-org/projects' })
    })
  })

  it('navigates to returnTo path after successful submission when valid', async () => {
    const mutateAsync = vi.fn().mockResolvedValue({})
    setupMocks(mutateAsync)
    render(<OrganizationNewPage returnTo="/resource-manager" />)

    fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'My Org' } })
    fireEvent.click(screen.getByRole('button', { name: /create organization/i }))

    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith({ to: '/resource-manager' })
    })
  })

  it('falls back to /organizations/$name/projects for invalid returnTo after success', async () => {
    const mutateAsync = vi.fn().mockResolvedValue({})
    setupMocks(mutateAsync)
    render(<OrganizationNewPage returnTo="javascript:alert(1)" />)

    fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'My Org' } })
    fireEvent.click(screen.getByRole('button', { name: /create organization/i }))

    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith({ to: '/organizations/my-org/projects' })
    })
  })

  it('shows error message when creation fails', async () => {
    const mutateAsync = vi.fn().mockRejectedValue(new Error('server error'))
    setupMocks(mutateAsync)
    render(<OrganizationNewPage />)

    fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'My Org' } })
    fireEvent.click(screen.getByRole('button', { name: /create organization/i }))

    await waitFor(() => {
      expect(screen.getByText(/server error/i)).toBeInTheDocument()
    })
  })

  it('does not call mutateAsync when name is empty on submit attempt', async () => {
    const mutateAsync = vi.fn()
    setupMocks(mutateAsync)
    render(<OrganizationNewPage />)

    // Force submit with empty name by directly submitting the form
    const form = screen.getByRole('form')
    fireEvent.submit(form)

    await waitFor(() => {
      expect(mutateAsync).not.toHaveBeenCalled()
    })
  })
})

import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

const mockNavigate = vi.fn()

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => ({ orgName: 'test-org' }),
    }),
    useNavigate: () => mockNavigate,
    Link: ({ children, className, to, params }: { children: React.ReactNode; className?: string; to?: string; params?: Record<string, string> }) => (
      <a href={to} data-params={JSON.stringify(params)} className={className}>{children}</a>
    ),
  }
})

vi.mock('@/queries/templates', () => ({
  useCreateTemplate: vi.fn(),
  makeOrgScope: vi.fn().mockReturnValue({ scope: 2, scopeName: 'test-org' }),
}))

vi.mock('@/queries/organizations', () => ({
  useGetOrganization: vi.fn(),
}))

import { useCreateTemplate } from '@/queries/templates'
import { useGetOrganization } from '@/queries/organizations'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { CreateOrgTemplatePage } from './new'

function setupMocks(
  mutateAsync = vi.fn().mockResolvedValue({}),
  userRole = Role.OWNER,
) {
  ;(useCreateTemplate as Mock).mockReturnValue({
    mutateAsync,
    isPending: false,
    reset: vi.fn(),
  })
  ;(useGetOrganization as Mock).mockReturnValue({
    data: { userRole },
    isPending: false,
    error: null,
  })
}

describe('CreateOrgTemplatePage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setupMocks()
  })

  it('renders the page heading', () => {
    render(<CreateOrgTemplatePage orgName="test-org" />)
    expect(screen.getByText(/create platform template/i)).toBeInTheDocument()
  })

  it('renders Display Name field', () => {
    render(<CreateOrgTemplatePage orgName="test-org" />)
    expect(screen.getByLabelText(/display name/i)).toBeInTheDocument()
  })

  it('renders Name (slug) field', () => {
    render(<CreateOrgTemplatePage orgName="test-org" />)
    expect(screen.getByLabelText(/name slug/i)).toBeInTheDocument()
  })

  it('renders Description field', () => {
    render(<CreateOrgTemplatePage orgName="test-org" />)
    expect(screen.getByLabelText(/description/i)).toBeInTheDocument()
  })

  it('renders CUE Template textarea', () => {
    render(<CreateOrgTemplatePage orgName="test-org" />)
    expect(screen.getByRole('textbox', { name: /cue template/i })).toBeInTheDocument()
  })

  it('renders Enabled switch defaulting to unchecked', () => {
    render(<CreateOrgTemplatePage orgName="test-org" />)
    const toggle = screen.getByRole('switch', { name: /enabled/i })
    expect(toggle).toBeInTheDocument()
    expect(toggle).toHaveAttribute('data-state', 'unchecked')
  })

  it('renders Create submit button', () => {
    render(<CreateOrgTemplatePage orgName="test-org" />)
    expect(screen.getByRole('button', { name: /^create$/i })).toBeInTheDocument()
  })

  it('renders a Cancel link back to the templates list', () => {
    render(<CreateOrgTemplatePage orgName="test-org" />)
    expect(screen.getByRole('link', { name: /cancel/i })).toBeInTheDocument()
  })

  it('auto-derives slug from display name', () => {
    render(<CreateOrgTemplatePage orgName="test-org" />)
    const displayNameInput = screen.getByLabelText(/display name/i)
    fireEvent.change(displayNameInput, { target: { value: 'My Web App' } })
    const slugInput = screen.getByLabelText(/name slug/i) as HTMLInputElement
    expect(slugInput.value).toBe('my-web-app')
  })

  it('shows validation error when name is empty', async () => {
    render(<CreateOrgTemplatePage orgName="test-org" />)
    fireEvent.click(screen.getByRole('button', { name: /^create$/i }))
    await waitFor(() => {
      expect(screen.getByText(/template name is required/i)).toBeInTheDocument()
    })
  })

  it('calls createMutation with form values on submit', async () => {
    const mutateAsync = vi.fn().mockResolvedValue({})
    setupMocks(mutateAsync)
    render(<CreateOrgTemplatePage orgName="test-org" />)

    fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'My Template' } })
    fireEvent.change(screen.getByLabelText(/description/i), { target: { value: 'A description' } })
    fireEvent.click(screen.getByRole('button', { name: /^create$/i }))

    await waitFor(() => {
      expect(mutateAsync).toHaveBeenCalledWith(
        expect.objectContaining({
          name: 'my-template',
          displayName: 'My Template',
          description: 'A description',
          enabled: false,
        }),
      )
    })
  })

  // HOL-555 removed the Mandatory proto field; HOL-558 shifts the concept to
  // TemplatePolicy REQUIRE rules. The Mandatory toggle must not render on the
  // org-template create form and the mutation payload must not carry a
  // mandatory field.
  it('does not render a Mandatory toggle (removed in HOL-555)', () => {
    render(<CreateOrgTemplatePage orgName="test-org" />)
    expect(screen.queryByRole('switch', { name: /mandatory/i })).not.toBeInTheDocument()
    expect(screen.queryByLabelText(/mandatory/i)).not.toBeInTheDocument()
  })

  it('does not pass mandatory in the mutation payload', async () => {
    const mutateAsync = vi.fn().mockResolvedValue({})
    setupMocks(mutateAsync)
    render(<CreateOrgTemplatePage orgName="test-org" />)

    fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'My Template' } })
    fireEvent.click(screen.getByRole('button', { name: /^create$/i }))

    await waitFor(() => {
      expect(mutateAsync).toHaveBeenCalled()
    })
    const payload = mutateAsync.mock.calls[0][0]
    expect(payload).not.toHaveProperty('mandatory')
  })

  it('navigates to template detail page after successful creation', async () => {
    const mutateAsync = vi.fn().mockResolvedValue({})
    setupMocks(mutateAsync)
    render(<CreateOrgTemplatePage orgName="test-org" />)

    fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'My Template' } })
    fireEvent.click(screen.getByRole('button', { name: /^create$/i }))

    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith(
        expect.objectContaining({
          to: '/orgs/$orgName/settings/org-templates/$templateName',
          params: expect.objectContaining({ orgName: 'test-org', templateName: 'my-template' }),
        }),
      )
    })
  })

  it('shows error message when creation fails', async () => {
    const mutateAsync = vi.fn().mockRejectedValue(new Error('server error'))
    setupMocks(mutateAsync)
    render(<CreateOrgTemplatePage orgName="test-org" />)

    fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'My Template' } })
    fireEvent.click(screen.getByRole('button', { name: /^create$/i }))

    await waitFor(() => {
      expect(screen.getByText(/server error/i)).toBeInTheDocument()
    })
  })

  it('renders breadcrumb navigation', () => {
    render(<CreateOrgTemplatePage orgName="test-org" />)
    expect(screen.getByText('test-org')).toBeInTheDocument()
    expect(screen.getByText('Settings')).toBeInTheDocument()
    expect(screen.getByText('Platform Templates')).toBeInTheDocument()
  })

  describe('Load httpbin Example button', () => {
    it('renders Load httpbin Example button', () => {
      render(<CreateOrgTemplatePage orgName="test-org" />)
      expect(screen.getByRole('button', { name: /load httpbin example/i })).toBeInTheDocument()
    })

    it('clicking Load httpbin Example populates all form fields', () => {
      render(<CreateOrgTemplatePage orgName="test-org" />)
      fireEvent.click(screen.getByRole('button', { name: /load httpbin example/i }))

      const nameInput = screen.getByLabelText(/name slug/i) as HTMLInputElement
      expect(nameInput.value).toBe('httpbin-platform')

      const displayNameInput = screen.getByLabelText(/display name/i) as HTMLInputElement
      expect(displayNameInput.value).toBe('httpbin Platform')

      const descriptionInput = screen.getByLabelText(/description/i) as HTMLInputElement
      expect(descriptionInput.value).toContain('HTTPRoute')

      const cueEditor = screen.getByRole('textbox', { name: /cue template/i }) as HTMLTextAreaElement
      expect(cueEditor.value).toContain('HTTPRoute')
      expect(cueEditor.value).toContain('platformResources')
    })
  })

  describe('Enabled switch', () => {
    it('passes enabled: true when toggle is switched on', async () => {
      const mutateAsync = vi.fn().mockResolvedValue({})
      setupMocks(mutateAsync)
      render(<CreateOrgTemplatePage orgName="test-org" />)

      fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'My Template' } })
      fireEvent.click(screen.getByRole('switch', { name: /enabled/i }))
      fireEvent.click(screen.getByRole('button', { name: /^create$/i }))

      await waitFor(() => {
        expect(mutateAsync).toHaveBeenCalledWith(
          expect.objectContaining({
            enabled: true,
          }),
        )
      })
    })
  })

  describe('permissions', () => {
    it('disables form fields for non-OWNER users', () => {
      setupMocks(vi.fn().mockResolvedValue({}), Role.VIEWER)
      render(<CreateOrgTemplatePage orgName="test-org" />)

      expect(screen.getByLabelText(/display name/i)).toBeDisabled()
      expect(screen.getByLabelText(/name slug/i)).toBeDisabled()
      expect(screen.getByLabelText(/description/i)).toBeDisabled()
      expect(screen.getByRole('textbox', { name: /cue template/i })).toBeDisabled()
      expect(screen.getByRole('switch', { name: /enabled/i })).toBeDisabled()
      expect(screen.getByRole('button', { name: /^create$/i })).toBeDisabled()
    })

    it('enables form fields for OWNER users', () => {
      setupMocks(vi.fn().mockResolvedValue({}), Role.OWNER)
      render(<CreateOrgTemplatePage orgName="test-org" />)

      expect(screen.getByLabelText(/display name/i)).not.toBeDisabled()
      expect(screen.getByLabelText(/name slug/i)).not.toBeDisabled()
      expect(screen.getByLabelText(/description/i)).not.toBeDisabled()
      expect(screen.getByRole('textbox', { name: /cue template/i })).not.toBeDisabled()
      expect(screen.getByRole('button', { name: /^create$/i })).not.toBeDisabled()
    })
  })
})

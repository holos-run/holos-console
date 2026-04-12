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
      useParams: () => ({ folderName: 'test-folder' }),
    }),
    useNavigate: () => mockNavigate,
    Link: ({ children, className, to, params }: { children: React.ReactNode; className?: string; to?: string; params?: Record<string, string> }) => (
      <a href={to} data-params={JSON.stringify(params)} className={className}>{children}</a>
    ),
  }
})

vi.mock('@/queries/templates', () => ({
  useCreateTemplate: vi.fn(),
  makeFolderScope: vi.fn().mockReturnValue({ scope: 2, scopeName: 'test-folder' }),
}))

vi.mock('@/queries/folders', () => ({
  useGetFolder: vi.fn(),
}))

import { useCreateTemplate } from '@/queries/templates'
import { useGetFolder } from '@/queries/folders'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { CreateFolderTemplatePage } from './new'

function setupMocks(
  mutateAsync = vi.fn().mockResolvedValue({}),
  userRole = Role.OWNER,
) {
  ;(useCreateTemplate as Mock).mockReturnValue({
    mutateAsync,
    isPending: false,
    reset: vi.fn(),
  })
  ;(useGetFolder as Mock).mockReturnValue({
    data: { name: 'test-folder', organization: 'test-org', userRole },
    isPending: false,
    error: null,
  })
}

describe('CreateFolderTemplatePage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setupMocks()
  })

  it('renders the page heading', () => {
    render(<CreateFolderTemplatePage folderName="test-folder" />)
    expect(screen.getByText(/create platform template/i)).toBeInTheDocument()
  })

  it('renders Display Name field', () => {
    render(<CreateFolderTemplatePage folderName="test-folder" />)
    expect(screen.getByLabelText(/display name/i)).toBeInTheDocument()
  })

  it('renders Name (slug) field', () => {
    render(<CreateFolderTemplatePage folderName="test-folder" />)
    expect(screen.getByLabelText(/name slug/i)).toBeInTheDocument()
  })

  it('renders Description field', () => {
    render(<CreateFolderTemplatePage folderName="test-folder" />)
    expect(screen.getByLabelText(/description/i)).toBeInTheDocument()
  })

  it('renders CUE Template textarea', () => {
    render(<CreateFolderTemplatePage folderName="test-folder" />)
    expect(screen.getByRole('textbox', { name: /cue template/i })).toBeInTheDocument()
  })

  it('renders Enabled switch defaulting to unchecked', () => {
    render(<CreateFolderTemplatePage folderName="test-folder" />)
    const toggle = screen.getByRole('switch', { name: /enabled/i })
    expect(toggle).toBeInTheDocument()
    expect(toggle).toHaveAttribute('data-state', 'unchecked')
  })

  it('renders Create submit button', () => {
    render(<CreateFolderTemplatePage folderName="test-folder" />)
    expect(screen.getByRole('button', { name: /^create$/i })).toBeInTheDocument()
  })

  it('renders a Cancel link back to the templates list', () => {
    render(<CreateFolderTemplatePage folderName="test-folder" />)
    expect(screen.getByRole('link', { name: /cancel/i })).toBeInTheDocument()
  })

  it('auto-derives slug from display name', () => {
    render(<CreateFolderTemplatePage folderName="test-folder" />)
    const displayNameInput = screen.getByLabelText(/display name/i)
    fireEvent.change(displayNameInput, { target: { value: 'My Web App' } })
    const slugInput = screen.getByLabelText(/name slug/i) as HTMLInputElement
    expect(slugInput.value).toBe('my-web-app')
  })

  it('shows validation error when name is empty', async () => {
    render(<CreateFolderTemplatePage folderName="test-folder" />)
    fireEvent.click(screen.getByRole('button', { name: /^create$/i }))
    await waitFor(() => {
      expect(screen.getByText(/template name is required/i)).toBeInTheDocument()
    })
  })

  it('calls createMutation with form values on submit', async () => {
    const mutateAsync = vi.fn().mockResolvedValue({})
    setupMocks(mutateAsync)
    render(<CreateFolderTemplatePage folderName="test-folder" />)

    fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'My Template' } })
    fireEvent.change(screen.getByLabelText(/description/i), { target: { value: 'A description' } })
    fireEvent.click(screen.getByRole('button', { name: /^create$/i }))

    await waitFor(() => {
      expect(mutateAsync).toHaveBeenCalledWith(
        expect.objectContaining({
          name: 'my-template',
          displayName: 'My Template',
          description: 'A description',
          mandatory: false,
          enabled: false,
        }),
      )
    })
  })

  it('navigates to template detail page after successful creation', async () => {
    const mutateAsync = vi.fn().mockResolvedValue({})
    setupMocks(mutateAsync)
    render(<CreateFolderTemplatePage folderName="test-folder" />)

    fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'My Template' } })
    fireEvent.click(screen.getByRole('button', { name: /^create$/i }))

    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith(
        expect.objectContaining({
          to: '/folders/$folderName/templates/$templateName',
          params: expect.objectContaining({ folderName: 'test-folder', templateName: 'my-template' }),
        }),
      )
    })
  })

  it('shows error message when creation fails', async () => {
    const mutateAsync = vi.fn().mockRejectedValue(new Error('server error'))
    setupMocks(mutateAsync)
    render(<CreateFolderTemplatePage folderName="test-folder" />)

    fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'My Template' } })
    fireEvent.click(screen.getByRole('button', { name: /^create$/i }))

    await waitFor(() => {
      expect(screen.getByText(/server error/i)).toBeInTheDocument()
    })
  })

  it('renders breadcrumb navigation', () => {
    render(<CreateFolderTemplatePage folderName="test-folder" />)
    expect(screen.getByText('Platform Templates')).toBeInTheDocument()
    expect(screen.getByText('test-folder')).toBeInTheDocument()
  })

  describe('Load Example button', () => {
    it('renders Load Example button', () => {
      render(<CreateFolderTemplatePage folderName="test-folder" />)
      expect(screen.getByRole('button', { name: /load example/i })).toBeInTheDocument()
    })

    it('clicking Load Example populates all form fields', () => {
      render(<CreateFolderTemplatePage folderName="test-folder" />)
      fireEvent.click(screen.getByRole('button', { name: /load example/i }))

      const nameInput = screen.getByLabelText(/name slug/i) as HTMLInputElement
      expect(nameInput.value).toBe('httproute-ingress')

      const displayNameInput = screen.getByLabelText(/display name/i) as HTMLInputElement
      expect(displayNameInput.value).toBe('HTTPRoute Ingress')

      const descriptionInput = screen.getByLabelText(/description/i) as HTMLInputElement
      expect(descriptionInput.value).toContain('HTTPRoute')

      const cueEditor = screen.getByRole('textbox', { name: /cue template/i }) as HTMLTextAreaElement
      expect(cueEditor.value).toContain('HTTPRoute')
      expect(cueEditor.value).toContain('istio-ingress')
    })
  })

  describe('Enabled switch', () => {
    it('passes enabled: true when toggle is switched on', async () => {
      const mutateAsync = vi.fn().mockResolvedValue({})
      setupMocks(mutateAsync)
      render(<CreateFolderTemplatePage folderName="test-folder" />)

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
      render(<CreateFolderTemplatePage folderName="test-folder" />)

      expect(screen.getByLabelText(/display name/i)).toBeDisabled()
      expect(screen.getByLabelText(/name slug/i)).toBeDisabled()
      expect(screen.getByLabelText(/description/i)).toBeDisabled()
      expect(screen.getByRole('textbox', { name: /cue template/i })).toBeDisabled()
      expect(screen.getByRole('switch', { name: /enabled/i })).toBeDisabled()
      expect(screen.getByRole('button', { name: /^create$/i })).toBeDisabled()
    })

    it('enables form fields for OWNER users', () => {
      setupMocks(vi.fn().mockResolvedValue({}), Role.OWNER)
      render(<CreateFolderTemplatePage folderName="test-folder" />)

      expect(screen.getByLabelText(/display name/i)).not.toBeDisabled()
      expect(screen.getByLabelText(/name slug/i)).not.toBeDisabled()
      expect(screen.getByLabelText(/description/i)).not.toBeDisabled()
      expect(screen.getByRole('textbox', { name: /cue template/i })).not.toBeDisabled()
      expect(screen.getByRole('button', { name: /^create$/i })).not.toBeDisabled()
    })
  })
})

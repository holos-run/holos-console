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
      useParams: () => ({ projectName: 'test-project' }),
    }),
    useNavigate: () => mockNavigate,
    Link: ({ children, className, to, params }: { children: React.ReactNode; className?: string; to?: string; params?: Record<string, string> }) => (
      <a href={to} data-params={JSON.stringify(params)} className={className}>{children}</a>
    ),
  }
})

vi.mock('@/queries/deployment-templates', () => ({
  useCreateDeploymentTemplate: vi.fn(),
  useRenderDeploymentTemplate: vi.fn(),
  useListLinkableOrgTemplates: vi.fn().mockReturnValue({ data: [] }),
}))

vi.mock('@/hooks/use-debounced-value', () => ({
  useDebouncedValue: vi.fn((value: unknown) => value),
}))

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

import { useCreateDeploymentTemplate, useRenderDeploymentTemplate, useListLinkableOrgTemplates } from '@/queries/deployment-templates'
import { CreateTemplatePage } from './new'

function setupMocks(
  mutateAsync = vi.fn().mockResolvedValue({}),
  renderData?: { renderedJson?: string },
  renderError?: Error,
) {
  ;(useCreateDeploymentTemplate as Mock).mockReturnValue({
    mutateAsync,
    isPending: false,
    reset: vi.fn(),
  })
  ;(useRenderDeploymentTemplate as Mock).mockReturnValue({
    data: renderData ?? undefined,
    error: renderError ?? null,
    isLoading: false,
    isError: !!renderError,
  })
}

describe('CreateTemplatePage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setupMocks()
  })

  it('renders the page heading', () => {
    render(<CreateTemplatePage />)
    expect(screen.getByText(/create deployment template/i)).toBeInTheDocument()
  })

  it('renders Display Name field', () => {
    render(<CreateTemplatePage />)
    expect(screen.getByLabelText(/display name/i)).toBeInTheDocument()
  })

  it('renders Name (slug) field', () => {
    render(<CreateTemplatePage />)
    expect(screen.getByLabelText(/name slug/i)).toBeInTheDocument()
  })

  it('renders Description field', () => {
    render(<CreateTemplatePage />)
    expect(screen.getByLabelText(/description/i)).toBeInTheDocument()
  })

  it('renders CUE Template textarea', () => {
    render(<CreateTemplatePage />)
    expect(screen.getByRole('textbox', { name: /cue template/i })).toBeInTheDocument()
  })

  it('renders Create Template submit button', () => {
    render(<CreateTemplatePage />)
    expect(screen.getByRole('button', { name: /create template/i })).toBeInTheDocument()
  })

  it('renders a Cancel link back to the templates list', () => {
    render(<CreateTemplatePage />)
    expect(screen.getByRole('link', { name: /cancel/i })).toBeInTheDocument()
  })

  it('auto-derives slug from display name', () => {
    render(<CreateTemplatePage />)
    const displayNameInput = screen.getByLabelText(/display name/i)
    fireEvent.change(displayNameInput, { target: { value: 'My Web App' } })
    const slugInput = screen.getByLabelText(/name slug/i) as HTMLInputElement
    expect(slugInput.value).toBe('my-web-app')
  })

  it('shows validation error when name is empty', async () => {
    render(<CreateTemplatePage />)
    fireEvent.click(screen.getByRole('button', { name: /create template/i }))
    await waitFor(() => {
      expect(screen.getByText(/template name is required/i)).toBeInTheDocument()
    })
  })

  it('calls createMutation with form values on submit', async () => {
    const mutateAsync = vi.fn().mockResolvedValue({})
    setupMocks(mutateAsync)
    render(<CreateTemplatePage />)

    fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'My Template' } })
    fireEvent.change(screen.getByLabelText(/description/i), { target: { value: 'A description' } })
    fireEvent.click(screen.getByRole('button', { name: /create template/i }))

    await waitFor(() => {
      expect(mutateAsync).toHaveBeenCalledWith(
        expect.objectContaining({
          name: 'my-template',
          displayName: 'My Template',
          description: 'A description',
        }),
      )
    })
  })

  it('navigates to template detail page after successful creation', async () => {
    const mutateAsync = vi.fn().mockResolvedValue({})
    setupMocks(mutateAsync)
    render(<CreateTemplatePage />)

    fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'My Template' } })
    fireEvent.click(screen.getByRole('button', { name: /create template/i }))

    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith(
        expect.objectContaining({
          to: '/projects/$projectName/templates/$templateName',
          params: expect.objectContaining({ templateName: 'my-template' }),
        }),
      )
    })
  })

  it('shows error message when creation fails', async () => {
    const mutateAsync = vi.fn().mockRejectedValue(new Error('server error'))
    setupMocks(mutateAsync)
    render(<CreateTemplatePage />)

    fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'My Template' } })
    fireEvent.click(screen.getByRole('button', { name: /create template/i }))

    await waitFor(() => {
      expect(screen.getByText(/server error/i)).toBeInTheDocument()
    })
  })

  it('renders Preview toggle button', () => {
    render(<CreateTemplatePage />)
    expect(screen.getByRole('button', { name: /preview/i })).toBeInTheDocument()
  })

  it('shows rendered JSON when preview is toggled and data is available', async () => {
    setupMocks(
      vi.fn().mockResolvedValue({}),
      { renderedJson: '{"apiVersion":"apps/v1"}' },
    )
    render(<CreateTemplatePage />)
    fireEvent.click(screen.getByRole('button', { name: /preview/i }))
    await waitFor(() => {
      expect(screen.getByText(/"apiVersion":"apps\/v1"/)).toBeInTheDocument()
    })
  })

  it('shows CUE template has default content', () => {
    render(<CreateTemplatePage />)
    const cueEditor = screen.getByRole('textbox', { name: /cue template/i }) as HTMLTextAreaElement
    expect(cueEditor.value).toContain('projectResources')
  })

  it('useRenderDeploymentTemplate is called with platform input including claims', () => {
    render(<CreateTemplatePage projectName="test-project" />)
    const calls = (useRenderDeploymentTemplate as Mock).mock.calls
    expect(calls.length).toBeGreaterThan(0)
    // 4th arg is cuePlatformInput
    const platformInput = calls[0][3]
    expect(platformInput).toContain('platform:')
    expect(platformInput).toContain('claims')
    expect(platformInput).toContain('email')
  })

  it('useRenderDeploymentTemplate is called with user input (not project/namespace)', () => {
    render(<CreateTemplatePage projectName="test-project" />)
    const calls = (useRenderDeploymentTemplate as Mock).mock.calls
    expect(calls.length).toBeGreaterThan(0)
    // 2nd arg is cueInput (user input)
    const userInput = calls[0][1]
    expect(userInput).toContain('input:')
    expect(userInput).not.toContain('project:')
    expect(userInput).not.toContain('namespace:')
  })

  describe('Load httpbin Example button', () => {
    it('renders Load httpbin Example button', () => {
      render(<CreateTemplatePage />)
      expect(screen.getByRole('button', { name: /load httpbin example/i })).toBeInTheDocument()
    })

    it('clicking Load httpbin Example changes the CUE textarea content', () => {
      render(<CreateTemplatePage />)
      const cueEditor = screen.getByRole('textbox', { name: /cue template/i }) as HTMLTextAreaElement
      const initialContent = cueEditor.value
      fireEvent.click(screen.getByRole('button', { name: /load httpbin example/i }))
      expect(cueEditor.value).not.toBe(initialContent)
      expect(cueEditor.value).toContain('go-httpbin')
    })

    it('httpbin example CUE contains ServiceAccount, Deployment, and Service', () => {
      render(<CreateTemplatePage />)
      fireEvent.click(screen.getByRole('button', { name: /load httpbin example/i }))
      const cueEditor = screen.getByRole('textbox', { name: /cue template/i }) as HTMLTextAreaElement
      expect(cueEditor.value).toContain('ServiceAccount')
      expect(cueEditor.value).toContain('Deployment')
      expect(cueEditor.value).toContain('Service')
    })
  })

  describe('linked platform templates', () => {
    const mockLinkable = [
      { name: 'httproute', displayName: 'HTTPRoute Gateway', description: 'Adds an HTTPRoute', mandatory: false },
      { name: 'mandatory-labels', displayName: 'Mandatory Labels', description: 'Enforces labels', mandatory: true },
    ]

    it('does not show linked templates section when no linkable templates exist', () => {
      ;(useListLinkableOrgTemplates as Mock).mockReturnValue({ data: [] })
      render(<CreateTemplatePage />)
      expect(screen.queryByLabelText(/linked platform templates/i)).not.toBeInTheDocument()
    })

    it('shows linked templates section when linkable templates exist', () => {
      ;(useListLinkableOrgTemplates as Mock).mockReturnValue({ data: mockLinkable })
      render(<CreateTemplatePage />)
      expect(screen.getByLabelText(/linked platform templates/i)).toBeInTheDocument()
    })

    it('renders each linkable template as a checkbox', () => {
      ;(useListLinkableOrgTemplates as Mock).mockReturnValue({ data: mockLinkable })
      render(<CreateTemplatePage />)
      expect(screen.getByRole('checkbox', { name: /httproute gateway/i })).toBeInTheDocument()
      expect(screen.getByRole('checkbox', { name: /mandatory labels/i })).toBeInTheDocument()
    })

    it('mandatory template checkbox is checked and disabled', () => {
      ;(useListLinkableOrgTemplates as Mock).mockReturnValue({ data: mockLinkable })
      render(<CreateTemplatePage />)
      const mandatoryCheckbox = screen.getByRole('checkbox', { name: /mandatory labels/i })
      expect(mandatoryCheckbox).toBeChecked()
      expect(mandatoryCheckbox).toBeDisabled()
    })

    it('non-mandatory template checkbox is unchecked and enabled by default', () => {
      ;(useListLinkableOrgTemplates as Mock).mockReturnValue({ data: mockLinkable })
      render(<CreateTemplatePage />)
      const checkbox = screen.getByRole('checkbox', { name: /httproute gateway/i })
      expect(checkbox).not.toBeChecked()
      expect(checkbox).not.toBeDisabled()
    })

    it('checking a non-mandatory template includes it in the create mutation', async () => {
      const mutateAsync = vi.fn().mockResolvedValue({})
      setupMocks(mutateAsync)
      ;(useListLinkableOrgTemplates as Mock).mockReturnValue({ data: mockLinkable })
      render(<CreateTemplatePage />)

      // Fill in required name field
      fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'My Template' } })

      // Check the non-mandatory template
      fireEvent.click(screen.getByRole('checkbox', { name: /httproute gateway/i }))

      fireEvent.click(screen.getByRole('button', { name: /create template/i }))

      await waitFor(() => {
        expect(mutateAsync).toHaveBeenCalledWith(
          expect.objectContaining({
            linkedOrgTemplates: ['httproute'],
          }),
        )
      })
    })

    it('unchecked template is not included in the create mutation', async () => {
      const mutateAsync = vi.fn().mockResolvedValue({})
      setupMocks(mutateAsync)
      ;(useListLinkableOrgTemplates as Mock).mockReturnValue({ data: mockLinkable })
      render(<CreateTemplatePage />)

      fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'My Template' } })
      fireEvent.click(screen.getByRole('button', { name: /create template/i }))

      await waitFor(() => {
        expect(mutateAsync).toHaveBeenCalledWith(
          expect.objectContaining({
            linkedOrgTemplates: [],
          }),
        )
      })
    })
  })
})

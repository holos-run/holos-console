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
}))

vi.mock('@/hooks/use-debounced-value', () => ({
  useDebouncedValue: vi.fn((value: unknown) => value),
}))

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

import { useCreateDeploymentTemplate, useRenderDeploymentTemplate } from '@/queries/deployment-templates'
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
    // 4th arg is cueSystemInput
    const systemInput = calls[0][3]
    expect(systemInput).toContain('platform:')
    expect(systemInput).toContain('claims')
    expect(systemInput).toContain('email')
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
})

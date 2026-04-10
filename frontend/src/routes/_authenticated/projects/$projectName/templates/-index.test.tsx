import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({ useParams: () => ({ projectName: 'test-project' }) }),
    Link: ({ children, className, to, params }: { children: React.ReactNode; className?: string; to?: string; params?: Record<string, string> }) => (
      <a href={to} data-params={JSON.stringify(params)} className={className}>{children}</a>
    ),
    useNavigate: () => vi.fn(),
  }
})

vi.mock('@/queries/templates', () => ({
  useListTemplates: vi.fn(),
  useDeleteTemplate: vi.fn(),
  useCloneTemplate: vi.fn(),
  makeProjectScope: vi.fn().mockReturnValue({ scope: 1, scopeName: 'test-project' }),
}))

vi.mock('@/queries/projects', () => ({
  useGetProject: vi.fn(),
}))

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

import { useListTemplates, useDeleteTemplate, useCloneTemplate } from '@/queries/templates'
import { useGetProject } from '@/queries/projects'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { DeploymentTemplatesPage } from './index'

function makeTemplate(name: string, description = '', displayName = '') {
  return { name, project: 'test-project', displayName, description, cueTemplate: '' }
}

function setupMocks(templates = [makeTemplate('web-app', 'Standard web app')], userRole = Role.OWNER) {
  ;(useListTemplates as Mock).mockReturnValue({ data: templates, isLoading: false, error: null })
  ;(useDeleteTemplate as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({}), isPending: false, error: null, reset: vi.fn() })
  ;(useCloneTemplate as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({ name: 'new-template' }), isPending: false })
  ;(useGetProject as Mock).mockReturnValue({ data: { name: 'test-project', userRole }, isLoading: false })
}

describe('DeploymentTemplatesPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders the templates list with template names', () => {
    setupMocks([makeTemplate('web-app', 'Standard web app'), makeTemplate('worker', 'Background worker')])
    render(<DeploymentTemplatesPage />)
    expect(screen.getByText('web-app')).toBeInTheDocument()
    expect(screen.getByText('worker')).toBeInTheDocument()
  })

  it('renders description text', () => {
    setupMocks([makeTemplate('web-app', 'Standard web application')])
    render(<DeploymentTemplatesPage />)
    expect(screen.getByText('Standard web application')).toBeInTheDocument()
  })

  it('shows empty state when no templates exist', () => {
    setupMocks([])
    render(<DeploymentTemplatesPage />)
    expect(screen.getByText(/no deployment templates/i)).toBeInTheDocument()
  })

  it('renders Create Template button for owners', () => {
    setupMocks([], Role.OWNER)
    render(<DeploymentTemplatesPage />)
    expect(screen.getAllByRole('button', { name: /create template/i }).length).toBeGreaterThan(0)
  })

  it('renders Create Template button for editors', () => {
    setupMocks([], Role.EDITOR)
    render(<DeploymentTemplatesPage />)
    expect(screen.getAllByRole('button', { name: /create template/i }).length).toBeGreaterThan(0)
  })

  it('does not render Create Template button for viewers', () => {
    setupMocks([], Role.VIEWER)
    render(<DeploymentTemplatesPage />)
    expect(screen.queryByRole('button', { name: /create template/i })).not.toBeInTheDocument()
  })

  it('Create Template button is a link to the new page', () => {
    setupMocks([], Role.OWNER)
    render(<DeploymentTemplatesPage />)
    const links = screen.getAllByRole('link')
    const createLinks = links.filter((l) => l.getAttribute('href')?.includes('/templates/new'))
    expect(createLinks.length).toBeGreaterThan(0)
  })

  it('renders delete buttons for each template (owner)', () => {
    setupMocks([makeTemplate('web-app'), makeTemplate('worker')], Role.OWNER)
    render(<DeploymentTemplatesPage />)
    const deleteButtons = screen.getAllByRole('button', { name: /delete/i })
    expect(deleteButtons.length).toBeGreaterThanOrEqual(2)
  })

  it('does not render delete buttons for viewers', () => {
    setupMocks([makeTemplate('web-app')], Role.VIEWER)
    render(<DeploymentTemplatesPage />)
    expect(screen.queryByRole('button', { name: /delete/i })).not.toBeInTheDocument()
  })

  it('empty state Create Template link points to new page', () => {
    setupMocks([], Role.OWNER)
    render(<DeploymentTemplatesPage />)
    const links = screen.getAllByRole('link')
    const createLinks = links.filter((l) => l.getAttribute('href')?.includes('/templates/new'))
    expect(createLinks.length).toBeGreaterThanOrEqual(2) // header + empty state
  })

  it('shows error state when fetch fails', () => {
    ;(useListTemplates as Mock).mockReturnValue({ data: undefined, isLoading: false, error: new Error('fetch failed') })
    ;(useDeleteTemplate as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false, error: null, reset: vi.fn() })
    ;(useCloneTemplate as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false })
    ;(useGetProject as Mock).mockReturnValue({ data: { name: 'test-project', userRole: Role.OWNER }, isLoading: false })
    render(<DeploymentTemplatesPage />)
    expect(screen.getByText(/fetch failed/)).toBeInTheDocument()
  })

  describe('clone action', () => {
    it('renders clone buttons for templates (owner)', () => {
      setupMocks([makeTemplate('web-app'), makeTemplate('worker')], Role.OWNER)
      render(<DeploymentTemplatesPage />)
      const cloneButtons = screen.getAllByRole('button', { name: /clone/i })
      expect(cloneButtons.length).toBeGreaterThanOrEqual(2)
    })

    it('renders clone buttons for templates (editor)', () => {
      setupMocks([makeTemplate('web-app')], Role.EDITOR)
      render(<DeploymentTemplatesPage />)
      expect(screen.getByRole('button', { name: /clone web-app/i })).toBeInTheDocument()
    })

    it('renders clone buttons for templates (viewer)', () => {
      setupMocks([makeTemplate('web-app')], Role.VIEWER)
      render(<DeploymentTemplatesPage />)
      expect(screen.getByRole('button', { name: /clone web-app/i })).toBeInTheDocument()
    })

    it('clicking clone opens dialog', async () => {
      setupMocks([makeTemplate('web-app', 'Standard web app')], Role.OWNER)
      const user = userEvent.setup()
      render(<DeploymentTemplatesPage />)
      await user.click(screen.getByRole('button', { name: /clone web-app/i }))
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })

    it('confirming clone calls cloneTemplate', async () => {
      setupMocks([makeTemplate('web-app', 'Standard web app')], Role.OWNER)
      const user = userEvent.setup()
      render(<DeploymentTemplatesPage />)
      await user.click(screen.getByRole('button', { name: /clone web-app/i }))
      const nameInput = screen.getByRole('textbox', { name: /^name$/i })
      await user.clear(nameInput)
      await user.type(nameInput, 'web-app-copy')
      const displayNameInput = screen.getByRole('textbox', { name: /display name/i })
      await user.clear(displayNameInput)
      await user.type(displayNameInput, 'Web App Copy')
      await user.click(screen.getByRole('button', { name: /^clone$/i }))
      const mutateAsync = (useCloneTemplate as Mock).mock.results[0].value.mutateAsync
      await waitFor(() => {
        expect(mutateAsync).toHaveBeenCalledWith(expect.objectContaining({
          sourceName: 'web-app',
          name: 'web-app-copy',
          displayName: 'Web App Copy',
        }))
      })
    })

    it('cancel closes clone dialog without saving', async () => {
      setupMocks([makeTemplate('web-app', 'Standard web app')], Role.OWNER)
      const user = userEvent.setup()
      render(<DeploymentTemplatesPage />)
      await user.click(screen.getByRole('button', { name: /clone web-app/i }))
      await user.click(screen.getByRole('button', { name: /cancel/i }))
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
      const mutateAsync = (useCloneTemplate as Mock).mock.results[0].value.mutateAsync
      expect(mutateAsync).not.toHaveBeenCalled()
    })
  })
})

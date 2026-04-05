import { render, screen, fireEvent, waitFor } from '@testing-library/react'
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

vi.mock('@/queries/deployment-templates', () => ({
  useListDeploymentTemplates: vi.fn(),
  useCreateDeploymentTemplate: vi.fn(),
  useDeleteDeploymentTemplate: vi.fn(),
  useRenderDeploymentTemplate: vi.fn(),
}))

vi.mock('@/queries/projects', () => ({
  useGetProject: vi.fn(),
}))

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

import { useListDeploymentTemplates, useCreateDeploymentTemplate, useDeleteDeploymentTemplate, useRenderDeploymentTemplate } from '@/queries/deployment-templates'
import { useGetProject } from '@/queries/projects'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { DeploymentTemplatesPage } from './index'

function makeTemplate(name: string, description = '', displayName = '') {
  return { name, project: 'test-project', displayName, description, cueTemplate: '' }
}

function setupMocks(templates = [makeTemplate('web-app', 'Standard web app')], userRole = Role.OWNER) {
  ;(useListDeploymentTemplates as Mock).mockReturnValue({ data: templates, isLoading: false, error: null })
  ;(useCreateDeploymentTemplate as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({}), isPending: false, reset: vi.fn() })
  ;(useDeleteDeploymentTemplate as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({}), isPending: false, error: null, reset: vi.fn() })
  ;(useGetProject as Mock).mockReturnValue({ data: { name: 'test-project', userRole }, isLoading: false })
  ;(useRenderDeploymentTemplate as Mock).mockReturnValue({ data: undefined, isLoading: false, isError: false, error: null })
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

  it('opens create dialog when Create Template button is clicked', () => {
    setupMocks([], Role.OWNER)
    render(<DeploymentTemplatesPage />)
    fireEvent.click(screen.getAllByRole('button', { name: /create template/i })[0])
    expect(screen.getByRole('dialog')).toBeInTheDocument()
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

  it('creates a template when form is submitted', async () => {
    setupMocks([], Role.OWNER)
    render(<DeploymentTemplatesPage />)
    fireEvent.click(screen.getAllByRole('button', { name: /create template/i })[0])

    fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'My Template' } })
    fireEvent.change(screen.getByLabelText(/description/i), { target: { value: 'A description' } })

    fireEvent.click(screen.getByRole('button', { name: /^create$/i }))

    const mutateAsync = (useCreateDeploymentTemplate as Mock).mock.results[0].value.mutateAsync
    await waitFor(() => {
      expect(mutateAsync).toHaveBeenCalledWith(
        expect.objectContaining({ displayName: 'My Template', description: 'A description' }),
      )
    })
  })

  it('shows error state when fetch fails', () => {
    ;(useListDeploymentTemplates as Mock).mockReturnValue({ data: undefined, isLoading: false, error: new Error('fetch failed') })
    ;(useCreateDeploymentTemplate as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false, reset: vi.fn() })
    ;(useDeleteDeploymentTemplate as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false, error: null, reset: vi.fn() })
    ;(useGetProject as Mock).mockReturnValue({ data: { name: 'test-project', userRole: Role.OWNER }, isLoading: false })
    render(<DeploymentTemplatesPage />)
    expect(screen.getByText(/fetch failed/)).toBeInTheDocument()
  })
})

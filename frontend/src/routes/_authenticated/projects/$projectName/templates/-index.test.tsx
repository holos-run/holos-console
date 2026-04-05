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
  useDeleteDeploymentTemplate: vi.fn(),
}))

vi.mock('@/queries/projects', () => ({
  useGetProject: vi.fn(),
}))

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

import { useListDeploymentTemplates, useDeleteDeploymentTemplate } from '@/queries/deployment-templates'
import { useGetProject } from '@/queries/projects'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { DeploymentTemplatesPage } from './index'

function makeTemplate(name: string, description = '', displayName = '') {
  return { name, project: 'test-project', displayName, description, cueTemplate: '' }
}

function setupMocks(templates = [makeTemplate('web-app', 'Standard web app')], userRole = Role.OWNER) {
  ;(useListDeploymentTemplates as Mock).mockReturnValue({ data: templates, isLoading: false, error: null })
  ;(useDeleteDeploymentTemplate as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({}), isPending: false, error: null, reset: vi.fn() })
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
    ;(useListDeploymentTemplates as Mock).mockReturnValue({ data: undefined, isLoading: false, error: new Error('fetch failed') })
    ;(useDeleteDeploymentTemplate as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false, error: null, reset: vi.fn() })
    ;(useGetProject as Mock).mockReturnValue({ data: { name: 'test-project', userRole: Role.OWNER }, isLoading: false })
    render(<DeploymentTemplatesPage />)
    expect(screen.getByText(/fetch failed/)).toBeInTheDocument()
  })
})

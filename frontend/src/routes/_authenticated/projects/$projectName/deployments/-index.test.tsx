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

vi.mock('@/queries/deployments', () => ({
  useListDeployments: vi.fn(),
  useCreateDeployment: vi.fn(),
  useDeleteDeployment: vi.fn(),
}))

vi.mock('@/queries/deployment-templates', () => ({
  useListDeploymentTemplates: vi.fn(),
}))

vi.mock('@/queries/projects', () => ({
  useGetProject: vi.fn(),
}))

vi.mock('@/components/ui/select', () => ({
  Select: ({ onValueChange, children }: { onValueChange?: (v: string) => void; children: React.ReactNode }) => (
    <div data-testid="select-root">
      <select data-testid="template-select" onChange={(e) => onValueChange?.(e.target.value)}>
        {children}
      </select>
    </div>
  ),
  SelectTrigger: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SelectContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SelectItem: ({ value, children }: { value: string; children: React.ReactNode }) => (
    <option value={value}>{children}</option>
  ),
  SelectValue: ({ placeholder }: { placeholder?: string }) => <span>{placeholder}</span>,
}))

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

import { useListDeployments, useCreateDeployment, useDeleteDeployment } from '@/queries/deployments'
import { useListDeploymentTemplates } from '@/queries/deployment-templates'
import { useGetProject } from '@/queries/projects'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { DeploymentPhase } from '@/gen/holos/console/v1/deployments_pb'
import { DeploymentsPage } from './index'

function makeDeployment(name: string, image = 'ghcr.io/org/app', tag = 'v1.0.0', phase = DeploymentPhase.RUNNING) {
  return { name, project: 'test-project', image, tag, template: 'web-app', displayName: '', description: '', phase, message: '' }
}

function makeTemplate(name: string) {
  return { name, project: 'test-project', displayName: '', description: '', cueTemplate: '' }
}

function setupMocks(
  deployments = [makeDeployment('api'), makeDeployment('worker', 'ghcr.io/org/wrk', 'latest', DeploymentPhase.PENDING)],
  userRole = Role.OWNER,
  templates = [makeTemplate('web-app'), makeTemplate('worker-tmpl')],
) {
  ;(useListDeployments as Mock).mockReturnValue({ data: deployments, isLoading: false, error: null })
  ;(useCreateDeployment as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({ name: 'api' }), isPending: false, reset: vi.fn() })
  ;(useDeleteDeployment as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({}), isPending: false, error: null, reset: vi.fn() })
  ;(useListDeploymentTemplates as Mock).mockReturnValue({ data: templates, isLoading: false })
  ;(useGetProject as Mock).mockReturnValue({ data: { name: 'test-project', userRole }, isLoading: false })
}

describe('DeploymentsPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders deployment names', () => {
    setupMocks()
    render(<DeploymentsPage />)
    expect(screen.getByText('api')).toBeInTheDocument()
    expect(screen.getByText('worker')).toBeInTheDocument()
  })

  it('renders image and tag for each deployment', () => {
    setupMocks()
    render(<DeploymentsPage />)
    expect(screen.getByText('ghcr.io/org/app')).toBeInTheDocument()
    expect(screen.getByText('v1.0.0')).toBeInTheDocument()
    expect(screen.getByText('latest')).toBeInTheDocument()
  })

  it('renders Running status badge', () => {
    setupMocks([makeDeployment('api', 'ghcr.io/org/app', 'v1.0.0', DeploymentPhase.RUNNING)])
    render(<DeploymentsPage />)
    expect(screen.getByText(/running/i)).toBeInTheDocument()
  })

  it('renders Pending status badge', () => {
    setupMocks([makeDeployment('worker', 'ghcr.io/org/wrk', 'latest', DeploymentPhase.PENDING)])
    render(<DeploymentsPage />)
    expect(screen.getByText(/pending/i)).toBeInTheDocument()
  })

  it('renders Failed status badge', () => {
    setupMocks([makeDeployment('api', 'ghcr.io/org/app', 'v1.0.0', DeploymentPhase.FAILED)])
    render(<DeploymentsPage />)
    expect(screen.getByText(/failed/i)).toBeInTheDocument()
  })

  it('shows empty state when no deployments', () => {
    setupMocks([])
    render(<DeploymentsPage />)
    expect(screen.getByText(/no deployments yet/i)).toBeInTheDocument()
  })

  it('renders Create Deployment button for owners', () => {
    setupMocks([], Role.OWNER)
    render(<DeploymentsPage />)
    expect(screen.getAllByRole('button', { name: /create deployment/i }).length).toBeGreaterThan(0)
  })

  it('renders Create Deployment button for editors', () => {
    setupMocks([], Role.EDITOR)
    render(<DeploymentsPage />)
    expect(screen.getAllByRole('button', { name: /create deployment/i }).length).toBeGreaterThan(0)
  })

  it('does not render Create Deployment button for viewers', () => {
    setupMocks([], Role.VIEWER)
    render(<DeploymentsPage />)
    expect(screen.queryByRole('button', { name: /create deployment/i })).not.toBeInTheDocument()
  })

  it('opens create dialog when Create Deployment button is clicked', () => {
    setupMocks([], Role.OWNER)
    render(<DeploymentsPage />)
    fireEvent.click(screen.getAllByRole('button', { name: /create deployment/i })[0])
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  it('renders delete buttons for owners', () => {
    setupMocks([makeDeployment('api')], Role.OWNER)
    render(<DeploymentsPage />)
    expect(screen.getAllByRole('button', { name: /delete/i }).length).toBeGreaterThanOrEqual(1)
  })

  it('does not render delete buttons for viewers', () => {
    setupMocks([makeDeployment('api')], Role.VIEWER)
    render(<DeploymentsPage />)
    expect(screen.queryByRole('button', { name: /delete/i })).not.toBeInTheDocument()
  })

  it('deployment name links to detail page', () => {
    setupMocks([makeDeployment('api')])
    render(<DeploymentsPage />)
    const link = screen.getByRole('link', { name: 'api' })
    expect(link.getAttribute('href')).toContain('deployments')
  })

  it('shows error state when fetch fails', () => {
    ;(useListDeployments as Mock).mockReturnValue({ data: undefined, isLoading: false, error: new Error('fetch failed') })
    ;(useCreateDeployment as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false, reset: vi.fn() })
    ;(useDeleteDeployment as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false, error: null, reset: vi.fn() })
    ;(useListDeploymentTemplates as Mock).mockReturnValue({ data: [], isLoading: false })
    ;(useGetProject as Mock).mockReturnValue({ data: { name: 'test-project', userRole: Role.OWNER }, isLoading: false })
    render(<DeploymentsPage />)
    expect(screen.getByText(/fetch failed/)).toBeInTheDocument()
  })

  it('create dialog has template options', async () => {
    setupMocks([], Role.OWNER)
    render(<DeploymentsPage />)
    fireEvent.click(screen.getAllByRole('button', { name: /create deployment/i })[0])
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    expect(screen.getByText('web-app')).toBeInTheDocument()
    expect(screen.getByText('worker-tmpl')).toBeInTheDocument()
  })

  it('create dialog validates required name field', async () => {
    setupMocks([], Role.OWNER)
    render(<DeploymentsPage />)
    fireEvent.click(screen.getAllByRole('button', { name: /create deployment/i })[0])
    fireEvent.click(screen.getByRole('button', { name: /^create$/i }))
    await waitFor(() => {
      expect(screen.getByText(/name is required/i)).toBeInTheDocument()
    })
  })

  it('creates a deployment when form is submitted with valid data', async () => {
    setupMocks([], Role.OWNER)
    render(<DeploymentsPage />)
    fireEvent.click(screen.getAllByRole('button', { name: /create deployment/i })[0])

    fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'My API' } })
    fireEvent.change(screen.getByLabelText(/^image$/i), { target: { value: 'ghcr.io/org/api' } })
    fireEvent.change(screen.getByLabelText(/^tag$/i), { target: { value: 'v1.0.0' } })
    fireEvent.change(screen.getByTestId('template-select'), { target: { value: 'web-app' } })

    fireEvent.click(screen.getByRole('button', { name: /^create$/i }))

    const mutateAsync = (useCreateDeployment as Mock).mock.results[0].value.mutateAsync
    await waitFor(() => {
      expect(mutateAsync).toHaveBeenCalledWith(
        expect.objectContaining({ image: 'ghcr.io/org/api', tag: 'v1.0.0', template: 'web-app' }),
      )
    })
  })
})

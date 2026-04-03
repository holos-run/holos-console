import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => ({ projectName: 'test-project', deploymentName: 'api' }),
    }),
    Link: ({ children, className, to, params }: { children: React.ReactNode; className?: string; to?: string; params?: Record<string, string> }) => (
      <a href={to} data-params={JSON.stringify(params)} className={className}>{children}</a>
    ),
    useNavigate: () => vi.fn(),
  }
})

vi.mock('@/queries/deployments', () => ({
  useGetDeployment: vi.fn(),
  useGetDeploymentStatus: vi.fn(),
  useGetDeploymentLogs: vi.fn(),
  useUpdateDeployment: vi.fn(),
  useDeleteDeployment: vi.fn(),
}))

vi.mock('@/queries/projects', () => ({
  useGetProject: vi.fn(),
}))

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

import { useGetDeployment, useGetDeploymentStatus, useGetDeploymentLogs, useUpdateDeployment, useDeleteDeployment } from '@/queries/deployments'
import { useGetProject } from '@/queries/projects'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { DeploymentPhase } from '@/gen/holos/console/v1/deployments_pb'
import { DeploymentDetailPage } from './$deploymentName'

const mockDeployment = {
  name: 'api',
  project: 'test-project',
  image: 'ghcr.io/org/api',
  tag: 'v1.2.3',
  template: 'web-app',
  displayName: 'API Service',
  description: '',
  phase: DeploymentPhase.RUNNING,
  message: '',
}

const mockStatus = {
  readyReplicas: 1,
  desiredReplicas: 1,
  availableReplicas: 1,
  conditions: [
    { type: 'Available', status: 'True', reason: 'MinimumReplicasAvailable', message: '' },
    { type: 'Progressing', status: 'True', reason: 'NewReplicaSetAvailable', message: '' },
  ],
  pods: [
    { name: 'api-7d8f9b6c4-xk2m3', phase: 'Running', ready: true, restartCount: 0 },
  ],
}

const mockLogs = '2024-01-15T10:30:01Z Starting server...\n2024-01-15T10:30:02Z Listening on :8080'

function setupMocks(userRole = Role.OWNER) {
  ;(useGetDeployment as Mock).mockReturnValue({ data: mockDeployment, isPending: false, error: null })
  ;(useGetDeploymentStatus as Mock).mockReturnValue({ data: mockStatus, isPending: false, error: null })
  ;(useGetDeploymentLogs as Mock).mockReturnValue({ data: mockLogs, isPending: false, error: null })
  ;(useUpdateDeployment as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({}), isPending: false, reset: vi.fn() })
  ;(useDeleteDeployment as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({}), isPending: false, error: null, reset: vi.fn() })
  ;(useGetProject as Mock).mockReturnValue({ data: { name: 'test-project', userRole }, isLoading: false })
}

describe('DeploymentDetailPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders deployment name', () => {
    setupMocks()
    render(<DeploymentDetailPage />)
    // displayName 'API Service' is shown in the h2; deployment name 'api' appears in the breadcrumb
    expect(screen.getByText('API Service')).toBeInTheDocument()
  })

  it('renders image and tag', () => {
    setupMocks()
    render(<DeploymentDetailPage />)
    expect(screen.getByText('ghcr.io/org/api')).toBeInTheDocument()
    expect(screen.getByText('v1.2.3')).toBeInTheDocument()
  })

  it('renders replica status', () => {
    setupMocks()
    render(<DeploymentDetailPage />)
    expect(screen.getByText(/1\/1 ready/)).toBeInTheDocument()
  })

  it('renders deployment conditions', () => {
    setupMocks()
    render(<DeploymentDetailPage />)
    expect(screen.getByText('Available')).toBeInTheDocument()
    expect(screen.getByText('Progressing')).toBeInTheDocument()
  })

  it('renders pod name in status', () => {
    setupMocks()
    render(<DeploymentDetailPage />)
    expect(screen.getByText('api-7d8f9b6c4-xk2m3')).toBeInTheDocument()
  })

  it('renders log content', () => {
    setupMocks()
    render(<DeploymentDetailPage />)
    expect(screen.getByText(/Starting server/)).toBeInTheDocument()
  })

  it('renders Re-deploy button for owners', () => {
    setupMocks(Role.OWNER)
    render(<DeploymentDetailPage />)
    expect(screen.getByRole('button', { name: /re-?deploy/i })).toBeInTheDocument()
  })

  it('renders Re-deploy button for editors', () => {
    setupMocks(Role.EDITOR)
    render(<DeploymentDetailPage />)
    expect(screen.getByRole('button', { name: /re-?deploy/i })).toBeInTheDocument()
  })

  it('does not render Re-deploy button for viewers', () => {
    setupMocks(Role.VIEWER)
    render(<DeploymentDetailPage />)
    expect(screen.queryByRole('button', { name: /re-?deploy/i })).not.toBeInTheDocument()
  })

  it('renders Delete button for owners', () => {
    setupMocks(Role.OWNER)
    render(<DeploymentDetailPage />)
    expect(screen.getByRole('button', { name: /delete deployment/i })).toBeInTheDocument()
  })

  it('does not render Delete button for editors', () => {
    setupMocks(Role.EDITOR)
    render(<DeploymentDetailPage />)
    expect(screen.queryByRole('button', { name: /delete deployment/i })).not.toBeInTheDocument()
  })

  it('does not render Delete button for viewers', () => {
    setupMocks(Role.VIEWER)
    render(<DeploymentDetailPage />)
    expect(screen.queryByRole('button', { name: /delete deployment/i })).not.toBeInTheDocument()
  })

  it('clicking Re-deploy opens update dialog', () => {
    setupMocks(Role.OWNER)
    render(<DeploymentDetailPage />)
    fireEvent.click(screen.getByRole('button', { name: /re-?deploy/i }))
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  it('update dialog is pre-populated with current image and tag', () => {
    setupMocks(Role.OWNER)
    render(<DeploymentDetailPage />)
    fireEvent.click(screen.getByRole('button', { name: /re-?deploy/i }))
    expect(screen.getByDisplayValue('ghcr.io/org/api')).toBeInTheDocument()
    expect(screen.getByDisplayValue('v1.2.3')).toBeInTheDocument()
  })

  it('update dialog calls mutation on save', async () => {
    setupMocks(Role.OWNER)
    render(<DeploymentDetailPage />)
    fireEvent.click(screen.getByRole('button', { name: /re-?deploy/i }))
    const imageInput = screen.getByDisplayValue('ghcr.io/org/api')
    fireEvent.change(imageInput, { target: { value: 'ghcr.io/org/api' } })
    const tagInput = screen.getByDisplayValue('v1.2.3')
    fireEvent.change(tagInput, { target: { value: 'v2.0.0' } })
    fireEvent.click(screen.getByRole('button', { name: /^deploy$/i }))
    const mutateAsync = (useUpdateDeployment as Mock).mock.results[0].value.mutateAsync
    await waitFor(() => {
      expect(mutateAsync).toHaveBeenCalledWith(
        expect.objectContaining({ tag: 'v2.0.0' }),
      )
    })
  })

  it('clicking Delete opens confirmation dialog', () => {
    setupMocks(Role.OWNER)
    render(<DeploymentDetailPage />)
    fireEvent.click(screen.getByRole('button', { name: /delete deployment/i }))
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  it('confirming delete calls useDeleteDeployment', async () => {
    setupMocks(Role.OWNER)
    render(<DeploymentDetailPage />)
    fireEvent.click(screen.getByRole('button', { name: /delete deployment/i }))
    fireEvent.click(screen.getByRole('button', { name: /^delete$/i }))
    const mutateAsync = (useDeleteDeployment as Mock).mock.results[0].value.mutateAsync
    await waitFor(() => {
      expect(mutateAsync).toHaveBeenCalledWith({ name: 'api' })
    })
  })

  it('shows skeleton while loading', () => {
    ;(useGetDeployment as Mock).mockReturnValue({ data: undefined, isPending: true, error: null })
    ;(useGetDeploymentStatus as Mock).mockReturnValue({ data: undefined, isPending: true, error: null })
    ;(useGetDeploymentLogs as Mock).mockReturnValue({ data: undefined, isPending: true, error: null })
    ;(useUpdateDeployment as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false, reset: vi.fn() })
    ;(useDeleteDeployment as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false, error: null, reset: vi.fn() })
    ;(useGetProject as Mock).mockReturnValue({ data: undefined, isLoading: true })
    render(<DeploymentDetailPage />)
    const skeletons = document.querySelectorAll('[data-slot="skeleton"]')
    expect(skeletons.length).toBeGreaterThan(0)
  })

  it('shows error when fetch fails', () => {
    ;(useGetDeployment as Mock).mockReturnValue({ data: undefined, isPending: false, error: new Error('not found') })
    ;(useGetDeploymentStatus as Mock).mockReturnValue({ data: undefined, isPending: false, error: null })
    ;(useGetDeploymentLogs as Mock).mockReturnValue({ data: undefined, isPending: false, error: null })
    ;(useUpdateDeployment as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false, reset: vi.fn() })
    ;(useDeleteDeployment as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false, error: null, reset: vi.fn() })
    ;(useGetProject as Mock).mockReturnValue({ data: { name: 'test-project', userRole: Role.OWNER }, isLoading: false })
    render(<DeploymentDetailPage />)
    expect(screen.getByText('not found')).toBeInTheDocument()
  })

  it('renders back link to deployments list', () => {
    setupMocks()
    render(<DeploymentDetailPage />)
    expect(screen.getByRole('link', { name: /back to deployments/i })).toBeInTheDocument()
  })
})

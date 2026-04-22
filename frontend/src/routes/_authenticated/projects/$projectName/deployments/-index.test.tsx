/**
 * Tests for the Deployments index page (HOL-858) — ResourceGrid v1 implementation.
 *
 * Mocks @/queries/deployments and @/queries/projects. Covers:
 *  - DeploymentsDescription banner (copy-locking assertions)
 *  - ResourceGrid renders project rows
 *  - Delete flow via ConfirmDeleteDialog
 *  - Extra columns: Phase badge and PolicyDrift badge
 */

import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { DeploymentPhase, type DeploymentStatusSummary } from '@/gen/holos/console/v1/deployments_pb'

// ---------------------------------------------------------------------------
// Router mock — Route.useParams / useSearch / useNavigate
// ---------------------------------------------------------------------------

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => ({ projectName: 'test-project' }),
      useSearch: () => ({}),
      fullPath: '/projects/test-project/deployments/',
    }),
    Link: ({
      children,
      className,
    }: {
      children: React.ReactNode
      className?: string
    }) => <a href="#" className={className}>{children}</a>,
    useNavigate: () => vi.fn(),
  }
})

// ---------------------------------------------------------------------------
// Query mocks
// ---------------------------------------------------------------------------

vi.mock('@/queries/deployments', () => ({
  useListDeployments: vi.fn(),
  useDeleteDeployment: vi.fn(),
}))

vi.mock('@/queries/projects', () => ({
  useGetProject: vi.fn(),
}))

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

// ---------------------------------------------------------------------------
// Imports after mocks
// ---------------------------------------------------------------------------

import { useListDeployments, useDeleteDeployment } from '@/queries/deployments'
import { useGetProject } from '@/queries/projects'
import { DeploymentsListPage, DeploymentsDescription } from './index'

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function makeSummary(
  phase: DeploymentPhase,
  readyReplicas = 0,
  desiredReplicas = 0,
  policyDrift?: boolean,
): DeploymentStatusSummary {
  return {
    $typeName: 'holos.console.v1.DeploymentStatusSummary',
    phase,
    readyReplicas,
    desiredReplicas,
    availableReplicas: readyReplicas,
    updatedReplicas: readyReplicas,
    observedGeneration: 0n,
    message: '',
    output: undefined,
    policyDrift,
  }
}

type MockDeployment = {
  name: string
  project: string
  displayName: string
  description: string
  image: string
  tag: string
  template: string
  phase: DeploymentPhase
  message: string
  statusSummary?: DeploymentStatusSummary
}

function makeDeployment(
  name: string,
  statusSummary?: DeploymentStatusSummary,
  description = '',
): MockDeployment {
  return {
    name,
    project: 'test-project',
    displayName: '',
    description,
    image: 'ghcr.io/org/app',
    tag: 'v1.0.0',
    template: 'web-app',
    phase: DeploymentPhase.UNSPECIFIED,
    message: '',
    statusSummary,
  }
}

function setupMocks({
  deployments = [
    makeDeployment('api', makeSummary(DeploymentPhase.RUNNING, 1, 1)),
    makeDeployment('worker', makeSummary(DeploymentPhase.PENDING, 0, 1)),
  ],
  isPending = false,
  error = null as Error | null,
  userRole = Role.OWNER,
} = {}) {
  ;(useListDeployments as Mock).mockReturnValue({ data: deployments, isPending, error })
  ;(useDeleteDeployment as Mock).mockReturnValue({
    mutateAsync: vi.fn().mockResolvedValue(undefined),
    isPending: false,
  })
  ;(useGetProject as Mock).mockReturnValue({
    data: { name: 'test-project', userRole },
    isLoading: false,
  })
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('DeploymentsListPage (ResourceGrid v1)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  // -------------------------------------------------------------------------
  // Description banner (copy-locking)
  // -------------------------------------------------------------------------

  describe('DeploymentsDescription banner', () => {
    it('renders the description banner', () => {
      render(<DeploymentsDescription />)
      expect(screen.getByTestId('deployments-description')).toBeInTheDocument()
    })

    it('contains the three verbatim bullet points', () => {
      render(<DeploymentsDescription />)
      expect(
        screen.getByText('Deployment is a collection of resource declarations (configuration).'),
      ).toBeInTheDocument()
      expect(
        screen.getByText('Deploying is applying the configuration to the platform.'),
      ).toBeInTheDocument()
      expect(
        screen.getByText('Controllers reconcile current state with desired state.'),
      ).toBeInTheDocument()
    })

    it('renders the description banner inside the full page', () => {
      setupMocks()
      render(<DeploymentsListPage />)
      expect(screen.getByTestId('deployments-description')).toBeInTheDocument()
    })
  })

  // -------------------------------------------------------------------------
  // Grid — project rows
  // -------------------------------------------------------------------------

  it('renders deployment names in the grid', () => {
    setupMocks()
    render(<DeploymentsListPage />)
    // Each name appears in both the Resource ID cell and the Display Name link
    expect(screen.getAllByText('api').length).toBeGreaterThanOrEqual(1)
    expect(screen.getAllByText('worker').length).toBeGreaterThanOrEqual(1)
  })

  it('shows loading skeleton when isPending is true', () => {
    setupMocks({ isPending: true, deployments: [] })
    render(<DeploymentsListPage />)
    expect(screen.getByTestId('resource-grid-loading')).toBeInTheDocument()
  })

  it('shows error state when fetch fails and no rows', () => {
    setupMocks({ error: new Error('fetch failed'), deployments: [] })
    render(<DeploymentsListPage />)
    expect(screen.getByText(/fetch failed/i)).toBeInTheDocument()
  })

  it('shows empty state when no deployments exist', () => {
    setupMocks({ deployments: [] })
    render(<DeploymentsListPage />)
    expect(screen.getByText(/no resources found/i)).toBeInTheDocument()
  })

  it('single parent hides Parent column', () => {
    setupMocks({ deployments: [makeDeployment('api'), makeDeployment('worker')] })
    render(<DeploymentsListPage />)
    // All rows have the same parentId (projectName) → Parent column hidden
    expect(screen.queryByRole('columnheader', { name: /parent/i })).not.toBeInTheDocument()
  })

  it('renders New Deployment button when user can create', () => {
    setupMocks({ userRole: Role.OWNER })
    render(<DeploymentsListPage />)
    expect(screen.getByRole('button', { name: /new deployment/i })).toBeInTheDocument()
  })

  it('does not render New Deployment button when user is viewer', () => {
    setupMocks({ userRole: Role.VIEWER })
    render(<DeploymentsListPage />)
    expect(screen.queryByRole('button', { name: /new deployment/i })).not.toBeInTheDocument()
  })

  it('deployment name links to detail page', () => {
    setupMocks({ deployments: [makeDeployment('api')] })
    render(<DeploymentsListPage />)
    const link = screen.getByRole('link', { name: 'api' })
    expect(link.getAttribute('href')).toContain('deployments/api')
  })

  // -------------------------------------------------------------------------
  // Delete flow
  // -------------------------------------------------------------------------

  it('delete button opens ConfirmDeleteDialog', async () => {
    setupMocks({ deployments: [makeDeployment('api')] })
    render(<DeploymentsListPage />)

    const deleteBtn = screen.getByRole('button', { name: /delete api/i })
    fireEvent.click(deleteBtn)

    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })
  })

  it('confirming delete invokes useDeleteDeployment.mutateAsync with { name }', async () => {
    const mutateAsync = vi.fn().mockResolvedValue(undefined)
    setupMocks({ deployments: [makeDeployment('api')] })
    ;(useDeleteDeployment as Mock).mockReturnValue({ mutateAsync, isPending: false })

    render(<DeploymentsListPage />)

    fireEvent.click(screen.getByRole('button', { name: /delete api/i }))

    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })

    fireEvent.click(screen.getByRole('button', { name: /^delete$/i }))

    await waitFor(() => {
      expect(mutateAsync).toHaveBeenCalledWith({ name: 'api' })
    })
  })

  // -------------------------------------------------------------------------
  // Extra columns — Phase badge and Policy Drift badge
  // -------------------------------------------------------------------------

  describe('Phase column (extraColumns)', () => {
    it('renders Running phase badge', () => {
      setupMocks({
        deployments: [makeDeployment('api', makeSummary(DeploymentPhase.RUNNING, 1, 1))],
      })
      render(<DeploymentsListPage />)
      expect(screen.getByText(/running/i)).toBeInTheDocument()
    })

    it('renders Pending phase badge', () => {
      setupMocks({
        deployments: [makeDeployment('worker', makeSummary(DeploymentPhase.PENDING, 0, 1))],
      })
      render(<DeploymentsListPage />)
      expect(screen.getByText(/pending/i)).toBeInTheDocument()
    })

    it('renders Unknown badge when statusSummary is absent', () => {
      setupMocks({ deployments: [makeDeployment('api')] })
      render(<DeploymentsListPage />)
      expect(screen.getByText(/unknown/i)).toBeInTheDocument()
    })
  })

  describe('PolicyDrift column (extraColumns)', () => {
    it('renders the Policy Drift badge when policyDrift is true', () => {
      setupMocks({
        deployments: [
          makeDeployment('api', makeSummary(DeploymentPhase.RUNNING, 1, 1, true)),
        ],
      })
      render(<DeploymentsListPage />)
      expect(screen.getByTestId('policy-drift-badge')).toBeInTheDocument()
    })

    it('does not render the Policy Drift badge when policyDrift is false', () => {
      setupMocks({
        deployments: [
          makeDeployment('api', makeSummary(DeploymentPhase.RUNNING, 1, 1, false)),
        ],
      })
      render(<DeploymentsListPage />)
      expect(screen.queryByTestId('policy-drift-badge')).not.toBeInTheDocument()
    })

    it('does not render the Policy Drift badge when policyDrift is undefined', () => {
      setupMocks({
        deployments: [makeDeployment('api', makeSummary(DeploymentPhase.RUNNING, 1, 1))],
      })
      render(<DeploymentsListPage />)
      expect(screen.queryByTestId('policy-drift-badge')).not.toBeInTheDocument()
    })

    it('renders the Policy Drift badge for viewers as well (read-only signal)', () => {
      setupMocks({
        deployments: [
          makeDeployment('api', makeSummary(DeploymentPhase.RUNNING, 1, 1, true)),
        ],
        userRole: Role.VIEWER,
      })
      render(<DeploymentsListPage />)
      expect(screen.getByTestId('policy-drift-badge')).toBeInTheDocument()
    })
  })

  // -------------------------------------------------------------------------
  // Description column
  // -------------------------------------------------------------------------

  it('description column shows deployment description', () => {
    setupMocks({ deployments: [makeDeployment('api', undefined, 'Serves the API')] })
    render(<DeploymentsListPage />)
    expect(screen.getByText('Serves the API')).toBeInTheDocument()
  })
})

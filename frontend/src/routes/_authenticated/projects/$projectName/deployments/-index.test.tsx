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
  useDeleteDeployment: vi.fn(),
}))

vi.mock('@/queries/projects', () => ({
  useGetProject: vi.fn(),
}))

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

import { useListDeployments, useDeleteDeployment } from '@/queries/deployments'
import { useGetProject } from '@/queries/projects'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { DeploymentPhase, type DeploymentStatusSummary, type DeploymentOutput } from '@/gen/holos/console/v1/deployments_pb'
import { DeploymentsPage } from './index'

function makeSummary(
  phase: DeploymentPhase,
  readyReplicas = 0,
  desiredReplicas = 0,
  output?: DeploymentOutput,
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
    output,
    policyDrift,
  }
}

function makeOutput(url: string): DeploymentOutput {
  return { $typeName: 'holos.console.v1.DeploymentOutput', url }
}

function makeDeployment(
  name: string,
  image = 'ghcr.io/org/app',
  tag = 'v1.0.0',
  statusSummary?: DeploymentStatusSummary,
) {
  // Legacy phase (field 8) and message (field 9) are deprecated and no longer
  // populated by the backend after the status cache rollout (#912). Tests
  // populate statusSummary only.
  return { name, project: 'test-project', image, tag, template: 'web-app', displayName: '', description: '', phase: DeploymentPhase.UNSPECIFIED, message: '', statusSummary }
}

function setupMocks(
  deployments = [
    makeDeployment('api', 'ghcr.io/org/app', 'v1.0.0', makeSummary(DeploymentPhase.RUNNING, 1, 1)),
    makeDeployment('worker', 'ghcr.io/org/wrk', 'latest', makeSummary(DeploymentPhase.PENDING, 0, 1)),
  ],
  userRole = Role.OWNER,
) {
  ;(useListDeployments as Mock).mockReturnValue({ data: deployments, isLoading: false, error: null })
  ;(useDeleteDeployment as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({}), isPending: false, error: null, reset: vi.fn() })
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

  it('renders Running badge with ready/desired replicas from status_summary', () => {
    setupMocks([
      makeDeployment('api', 'ghcr.io/org/app', 'v1.0.0',
        makeSummary(DeploymentPhase.RUNNING, 2, 3)),
    ])
    render(<DeploymentsPage />)
    expect(screen.getByText(/running/i)).toBeInTheDocument()
    expect(screen.getByText('2/3')).toBeInTheDocument()
  })

  it('renders Pending badge with replica count from status_summary', () => {
    setupMocks([
      makeDeployment('worker', 'ghcr.io/org/wrk', 'latest',
        makeSummary(DeploymentPhase.PENDING, 0, 1)),
    ])
    render(<DeploymentsPage />)
    expect(screen.getByText(/pending/i)).toBeInTheDocument()
    expect(screen.getByText('0/1')).toBeInTheDocument()
  })

  it('renders Failed badge with replica count from status_summary', () => {
    setupMocks([
      makeDeployment('api', 'ghcr.io/org/app', 'v1.0.0',
        makeSummary(DeploymentPhase.FAILED, 0, 1)),
    ])
    render(<DeploymentsPage />)
    expect(screen.getByText(/failed/i)).toBeInTheDocument()
    expect(screen.getByText('0/1')).toBeInTheDocument()
  })

  it('renders Unknown only when status_summary is missing', () => {
    setupMocks([
      makeDeployment('api', 'ghcr.io/org/app', 'v1.0.0' /* no summary */),
    ])
    render(<DeploymentsPage />)
    expect(screen.getByText(/unknown/i)).toBeInTheDocument()
    // No replica count rendered when summary is missing.
    expect(screen.queryByText(/^\d+\/\d+$/)).not.toBeInTheDocument()
  })

  it('omits replica count when desired_replicas is zero', () => {
    setupMocks([
      makeDeployment('api', 'ghcr.io/org/app', 'v1.0.0',
        makeSummary(DeploymentPhase.RUNNING, 0, 0)),
    ])
    render(<DeploymentsPage />)
    expect(screen.getByText(/running/i)).toBeInTheDocument()
    expect(screen.queryByText('0/0')).not.toBeInTheDocument()
  })

  it('shows empty state when no deployments', () => {
    setupMocks([])
    render(<DeploymentsPage />)
    expect(screen.getByText(/no deployments yet/i)).toBeInTheDocument()
  })

  it('renders Create Deployment link for owners', () => {
    setupMocks([], Role.OWNER)
    render(<DeploymentsPage />)
    const links = screen.getAllByRole('link', { name: /create deployment/i })
    expect(links.length).toBeGreaterThan(0)
    expect(links[0].getAttribute('href')).toContain('deployments/new')
  })

  it('renders Create Deployment link for editors', () => {
    setupMocks([], Role.EDITOR)
    render(<DeploymentsPage />)
    const links = screen.getAllByRole('link', { name: /create deployment/i })
    expect(links.length).toBeGreaterThan(0)
    expect(links[0].getAttribute('href')).toContain('deployments/new')
  })

  it('does not render Create Deployment link for viewers', () => {
    setupMocks([], Role.VIEWER)
    render(<DeploymentsPage />)
    expect(screen.queryByRole('link', { name: /create deployment/i })).not.toBeInTheDocument()
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
    ;(useDeleteDeployment as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false, error: null, reset: vi.fn() })
    ;(useGetProject as Mock).mockReturnValue({ data: { name: 'test-project', userRole: Role.OWNER }, isLoading: false })
    render(<DeploymentsPage />)
    expect(screen.getByText(/fetch failed/)).toBeInTheDocument()
  })

  it('opens delete dialog when delete button is clicked', async () => {
    setupMocks([makeDeployment('api')], Role.OWNER)
    render(<DeploymentsPage />)
    fireEvent.click(screen.getByRole('button', { name: /delete api/i }))
    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })
  })

  it('Create Deployment link in empty state navigates to new page', () => {
    setupMocks([], Role.OWNER)
    render(<DeploymentsPage />)
    const links = screen.getAllByRole('link', { name: /create deployment/i })
    // All Create Deployment links should point to the new page
    links.forEach((link) => {
      expect(link.getAttribute('href')).toContain('deployments/new')
    })
  })

  it('renders Open link when statusSummary.output.url is a safe http(s) URL', () => {
    setupMocks([
      makeDeployment('api', 'ghcr.io/org/app', 'v1.0.0',
        makeSummary(DeploymentPhase.RUNNING, 1, 1, makeOutput('https://api.example.com'))),
    ])
    render(<DeploymentsPage />)
    const link = screen.getByRole('link', { name: /open api/i })
    expect(link.getAttribute('href')).toBe('https://api.example.com')
    expect(link.getAttribute('target')).toBe('_blank')
    expect(link.getAttribute('rel')).toBe('noopener noreferrer')
  })

  it('does not render Open link when statusSummary.output.url is absent', () => {
    setupMocks([
      makeDeployment('api', 'ghcr.io/org/app', 'v1.0.0',
        makeSummary(DeploymentPhase.RUNNING, 1, 1 /* no output */)),
    ])
    render(<DeploymentsPage />)
    expect(screen.queryByRole('link', { name: /open api/i })).not.toBeInTheDocument()
  })

  it('does not render Open link when statusSummary.output.url is empty string', () => {
    setupMocks([
      makeDeployment('api', 'ghcr.io/org/app', 'v1.0.0',
        makeSummary(DeploymentPhase.RUNNING, 1, 1, makeOutput(''))),
    ])
    render(<DeploymentsPage />)
    expect(screen.queryByRole('link', { name: /open api/i })).not.toBeInTheDocument()
  })

  it('does not render Open link when output.url uses an unsafe scheme', () => {
    // Defense-in-depth: templates are admin-authored, but the UI still
    // refuses javascript:/data:/vbscript:/file: URLs so they cannot reach
    // an anchor href and execute on click.
    setupMocks([
      makeDeployment('api', 'ghcr.io/org/app', 'v1.0.0',
        makeSummary(DeploymentPhase.RUNNING, 1, 1, makeOutput('javascript:alert(1)'))),
    ])
    render(<DeploymentsPage />)
    expect(screen.queryByRole('link', { name: /open api/i })).not.toBeInTheDocument()
  })

  describe('policy drift badge', () => {
    // HOL-559: the deployment list surfaces policy drift when the backend
    // populates status_summary.policy_drift. The flag is sourced from the
    // folder-namespace render-state store via HOL-567.
    it('renders the Policy Drift badge when policy_drift is true', () => {
      setupMocks([
        makeDeployment(
          'api',
          'ghcr.io/org/app',
          'v1.0.0',
          makeSummary(DeploymentPhase.RUNNING, 1, 1, undefined, true),
        ),
      ])
      render(<DeploymentsPage />)
      expect(screen.getByTestId('policy-drift-badge')).toBeInTheDocument()
    })

    it('does not render the Policy Drift badge when policy_drift is false', () => {
      setupMocks([
        makeDeployment(
          'api',
          'ghcr.io/org/app',
          'v1.0.0',
          makeSummary(DeploymentPhase.RUNNING, 1, 1, undefined, false),
        ),
      ])
      render(<DeploymentsPage />)
      expect(screen.queryByTestId('policy-drift-badge')).not.toBeInTheDocument()
    })

    it('does not render the Policy Drift badge when policy_drift is undefined', () => {
      setupMocks([
        makeDeployment(
          'api',
          'ghcr.io/org/app',
          'v1.0.0',
          makeSummary(DeploymentPhase.RUNNING, 1, 1),
        ),
      ])
      render(<DeploymentsPage />)
      expect(screen.queryByTestId('policy-drift-badge')).not.toBeInTheDocument()
    })

    it('renders the Policy Drift badge for viewers as well (read-only signal)', () => {
      setupMocks(
        [
          makeDeployment(
            'api',
            'ghcr.io/org/app',
            'v1.0.0',
            makeSummary(DeploymentPhase.RUNNING, 1, 1, undefined, true),
          ),
        ],
        Role.VIEWER,
      )
      render(<DeploymentsPage />)
      expect(screen.getByTestId('policy-drift-badge')).toBeInTheDocument()
    })
  })
})

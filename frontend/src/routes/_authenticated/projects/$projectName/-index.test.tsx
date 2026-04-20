import { render, screen } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => ({ projectName: 'my-project' }),
    }),
    Link: ({
      children,
      to,
      params,
      className,
    }: {
      children: React.ReactNode
      to: string
      params?: Record<string, string>
      className?: string
    }) => {
      let href = to
      if (params) {
        for (const [k, v] of Object.entries(params)) {
          href = href.replace(`$${k}`, v)
        }
      }
      return (
        <a href={href} className={className}>
          {children}
        </a>
      )
    },
  }
})

vi.mock('@/queries/deployments', () => ({
  useListDeployments: vi.fn(),
}))

vi.mock('@/queries/projects', () => ({
  useGetProject: vi.fn(),
}))

import { useListDeployments } from '@/queries/deployments'
import { useGetProject } from '@/queries/projects'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { DeploymentPhase } from '@/gen/holos/console/v1/deployments_pb'
import { ProjectIndexPage } from './index'

type DeploymentFixture = {
  name: string
  image: string
  tag: string
  statusSummary?: {
    phase: DeploymentPhase
    desiredReplicas: number
    readyReplicas: number
  }
}

function makeDeployment(
  name: string,
  overrides: Partial<DeploymentFixture> = {},
): DeploymentFixture {
  return {
    name,
    image: 'registry/app',
    tag: 'v1.0.0',
    statusSummary: {
      phase: DeploymentPhase.RUNNING,
      desiredReplicas: 1,
      readyReplicas: 1,
    },
    ...overrides,
  }
}

function setup(
  deployments: DeploymentFixture[] | undefined = [],
  overrides: {
    isPending?: boolean
    error?: Error | null
    userRole?: Role
  } = {},
) {
  ;(useListDeployments as Mock).mockReturnValue({
    data: overrides.isPending ? undefined : deployments,
    isPending: overrides.isPending ?? false,
    error: overrides.error ?? null,
  })
  ;(useGetProject as Mock).mockReturnValue({
    data: {
      name: 'my-project',
      organization: 'my-org',
      userRole: overrides.userRole ?? Role.OWNER,
    },
    isPending: false,
    error: null,
  })
}

describe('ProjectIndexPage', () => {
  beforeEach(() => vi.clearAllMocks())

  it('renders all three sections: Deployments, Usage / Quota / Limits, Service Status', () => {
    setup([])
    render(<ProjectIndexPage projectName="my-project" />)
    // "Deployments" is used twice — the section title and a quota-bar label —
    // so match both. The other titles are unique.
    expect(screen.getAllByText('Deployments').length).toBeGreaterThanOrEqual(1)
    expect(screen.getByText('Usage / Quota / Limits')).toBeInTheDocument()
    expect(screen.getByText('Service Status')).toBeInTheDocument()
  })

  it('renders deployments loading skeleton while pending', () => {
    setup(undefined, { isPending: true })
    const { container } = render(<ProjectIndexPage projectName="my-project" />)
    expect(
      container.querySelector('[data-testid="deployments-loading"]'),
    ).toBeInTheDocument()
  })

  it('renders deployments error when the list fails', () => {
    setup([], { error: new Error('bad gateway') })
    render(<ProjectIndexPage projectName="my-project" />)
    expect(screen.getByText('bad gateway')).toBeInTheDocument()
  })

  it('renders deployment empty state', () => {
    setup([])
    render(<ProjectIndexPage projectName="my-project" />)
    expect(screen.getByText(/no deployments yet/i)).toBeInTheDocument()
  })

  it('renders a row per deployment with name, image:tag, and phase', () => {
    setup([
      makeDeployment('web'),
      makeDeployment('worker', { image: 'registry/worker', tag: 'v2' }),
    ])
    render(<ProjectIndexPage projectName="my-project" />)
    expect(screen.getByRole('link', { name: 'web' })).toHaveAttribute(
      'href',
      '/projects/my-project/deployments/web',
    )
    expect(screen.getByText('registry/app:v1.0.0')).toBeInTheDocument()
    expect(screen.getByText('registry/worker:v2')).toBeInTheDocument()
    // Both deployments show Running.
    expect(screen.getAllByText('Running').length).toBe(2)
  })

  it('shows Create Deployment for OWNER and EDITOR roles', () => {
    setup([], { userRole: Role.EDITOR })
    render(<ProjectIndexPage projectName="my-project" />)
    expect(
      screen.getByRole('link', { name: /create deployment/i }),
    ).toBeInTheDocument()
  })

  it('hides Create Deployment for VIEWER role', () => {
    setup([], { userRole: Role.VIEWER })
    render(<ProjectIndexPage projectName="my-project" />)
    expect(
      screen.queryByRole('link', { name: /create deployment/i }),
    ).toBeNull()
  })

  it('renders the View all link pointing at the deployments list', () => {
    setup([])
    render(<ProjectIndexPage projectName="my-project" />)
    expect(screen.getByRole('link', { name: /view all/i })).toHaveAttribute(
      'href',
      '/projects/my-project/deployments',
    )
  })

  it('renders the Quota placeholder caption', () => {
    setup([])
    render(<ProjectIndexPage projectName="my-project" />)
    expect(
      screen.getByText(/resource tracking not yet implemented/i),
    ).toBeInTheDocument()
  })

  it('renders placeholder progress bars with role="progressbar"', () => {
    setup([])
    render(<ProjectIndexPage projectName="my-project" />)
    // CPU, Memory, Storage, Deployments — four bars.
    expect(screen.getAllByRole('progressbar').length).toBe(4)
  })

  it('renders the Deployment Service row unconditionally', () => {
    setup([])
    render(<ProjectIndexPage projectName="my-project" />)
    expect(screen.getByText('Deployment Service')).toBeInTheDocument()
  })

  it('renders Database and Identity Provider dependency rows', () => {
    setup([])
    render(<ProjectIndexPage projectName="my-project" />)
    expect(screen.getByText('Database')).toBeInTheDocument()
    expect(screen.getByText('Identity Provider')).toBeInTheDocument()
  })
})

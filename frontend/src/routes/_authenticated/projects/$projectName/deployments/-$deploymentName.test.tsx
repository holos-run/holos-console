import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

// Default useSearch stub returns 'status' tab. The return type is widened to
// `string` so tests can exercise `validateTab` with legacy values (e.g.
// 'template') that the production validator now rejects — see the
// `?tab=template` regression test below (HOL-611).
const mockUseSearch = vi.fn<() => { tab: string }>(() => ({ tab: 'status' }))
const mockUseNavigate = vi.fn(() => vi.fn())

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => ({ projectName: 'test-project', deploymentName: 'api' }),
      useSearch: () => mockUseSearch(),
    }),
    Link: ({ children, className, to, params }: { children: React.ReactNode; className?: string; to?: string; params?: Record<string, string> }) => (
      <a href={to} data-params={JSON.stringify(params)} className={className}>{children}</a>
    ),
    useNavigate: () => mockUseNavigate(),
  }
})

vi.mock('@/queries/deployments', () => ({
  useGetDeployment: vi.fn(),
  useGetDeploymentStatus: vi.fn(),
  useGetDeploymentLogs: vi.fn(),
  useUpdateDeployment: vi.fn(),
  useDeleteDeployment: vi.fn(),
  useListNamespaceSecrets: vi.fn(),
  useListNamespaceConfigMaps: vi.fn(),
  useGetDeploymentPolicyState: vi.fn(),
  useGetDeploymentRenderPreview: vi.fn(),
}))

vi.mock('@/queries/projects', () => ({
  useGetProject: vi.fn(),
}))

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

// ReverseDependents is covered by its own unit test; stub it here so
// deployment detail tests stay focused on deployment behavior.
vi.mock('@/components/templates/ReverseDependents', () => ({
  ReverseDependents: () => <div data-testid="reverse-dependents-stub" />,
}))

vi.mock('@/queries/templateDependencies', () => ({
  useListTemplateDependents: vi.fn().mockReturnValue({
    data: [],
    isPending: false,
    error: null,
  }),
  useListDeploymentDependents: vi.fn().mockReturnValue({
    data: [],
    isPending: false,
    error: null,
  }),
}))

vi.mock('@/lib/scope-labels', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/scope-labels')>()
  return {
    ...actual,
    namespaceForProject: vi.fn((name: string) => `holos-prj-${name}`),
  }
})

import { useGetDeployment, useGetDeploymentStatus, useGetDeploymentLogs, useUpdateDeployment, useDeleteDeployment, useListNamespaceSecrets, useListNamespaceConfigMaps, useGetDeploymentPolicyState, useGetDeploymentRenderPreview } from '@/queries/deployments'
import { useGetProject } from '@/queries/projects'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { DeploymentPhase } from '@/gen/holos/console/v1/deployments_pb'
import { DeploymentDetailPage } from './$deploymentName'

const mockDeployment = {
  name: 'api',
  project: 'test-project',
  image: 'ghcr.io/org/api',
  tag: 'v1.2.3',
  port: 8080,
  template: 'web-app',
  displayName: 'API Service',
  description: '',
  phase: DeploymentPhase.RUNNING,
  message: '',
  command: [] as string[],
  args: [] as string[],
  env: [] as unknown[],
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
    { name: 'api-7d8f9b6c4-xk2m3', phase: 'Running', ready: true, restartCount: 0, containerStatuses: [], events: [] },
  ],
  events: [],
}

/** Creates a Timestamp-like object for protobuf. */
function ts(isoStr: string) {
  const ms = new Date(isoStr).getTime()
  return { seconds: BigInt(Math.floor(ms / 1000)), nanos: 0, toDate: () => new Date(isoStr) }
}

const mockStatusWithEvents = {
  ...mockStatus,
  events: [
    {
      type: 'Warning',
      reason: 'Failed',
      message: 'Failed to pull image "ghcr.io/org/app:bad": rpc error',
      source: 'kubelet',
      count: 4,
      firstSeen: ts('2025-01-15T10:00:00Z'),
      lastSeen: ts('2025-01-15T10:28:00Z'),
      involvedObjectName: 'api-7d8f9b6c4-xk2m3',
    },
    {
      type: 'Normal',
      reason: 'Scheduled',
      message: 'Successfully assigned default/api-7d8f9b6c4-xk2m3',
      source: 'default-scheduler',
      count: 1,
      firstSeen: ts('2025-01-15T10:00:00Z'),
      lastSeen: ts('2025-01-15T10:00:00Z'),
      involvedObjectName: 'api-7d8f9b6c4-xk2m3',
    },
  ],
  pods: [
    {
      name: 'api-7d8f9b6c4-xk2m3',
      phase: 'Pending',
      ready: false,
      restartCount: 2,
      containerStatuses: [
        {
          name: 'app',
          state: 'waiting',
          reason: 'ImagePullBackOff',
          message: 'Failed to pull "ghcr.io/org/app:bad"',
          image: 'ghcr.io/org/app:bad',
          ready: false,
          restartCount: 2,
          startedAt: undefined,
        },
      ],
      events: [],
    },
  ],
}

const mockStatusWithHealthyContainers = {
  ...mockStatus,
  pods: [
    {
      name: 'api-7d8f9b6c4-xk2m3',
      phase: 'Running',
      ready: true,
      restartCount: 0,
      containerStatuses: [
        {
          name: 'app',
          state: 'running',
          reason: '',
          message: '',
          image: 'ghcr.io/org/api:v1.2.3',
          ready: true,
          restartCount: 0,
          startedAt: ts('2025-01-15T10:00:00Z'),
        },
      ],
      events: [],
    },
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
  ;(useListNamespaceSecrets as Mock).mockReturnValue({ data: [], isLoading: false })
  ;(useListNamespaceConfigMaps as Mock).mockReturnValue({ data: [], isLoading: false })
  ;(useGetDeploymentPolicyState as Mock).mockReturnValue({ data: undefined, isPending: false, error: null })
  ;(useGetDeploymentRenderPreview as Mock).mockReturnValue({ data: undefined, isPending: false, error: null })
}

describe('DeploymentDetailPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockUseSearch.mockReturnValue({ tab: 'status' })
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

  it('re-deploy dialog shows command and args inputs', () => {
    setupMocks(Role.OWNER)
    render(<DeploymentDetailPage />)
    fireEvent.click(screen.getByRole('button', { name: /re-?deploy/i }))
    expect(screen.getByText(/^command$/i)).toBeInTheDocument()
    expect(screen.getByText(/^args$/i)).toBeInTheDocument()
  })

  it('re-deploy dialog pre-populates command from deployment', () => {
    ;(useGetDeployment as Mock).mockReturnValue({
      data: { ...mockDeployment, command: ['myapp'], args: ['--port', '8080'], env: [] },
      isPending: false,
      error: null,
    })
    ;(useGetDeploymentStatus as Mock).mockReturnValue({ data: mockStatus, isPending: false, error: null })
    ;(useGetDeploymentLogs as Mock).mockReturnValue({ data: mockLogs, isPending: false, error: null })
    ;(useUpdateDeployment as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({}), isPending: false, reset: vi.fn() })
    ;(useDeleteDeployment as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({}), isPending: false, error: null, reset: vi.fn() })
    ;(useGetProject as Mock).mockReturnValue({ data: { name: 'test-project', userRole: Role.OWNER }, isLoading: false })
    render(<DeploymentDetailPage />)
    fireEvent.click(screen.getByRole('button', { name: /re-?deploy/i }))
    expect(screen.getByText('myapp')).toBeInTheDocument()
    expect(screen.getByText('--port')).toBeInTheDocument()
    expect(screen.getByText('8080')).toBeInTheDocument()
  })

  it('re-deploy passes command and args to mutateAsync', async () => {
    setupMocks(Role.OWNER)
    render(<DeploymentDetailPage />)
    fireEvent.click(screen.getByRole('button', { name: /re-?deploy/i }))

    // Add a command
    fireEvent.change(screen.getByLabelText(/command entry/i), { target: { value: 'myapp' } })
    fireEvent.click(screen.getByRole('button', { name: /add command/i }))

    fireEvent.click(screen.getByRole('button', { name: /^deploy$/i }))
    const mutateAsync = (useUpdateDeployment as Mock).mock.results[0].value.mutateAsync
    await waitFor(() => {
      expect(mutateAsync).toHaveBeenCalledWith(
        expect.objectContaining({ command: ['myapp'] }),
      )
    })
  })

  it('re-deploy dialog has Port field', () => {
    setupMocks(Role.OWNER)
    render(<DeploymentDetailPage />)
    fireEvent.click(screen.getByRole('button', { name: /re-?deploy/i }))
    expect(screen.getByLabelText(/^port$/i)).toBeInTheDocument()
  })

  it('re-deploy dialog pre-populates port from deployment', () => {
    setupMocks(Role.OWNER)
    render(<DeploymentDetailPage />)
    fireEvent.click(screen.getByRole('button', { name: /re-?deploy/i }))
    const portInput = screen.getByLabelText(/^port$/i) as HTMLInputElement
    expect(portInput.value).toBe('8080')
  })

  it('re-deploy passes port to mutateAsync', async () => {
    setupMocks(Role.OWNER)
    render(<DeploymentDetailPage />)
    fireEvent.click(screen.getByRole('button', { name: /re-?deploy/i }))

    const portInput = screen.getByLabelText(/^port$/i)
    fireEvent.change(portInput, { target: { value: '3000' } })

    fireEvent.click(screen.getByRole('button', { name: /^deploy$/i }))
    const mutateAsync = (useUpdateDeployment as Mock).mock.results[0].value.mutateAsync
    await waitFor(() => {
      expect(mutateAsync).toHaveBeenCalledWith(
        expect.objectContaining({ port: 3000 }),
      )
    })
  })

  it('re-deploy dialog has Environment Variables section', () => {
    setupMocks(Role.OWNER)
    render(<DeploymentDetailPage />)
    fireEvent.click(screen.getByRole('button', { name: /re-?deploy/i }))
    expect(screen.getAllByText(/environment variables/i).length).toBeGreaterThan(0)
    expect(screen.getByRole('button', { name: /add environment variable/i })).toBeInTheDocument()
  })

  it('re-deploy passes env to mutateAsync', async () => {
    setupMocks(Role.OWNER)
    render(<DeploymentDetailPage />)
    fireEvent.click(screen.getByRole('button', { name: /re-?deploy/i }))

    // Add an env var
    fireEvent.click(screen.getByRole('button', { name: /add environment variable/i }))
    fireEvent.change(screen.getByLabelText(/env var name/i), { target: { value: 'DB_URL' } })
    fireEvent.change(screen.getByLabelText(/literal value/i), { target: { value: 'postgres://localhost' } })

    fireEvent.click(screen.getByRole('button', { name: /^deploy$/i }))
    const mutateAsync = (useUpdateDeployment as Mock).mock.results[0].value.mutateAsync
    await waitFor(() => {
      expect(mutateAsync).toHaveBeenCalledWith(
        expect.objectContaining({
          env: [expect.objectContaining({ name: 'DB_URL', source: { case: 'value', value: 'postgres://localhost' } })],
        }),
      )
    })
  })

  it('does not render env vars table when deployment has no env vars', () => {
    setupMocks(Role.OWNER)
    render(<DeploymentDetailPage />)
    expect(screen.queryByRole('columnheader', { name: /name/i })).not.toBeInTheDocument()
  })

  it('renders env vars table when deployment has env vars', () => {
    ;(useGetDeployment as Mock).mockReturnValue({
      data: {
        ...mockDeployment,
        env: [
          { name: 'DATABASE_URL', source: { case: 'value', value: 'postgres://localhost' } },
          { name: 'API_KEY', source: { case: 'secretKeyRef', value: { name: 'my-secret', key: 'token' } } },
        ],
      },
      isPending: false,
      error: null,
    })
    ;(useGetDeploymentStatus as Mock).mockReturnValue({ data: mockStatus, isPending: false, error: null })
    ;(useGetDeploymentLogs as Mock).mockReturnValue({ data: mockLogs, isPending: false, error: null })
    ;(useUpdateDeployment as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({}), isPending: false, reset: vi.fn() })
    ;(useDeleteDeployment as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({}), isPending: false, error: null, reset: vi.fn() })
    ;(useGetProject as Mock).mockReturnValue({ data: { name: 'test-project', userRole: Role.OWNER }, isLoading: false })
    ;(useListNamespaceSecrets as Mock).mockReturnValue({ data: [], isLoading: false })
    ;(useListNamespaceConfigMaps as Mock).mockReturnValue({ data: [], isLoading: false })
    render(<DeploymentDetailPage />)
    expect(screen.getByText('DATABASE_URL')).toBeInTheDocument()
    expect(screen.getByText('postgres://localhost')).toBeInTheDocument()
    expect(screen.getByText('API_KEY')).toBeInTheDocument()
    expect(screen.getByText('my-secret → token')).toBeInTheDocument()
    expect(screen.getByText('Secret')).toBeInTheDocument()
  })

  it('re-deploy dialog pre-populates env from deployment', () => {
    ;(useGetDeployment as Mock).mockReturnValue({
      data: {
        ...mockDeployment,
        env: [{ name: 'DB_URL', source: { case: 'value', value: 'postgres://localhost' } }],
      },
      isPending: false,
      error: null,
    })
    ;(useGetDeploymentStatus as Mock).mockReturnValue({ data: mockStatus, isPending: false, error: null })
    ;(useGetDeploymentLogs as Mock).mockReturnValue({ data: mockLogs, isPending: false, error: null })
    ;(useUpdateDeployment as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({}), isPending: false, reset: vi.fn() })
    ;(useDeleteDeployment as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({}), isPending: false, error: null, reset: vi.fn() })
    ;(useGetProject as Mock).mockReturnValue({ data: { name: 'test-project', userRole: Role.OWNER }, isLoading: false })
    ;(useListNamespaceSecrets as Mock).mockReturnValue({ data: [], isLoading: false })
    ;(useListNamespaceConfigMaps as Mock).mockReturnValue({ data: [], isLoading: false })
    render(<DeploymentDetailPage />)
    fireEvent.click(screen.getByRole('button', { name: /re-?deploy/i }))
    // The env var name input should be pre-populated
    expect(screen.getByDisplayValue('DB_URL')).toBeInTheDocument()
  })

  // ── Tab layout tests ──────────────────────────────────────────────────────

  it('renders Status tab by default', () => {
    setupMocks()
    render(<DeploymentDetailPage />)
    // Status tab trigger should be present and active
    const statusTab = screen.getByRole('tab', { name: /status/i })
    expect(statusTab).toBeInTheDocument()
    expect(statusTab).toHaveAttribute('data-state', 'active')
  })

  it('Status tab content is visible by default without clicking', () => {
    setupMocks()
    render(<DeploymentDetailPage />)
    // Replica count visible on default Status tab
    expect(screen.getByText(/1\/1 ready/)).toBeInTheDocument()
  })

  it('switches to Logs tab when clicked and shows log viewer', async () => {
    const user = userEvent.setup()
    setupMocks()
    render(<DeploymentDetailPage />)
    await user.click(screen.getByRole('tab', { name: /logs/i }))
    expect(screen.getByText(/Starting server/)).toBeInTheDocument()
  })

  it('does not render a Template tab trigger (HOL-611)', () => {
    setupMocks()
    render(<DeploymentDetailPage />)
    expect(screen.queryByRole('tab', { name: /template/i })).not.toBeInTheDocument()
  })

  it('does not render the Template Preview panel (HOL-611)', () => {
    setupMocks()
    render(<DeploymentDetailPage />)
    expect(screen.queryByText('Template Preview')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('CUE Template')).not.toBeInTheDocument()
  })

  it('keeps Re-deploy and Delete buttons visible on all tabs', async () => {
    const user = userEvent.setup()
    setupMocks(Role.OWNER)
    render(<DeploymentDetailPage />)

    // Visible on Status tab (default)
    expect(screen.getByRole('button', { name: /re-?deploy/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /delete deployment/i })).toBeInTheDocument()

    // Visible on Logs tab
    await user.click(screen.getByRole('tab', { name: /logs/i }))
    expect(screen.getByRole('button', { name: /re-?deploy/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /delete deployment/i })).toBeInTheDocument()
  })

  it('activates Logs tab when URL has ?tab=logs', () => {
    mockUseSearch.mockReturnValue({ tab: 'logs' })
    setupMocks()
    render(<DeploymentDetailPage />)
    const logsTab = screen.getByRole('tab', { name: /logs/i })
    expect(logsTab).toHaveAttribute('data-state', 'active')
    expect(screen.getByText(/Starting server/)).toBeInTheDocument()
  })

  it('falls back to Status tab when URL has ?tab=template (HOL-611)', () => {
    // Template tab no longer exists. An inbound deep-link with the legacy
    // value should degrade to Status rather than producing an orphaned
    // "unknown tab" state, so existing bookmarks do not break.
    mockUseSearch.mockReturnValue({ tab: 'template' })
    setupMocks()
    render(<DeploymentDetailPage />)
    const statusTab = screen.getByRole('tab', { name: /status/i })
    expect(statusTab).toHaveAttribute('data-state', 'active')
  })

  it('log viewer does not have max-h-96 class (uses larger height)', async () => {
    const user = userEvent.setup()
    setupMocks()
    render(<DeploymentDetailPage />)
    await user.click(screen.getByRole('tab', { name: /logs/i }))
    // The log <pre> should not be capped at max-h-96
    const logPre = screen.getByText(/Starting server/).closest('pre')
    expect(logPre).not.toHaveClass('max-h-96')
  })

  describe('Logs tab section', () => {
    it('renders log content after clicking Logs tab', async () => {
      const user = userEvent.setup()
      setupMocks()
      render(<DeploymentDetailPage />)
      await user.click(screen.getByRole('tab', { name: /logs/i }))
      expect(screen.getByText(/Starting server/)).toBeInTheDocument()
    })

    it('renders tail lines selector in Logs tab', async () => {
      const user = userEvent.setup()
      setupMocks()
      render(<DeploymentDetailPage />)
      await user.click(screen.getByRole('tab', { name: /logs/i }))
      expect(screen.getByRole('combobox', { name: /tail lines/i })).toBeInTheDocument()
    })

    it('renders previous checkbox in Logs tab', async () => {
      const user = userEvent.setup()
      setupMocks()
      render(<DeploymentDetailPage />)
      await user.click(screen.getByRole('tab', { name: /logs/i }))
      expect(screen.getByLabelText(/previous/i)).toBeInTheDocument()
    })
  })

  // ── Events table tests ──────────────────────────────────────────────────

  describe('Events section', () => {
    function setupWithEvents() {
      ;(useGetDeployment as Mock).mockReturnValue({ data: mockDeployment, isPending: false, error: null })
      ;(useGetDeploymentStatus as Mock).mockReturnValue({ data: mockStatusWithEvents, isPending: false, error: null })
      ;(useGetDeploymentLogs as Mock).mockReturnValue({ data: mockLogs, isPending: false, error: null })
      ;(useUpdateDeployment as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({}), isPending: false, reset: vi.fn() })
      ;(useDeleteDeployment as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({}), isPending: false, error: null, reset: vi.fn() })
      ;(useGetProject as Mock).mockReturnValue({ data: { name: 'test-project', userRole: Role.OWNER }, isLoading: false })
      ;(useListNamespaceSecrets as Mock).mockReturnValue({ data: [], isLoading: false })
      ;(useListNamespaceConfigMaps as Mock).mockReturnValue({ data: [], isLoading: false })
    }

    it('renders events table with correct columns', () => {
      setupWithEvents()
      render(<DeploymentDetailPage />)
      expect(screen.getByText('Events')).toBeInTheDocument()
      expect(screen.getByText('Reason')).toBeInTheDocument()
      expect(screen.getByText('Message')).toBeInTheDocument()
      expect(screen.getByText('Source')).toBeInTheDocument()
    })

    it('renders warning events with warning styling', () => {
      setupWithEvents()
      render(<DeploymentDetailPage />)
      // The Warning event row should contain the reason and message
      expect(screen.getByText('Failed')).toBeInTheDocument()
      expect(screen.getByText(/Failed to pull image/)).toBeInTheDocument()
      // Check that the warning icon is present (AlertTriangle / TriangleAlert)
      const warningRow = screen.getByText('Failed').closest('tr')
      expect(warningRow).not.toBeNull()
      // Warning rows should have a distinguishing visual treatment
      const warningIcon = warningRow!.querySelector('[data-testid="event-warning-icon"]')
      expect(warningIcon).toBeInTheDocument()
    })

    it('renders normal events without warning styling', () => {
      setupWithEvents()
      render(<DeploymentDetailPage />)
      expect(screen.getByText('Scheduled')).toBeInTheDocument()
      expect(screen.getByText(/Successfully assigned/)).toBeInTheDocument()
      const normalRow = screen.getByText('Scheduled').closest('tr')
      expect(normalRow).not.toBeNull()
      const normalIcon = normalRow!.querySelector('[data-testid="event-normal-icon"]')
      expect(normalIcon).toBeInTheDocument()
    })

    it('shows event count for events with count > 1', () => {
      setupWithEvents()
      render(<DeploymentDetailPage />)
      // The warning event has count=4, should show "x4"
      expect(screen.getByText(/x4/)).toBeInTheDocument()
    })

    it('shows event source', () => {
      setupWithEvents()
      render(<DeploymentDetailPage />)
      expect(screen.getByText('kubelet')).toBeInTheDocument()
      expect(screen.getByText('default-scheduler')).toBeInTheDocument()
    })

    it('shows "No events" when events array is empty', () => {
      setupMocks()
      render(<DeploymentDetailPage />)
      expect(screen.getByText(/no events/i)).toBeInTheDocument()
    })
  })

  // ── Container status tests ──────────────────────────────────────────────

  describe('Container status section', () => {
    function setupWithContainerStatus() {
      ;(useGetDeployment as Mock).mockReturnValue({ data: mockDeployment, isPending: false, error: null })
      ;(useGetDeploymentStatus as Mock).mockReturnValue({ data: mockStatusWithEvents, isPending: false, error: null })
      ;(useGetDeploymentLogs as Mock).mockReturnValue({ data: mockLogs, isPending: false, error: null })
      ;(useUpdateDeployment as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({}), isPending: false, reset: vi.fn() })
      ;(useDeleteDeployment as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({}), isPending: false, error: null, reset: vi.fn() })
      ;(useGetProject as Mock).mockReturnValue({ data: { name: 'test-project', userRole: Role.OWNER }, isLoading: false })
      ;(useListNamespaceSecrets as Mock).mockReturnValue({ data: [], isLoading: false })
      ;(useListNamespaceConfigMaps as Mock).mockReturnValue({ data: [], isLoading: false })
    }

    it('renders container name and state', () => {
      setupWithContainerStatus()
      render(<DeploymentDetailPage />)
      expect(screen.getByText('app')).toBeInTheDocument()
      expect(screen.getByText(/waiting/i)).toBeInTheDocument()
    })

    it('renders container reason and message for error state', () => {
      setupWithContainerStatus()
      render(<DeploymentDetailPage />)
      expect(screen.getByText('ImagePullBackOff')).toBeInTheDocument()
    })

    it('highlights containers in error state', () => {
      setupWithContainerStatus()
      render(<DeploymentDetailPage />)
      // The container in waiting state with error reason should have error styling
      const badge = screen.getByText(/waiting/i)
      // waiting + error reason = yellow/amber badge
      expect(badge.className).toMatch(/yellow|amber|destructive/)
    })

    it('renders healthy container state with green badge', () => {
      ;(useGetDeployment as Mock).mockReturnValue({ data: mockDeployment, isPending: false, error: null })
      ;(useGetDeploymentStatus as Mock).mockReturnValue({ data: mockStatusWithHealthyContainers, isPending: false, error: null })
      ;(useGetDeploymentLogs as Mock).mockReturnValue({ data: mockLogs, isPending: false, error: null })
      ;(useUpdateDeployment as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({}), isPending: false, reset: vi.fn() })
      ;(useDeleteDeployment as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({}), isPending: false, error: null, reset: vi.fn() })
      ;(useGetProject as Mock).mockReturnValue({ data: { name: 'test-project', userRole: Role.OWNER }, isLoading: false })
      ;(useListNamespaceSecrets as Mock).mockReturnValue({ data: [], isLoading: false })
      ;(useListNamespaceConfigMaps as Mock).mockReturnValue({ data: [], isLoading: false })
      render(<DeploymentDetailPage />)
      const runningBadge = screen.getByText(/running/i, { selector: '[data-testid="container-state-badge"]' })
      expect(runningBadge.className).toMatch(/green/)
    })

    it('shows container image', () => {
      ;(useGetDeployment as Mock).mockReturnValue({ data: mockDeployment, isPending: false, error: null })
      ;(useGetDeploymentStatus as Mock).mockReturnValue({ data: mockStatusWithEvents, isPending: false, error: null })
      ;(useGetDeploymentLogs as Mock).mockReturnValue({ data: mockLogs, isPending: false, error: null })
      ;(useUpdateDeployment as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({}), isPending: false, reset: vi.fn() })
      ;(useDeleteDeployment as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({}), isPending: false, error: null, reset: vi.fn() })
      ;(useGetProject as Mock).mockReturnValue({ data: { name: 'test-project', userRole: Role.OWNER }, isLoading: false })
      ;(useListNamespaceSecrets as Mock).mockReturnValue({ data: [], isLoading: false })
      ;(useListNamespaceConfigMaps as Mock).mockReturnValue({ data: [], isLoading: false })
      render(<DeploymentDetailPage />)
      // The container's image should be displayed
      expect(screen.getByText('ghcr.io/org/app:bad')).toBeInTheDocument()
    })

    it('terminated container with no error reason gets green badge', () => {
      const terminatedNormalStatus = {
        ...mockStatus,
        pods: [
          {
            name: 'job-pod-1',
            phase: 'Succeeded',
            ready: false,
            restartCount: 0,
            containerStatuses: [
              { name: 'worker', state: 'terminated', reason: 'Completed', message: '', image: 'ghcr.io/org/worker:v1', ready: false, restartCount: 0, startedAt: ts('2025-01-15T10:00:00Z') },
            ],
            events: [],
          },
        ],
      }
      ;(useGetDeployment as Mock).mockReturnValue({ data: mockDeployment, isPending: false, error: null })
      ;(useGetDeploymentStatus as Mock).mockReturnValue({ data: terminatedNormalStatus, isPending: false, error: null })
      ;(useGetDeploymentLogs as Mock).mockReturnValue({ data: mockLogs, isPending: false, error: null })
      ;(useUpdateDeployment as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({}), isPending: false, reset: vi.fn() })
      ;(useDeleteDeployment as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({}), isPending: false, error: null, reset: vi.fn() })
      ;(useGetProject as Mock).mockReturnValue({ data: { name: 'test-project', userRole: Role.OWNER }, isLoading: false })
      ;(useListNamespaceSecrets as Mock).mockReturnValue({ data: [], isLoading: false })
      ;(useListNamespaceConfigMaps as Mock).mockReturnValue({ data: [], isLoading: false })
      render(<DeploymentDetailPage />)
      const terminatedBadge = screen.getByText(/terminated/i, { selector: '[data-testid="container-state-badge"]' })
      // Completed (normal exit) should get green, not red
      expect(terminatedBadge.className).toMatch(/green/)
      expect(terminatedBadge.className).not.toMatch(/red/)
    })

    it('does not render container section when containerStatuses is empty', () => {
      setupMocks()
      render(<DeploymentDetailPage />)
      // With empty containerStatuses, no "Containers:" label should appear
      expect(screen.queryByText(/containers:/i)).not.toBeInTheDocument()
    })

    it('renders multiple pods with different container states', () => {
      const multiPodStatus = {
        ...mockStatus,
        events: [],
        pods: [
          {
            name: 'api-pod-1',
            phase: 'Running',
            ready: true,
            restartCount: 0,
            containerStatuses: [
              { name: 'app', state: 'running', reason: '', message: '', image: 'ghcr.io/org/api:v1', ready: true, restartCount: 0, startedAt: ts('2025-01-15T10:00:00Z') },
            ],
            events: [],
          },
          {
            name: 'api-pod-2',
            phase: 'Pending',
            ready: false,
            restartCount: 3,
            containerStatuses: [
              { name: 'app', state: 'waiting', reason: 'CrashLoopBackOff', message: 'back-off', image: 'ghcr.io/org/api:v1', ready: false, restartCount: 3, startedAt: undefined },
            ],
            events: [],
          },
        ],
      }
      ;(useGetDeployment as Mock).mockReturnValue({ data: mockDeployment, isPending: false, error: null })
      ;(useGetDeploymentStatus as Mock).mockReturnValue({ data: multiPodStatus, isPending: false, error: null })
      ;(useGetDeploymentLogs as Mock).mockReturnValue({ data: mockLogs, isPending: false, error: null })
      ;(useUpdateDeployment as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({}), isPending: false, reset: vi.fn() })
      ;(useDeleteDeployment as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({}), isPending: false, error: null, reset: vi.fn() })
      ;(useGetProject as Mock).mockReturnValue({ data: { name: 'test-project', userRole: Role.OWNER }, isLoading: false })
      ;(useListNamespaceSecrets as Mock).mockReturnValue({ data: [], isLoading: false })
      ;(useListNamespaceConfigMaps as Mock).mockReturnValue({ data: [], isLoading: false })
      render(<DeploymentDetailPage />)
      expect(screen.getByText('api-pod-1')).toBeInTheDocument()
      expect(screen.getByText('api-pod-2')).toBeInTheDocument()
      expect(screen.getByText('CrashLoopBackOff')).toBeInTheDocument()
    })
  })

  // ── Output URL row tests ─────────────────────────────────────────────────
  //
  // The Status tab surfaces the deployment's primary URL from the live
  // aggregator on `deployment.statusSummary.output.url` (HOL-574). The row
  // is visible only when that field is a safe http:/https: URL; otherwise
  // nothing is rendered. Since HOL-611 removed the Template Preview panel
  // from this page, the `useGetDeploymentRenderPreview` fallback is gone —
  // the primary URL comes exclusively from `statusSummary.output.url`.

  /** Builds a Deployment fixture with the supplied statusSummary.output.url. */
  function deploymentWithPrimaryURL(url: string | undefined) {
    if (url === undefined) {
      return { ...mockDeployment, statusSummary: undefined }
    }
    return {
      ...mockDeployment,
      statusSummary: {
        $typeName: 'holos.console.v1.DeploymentStatusSummary',
        phase: DeploymentPhase.RUNNING,
        readyReplicas: 1,
        desiredReplicas: 1,
        availableReplicas: 1,
        updatedReplicas: 1,
        observedGeneration: 0n,
        message: '',
        output: {
          $typeName: 'holos.console.v1.DeploymentOutput',
          url,
          links: [],
        },
      },
    }
  }

  describe('Output URL row', () => {
    it('renders an App URL link when statusSummary.output.url is a non-empty https URL', () => {
      setupMocks()
      ;(useGetDeployment as Mock).mockReturnValue({
        data: deploymentWithPrimaryURL('https://example.com/app'),
        isPending: false,
        error: null,
      })
      render(<DeploymentDetailPage />)
      const link = screen.getByRole('link', { name: /https:\/\/example\.com\/app/ })
      expect(link.getAttribute('href')).toBe('https://example.com/app')
      expect(link.getAttribute('target')).toBe('_blank')
      const rel = link.getAttribute('rel') ?? ''
      expect(rel).toContain('noopener')
      expect(rel).toContain('noreferrer')
      expect(screen.getByText(/^App URL$/i)).toBeInTheDocument()
    })

    it('does not render the App URL row when statusSummary is undefined', () => {
      setupMocks()
      ;(useGetDeployment as Mock).mockReturnValue({
        data: deploymentWithPrimaryURL(undefined),
        isPending: false,
        error: null,
      })
      render(<DeploymentDetailPage />)
      expect(screen.queryByTestId('deployment-output-url')).not.toBeInTheDocument()
      expect(screen.queryByText(/^App URL$/i)).not.toBeInTheDocument()
    })

    it('does not render the App URL row when statusSummary.output.url is an empty string', () => {
      setupMocks()
      ;(useGetDeployment as Mock).mockReturnValue({
        data: deploymentWithPrimaryURL(''),
        isPending: false,
        error: null,
      })
      render(<DeploymentDetailPage />)
      expect(screen.queryByTestId('deployment-output-url')).not.toBeInTheDocument()
      expect(screen.queryByText(/^App URL$/i)).not.toBeInTheDocument()
    })

    // Security: a compromised or buggy controller annotation could publish a
    // scheme like `javascript:`, `data:`, `vbscript:`, or `file:`. Rendering
    // such a value into an anchor href would let a click execute script or
    // load attacker-controlled content in the console UI context. The row
    // must be dropped entirely when the scheme is not http: or https:.
    it.each([
      ['javascript: scheme', 'javascript:alert(1)'],
      ['data: scheme', 'data:text/html,<script>alert(1)</script>'],
      ['vbscript: scheme', 'vbscript:msgbox(1)'],
      ['file: scheme', 'file:///etc/passwd'],
      ['malformed URL', 'not a url'],
      ['mailto: scheme', 'mailto:user@example.com'],
    ])('does not render the App URL link for unsafe or malformed URLs (%s)', (_label, url) => {
      setupMocks()
      ;(useGetDeployment as Mock).mockReturnValue({
        data: deploymentWithPrimaryURL(url),
        isPending: false,
        error: null,
      })
      render(<DeploymentDetailPage />)
      expect(screen.queryByTestId('deployment-output-url')).not.toBeInTheDocument()
      expect(screen.queryByText(/^App URL$/i)).not.toBeInTheDocument()
      const anchors = document.querySelectorAll('a')
      anchors.forEach((a) => {
        expect(a.getAttribute('href')).not.toBe(url)
      })
    })

    it('renders the App URL link for an http: scheme', () => {
      setupMocks()
      ;(useGetDeployment as Mock).mockReturnValue({
        data: deploymentWithPrimaryURL('http://example.com/app'),
        isPending: false,
        error: null,
      })
      render(<DeploymentDetailPage />)
      const link = screen.getByRole('link', { name: /http:\/\/example\.com\/app/ })
      expect(link.getAttribute('href')).toBe('http://example.com/app')
    })
  })

  // ── Links section tests (HOL-575) ────────────────────────────────────────
  //
  // The Status tab surfaces the full set of secondary external links from
  // `Deployment.statusSummary.output.links` in a dedicated "Links" section
  // that sits below the existing "App URL" row. The aggregated link set is
  // sourced from `useGetDeployment` (not `useGetDeploymentRenderPreview`)
  // because the HOL-573 / HOL-574 aggregator harvests live resource
  // annotations into `statusSummary.output.links` — those entries are not
  // present in the render preview, which only sees template-evaluated
  // output. Each entry renders as a target=_blank anchor with
  // rel=noopener noreferrer; descriptions appear via the native title
  // attribute (tooltip); ArgoCD-sourced links carry a small "argocd" pill
  // so operators can tell which annotation family produced them.

  /** Builds a Deployment fixture with the supplied statusSummary.output. */
  function deploymentWithOutput(output: { url?: string; links?: unknown[] } | undefined) {
    return {
      ...mockDeployment,
      statusSummary: output
        ? {
            $typeName: 'holos.console.v1.DeploymentStatusSummary',
            phase: DeploymentPhase.RUNNING,
            readyReplicas: 1,
            desiredReplicas: 1,
            availableReplicas: 1,
            updatedReplicas: 1,
            observedGeneration: 0n,
            message: '',
            output: { $typeName: 'holos.console.v1.DeploymentOutput', url: output.url ?? '', links: output.links ?? [] },
          }
        : undefined,
    }
  }

  describe('Links section', () => {
    it('does not render the Links section when statusSummary.output is undefined', () => {
      setupMocks()
      ;(useGetDeployment as Mock).mockReturnValue({
        data: deploymentWithOutput(undefined),
        isPending: false,
        error: null,
      })
      render(<DeploymentDetailPage />)
      expect(screen.queryByTestId('deployment-links')).not.toBeInTheDocument()
    })

    it('does not render the Links section when output.links is empty', () => {
      setupMocks()
      ;(useGetDeployment as Mock).mockReturnValue({
        data: deploymentWithOutput({ url: '', links: [] }),
        isPending: false,
        error: null,
      })
      render(<DeploymentDetailPage />)
      expect(screen.queryByTestId('deployment-links')).not.toBeInTheDocument()
    })

    it('renders only the primary App URL when statusSummary has no links (backwards-compat)', () => {
      // Primary URL surfaces via the App URL row sourced from
      // statusSummary.output.url; the Links section stays hidden when
      // output.links is empty.
      setupMocks()
      ;(useGetDeployment as Mock).mockReturnValue({
        data: deploymentWithOutput({ url: 'https://app.example.com', links: [] }),
        isPending: false,
        error: null,
      })
      render(<DeploymentDetailPage />)
      expect(screen.getByTestId('deployment-output-url')).toBeInTheDocument()
      expect(screen.queryByTestId('deployment-links')).not.toBeInTheDocument()
      const appUrl = screen.getByRole('link', { name: /https:\/\/app\.example\.com/ })
      expect(appUrl.getAttribute('href')).toBe('https://app.example.com')
    })

    it('renders a single named link with anchor attributes', () => {
      setupMocks()
      ;(useGetDeployment as Mock).mockReturnValue({
        data: deploymentWithOutput({
          links: [
            {
              url: 'https://logs.example.com',
              title: 'Logs',
              description: 'Application log dashboard',
              source: 'holos',
              name: 'logs',
            },
          ],
        }),
        isPending: false,
        error: null,
      })
      render(<DeploymentDetailPage />)
      const link = screen.getByRole('link', { name: /^Logs$/ })
      expect(link.getAttribute('href')).toBe('https://logs.example.com')
      expect(link.getAttribute('target')).toBe('_blank')
      const rel = link.getAttribute('rel') ?? ''
      expect(rel).toContain('noopener')
      expect(rel).toContain('noreferrer')
    })

    it('exposes link description via the title attribute (tooltip trigger)', () => {
      setupMocks()
      ;(useGetDeployment as Mock).mockReturnValue({
        data: deploymentWithOutput({
          links: [
            {
              url: 'https://logs.example.com',
              title: 'Logs',
              description: 'Application log dashboard',
              source: 'holos',
              name: 'logs',
            },
          ],
        }),
        isPending: false,
        error: null,
      })
      render(<DeploymentDetailPage />)
      const link = screen.getByRole('link', { name: /^Logs$/ })
      // Tooltip trigger — the description is exposed via the native title
      // attribute so screen readers and bare-DOM consumers can reach it.
      expect(link.getAttribute('title')).toBe('Application log dashboard')
    })

    it('renders App URL row first, then Links section in backend order', () => {
      // The App URL row stays its own discrete row at the top with the
      // legacy `deployment-output-url` testid; the new Links section
      // sits below and walks `output.links` in the order the backend
      // supplied (the aggregator already sorts by (name, source)).
      setupMocks()
      ;(useGetDeployment as Mock).mockReturnValue({
        data: deploymentWithOutput({
          url: 'https://app.example.com',
          links: [
            { url: 'https://docs.example.com', title: 'Docs', description: '', source: 'holos', name: 'docs' },
            { url: 'https://logs.example.com', title: 'Logs', description: '', source: 'holos', name: 'logs' },
            { url: 'https://metrics.example.com', title: 'Metrics', description: '', source: 'argocd', name: 'metrics' },
          ],
        }),
        isPending: false,
        error: null,
      })
      render(<DeploymentDetailPage />)
      // App URL row still appears with the primary URL.
      expect(screen.getByTestId('deployment-output-url')).toBeInTheDocument()
      // Links section appears below with the secondary entries in
      // backend order; the primary URL is NOT duplicated inside Links.
      const section = screen.getByTestId('deployment-links')
      const anchors = Array.from(section.querySelectorAll('a'))
      const hrefs = anchors.map((a) => a.getAttribute('href'))
      expect(hrefs).toEqual([
        'https://docs.example.com',
        'https://logs.example.com',
        'https://metrics.example.com',
      ])
    })

    it('falls back to link.name when title is empty', () => {
      setupMocks()
      ;(useGetDeployment as Mock).mockReturnValue({
        data: deploymentWithOutput({
          links: [
            { url: 'https://x.example.com', title: '', description: '', source: 'argocd', name: 'metrics' },
          ],
        }),
        isPending: false,
        error: null,
      })
      render(<DeploymentDetailPage />)
      expect(screen.getByRole('link', { name: 'metrics' })).toBeInTheDocument()
    })

    it('falls back to URL host when title and name are both empty', () => {
      setupMocks()
      ;(useGetDeployment as Mock).mockReturnValue({
        data: deploymentWithOutput({
          links: [
            { url: 'https://nameless.example.com/path', title: '', description: '', source: 'holos', name: '' },
          ],
        }),
        isPending: false,
        error: null,
      })
      render(<DeploymentDetailPage />)
      expect(screen.getByRole('link', { name: 'nameless.example.com' })).toBeInTheDocument()
    })

    it('renders an "argocd" indicator for ArgoCD-sourced links', () => {
      setupMocks()
      ;(useGetDeployment as Mock).mockReturnValue({
        data: deploymentWithOutput({
          links: [
            { url: 'https://metrics.example.com', title: 'Metrics', description: '', source: 'argocd', name: 'metrics' },
            { url: 'https://logs.example.com', title: 'Logs', description: '', source: 'holos', name: 'logs' },
          ],
        }),
        isPending: false,
        error: null,
      })
      render(<DeploymentDetailPage />)
      // The argocd link row should carry an "argocd" pill; the holos one
      // should not.
      const section = screen.getByTestId('deployment-links')
      const argoRow = section.querySelector('[data-testid="deployment-link-row-metrics"]')
      const holosRow = section.querySelector('[data-testid="deployment-link-row-logs"]')
      expect(argoRow).not.toBeNull()
      expect(holosRow).not.toBeNull()
      expect(argoRow!.textContent).toContain('argocd')
      expect(holosRow!.textContent).not.toContain('argocd')
    })

    it('skips secondary links whose URL is unsafe', () => {
      setupMocks()
      ;(useGetDeployment as Mock).mockReturnValue({
        data: deploymentWithOutput({
          links: [
            { url: 'javascript:alert(1)', title: 'Bad', description: '', source: 'holos', name: 'bad' },
            { url: 'https://good.example.com', title: 'Good', description: '', source: 'holos', name: 'good' },
          ],
        }),
        isPending: false,
        error: null,
      })
      render(<DeploymentDetailPage />)
      expect(screen.queryByRole('link', { name: 'Bad' })).not.toBeInTheDocument()
      expect(screen.getByRole('link', { name: 'Good' })).toBeInTheDocument()
      // Verify the unsafe URL never reached an href.
      const anchors = document.querySelectorAll('a')
      anchors.forEach((a) => {
        expect(a.getAttribute('href')).not.toBe('javascript:alert(1)')
      })
    })
  })

  // HOL-559: drift badge + Reconcile action for the deployment detail page.
  // The detail page fetches PolicyState via useGetDeploymentPolicyState and
  // renders the shared PolicySection. Reconcile is gated on write
  // permission (OWNER / EDITOR); viewers see the drift signal but no
  // action. All RPCs are mocked.
  describe('policy drift section', () => {
    function setupDriftState(drift: boolean) {
      ;(useGetDeploymentPolicyState as Mock).mockReturnValue({
        data: {
          $typeName: 'holos.console.v1.PolicyState',
          appliedSet: [{ $typeName: 'holos.console.v1.LinkedTemplateRef', namespace: 'holos-org-acme', name: 'base', versionConstraint: '' }],
          currentSet: [{ $typeName: 'holos.console.v1.LinkedTemplateRef', namespace: 'holos-org-acme', name: 'base', versionConstraint: '' }],
          addedRefs: drift ? [{ $typeName: 'holos.console.v1.LinkedTemplateRef', namespace: 'holos-org-acme', name: 'sidecar', versionConstraint: '' }] : [],
          removedRefs: [],
          drift,
          hasAppliedState: true,
        },
        isPending: false,
        error: null,
      })
    }

    it('renders the Policy Drift badge when drift is true', () => {
      setupMocks(Role.OWNER)
      setupDriftState(true)
      render(<DeploymentDetailPage />)
      expect(screen.getByTestId('policy-drift-badge')).toBeInTheDocument()
    })

    it('does not render the Policy Drift badge when drift is false', () => {
      setupMocks(Role.OWNER)
      setupDriftState(false)
      render(<DeploymentDetailPage />)
      expect(screen.queryByTestId('policy-drift-badge')).not.toBeInTheDocument()
      expect(screen.getByTestId('policy-in-sync')).toBeInTheDocument()
    })

    it('renders the Reconcile button for owners when drift is true', () => {
      setupMocks(Role.OWNER)
      setupDriftState(true)
      render(<DeploymentDetailPage />)
      expect(screen.getByRole('button', { name: /reconcile policy drift/i })).toBeInTheDocument()
    })

    it('renders the Reconcile button for editors when drift is true', () => {
      setupMocks(Role.EDITOR)
      setupDriftState(true)
      render(<DeploymentDetailPage />)
      expect(screen.getByRole('button', { name: /reconcile policy drift/i })).toBeInTheDocument()
    })

    it('does not render the Reconcile button for viewers', () => {
      setupMocks(Role.VIEWER)
      setupDriftState(true)
      render(<DeploymentDetailPage />)
      // Viewers still see the drift badge — the read-only signal — but
      // never see the Reconcile action.
      expect(screen.getByTestId('policy-drift-badge')).toBeInTheDocument()
      expect(screen.queryByRole('button', { name: /reconcile policy drift/i })).not.toBeInTheDocument()
    })

    it('clicking Reconcile calls useUpdateDeployment with preserved fields', async () => {
      setupMocks(Role.OWNER)
      setupDriftState(true)
      render(<DeploymentDetailPage />)
      fireEvent.click(screen.getByRole('button', { name: /reconcile policy drift/i }))
      const mutateAsync = (useUpdateDeployment as Mock).mock.results[0].value.mutateAsync
      await waitFor(() => {
        expect(mutateAsync).toHaveBeenCalledWith(
          expect.objectContaining({
            image: mockDeployment.image,
            tag: mockDeployment.tag,
            port: mockDeployment.port,
          }),
        )
      })
    })

    it('Reconcile success shows a success toast', async () => {
      setupMocks(Role.OWNER)
      setupDriftState(true)
      const { toast } = await import('sonner')
      render(<DeploymentDetailPage />)
      fireEvent.click(screen.getByRole('button', { name: /reconcile policy drift/i }))
      await waitFor(() => {
        expect(toast.success).toHaveBeenCalledWith('Reconcile requested')
      })
    })

    it('Reconcile failure shows an error toast', async () => {
      setupMocks(Role.OWNER)
      setupDriftState(true)
      ;(useUpdateDeployment as Mock).mockReturnValue({
        mutateAsync: vi.fn().mockRejectedValue(new Error('boom')),
        isPending: false,
        reset: vi.fn(),
      })
      const { toast } = await import('sonner')
      render(<DeploymentDetailPage />)
      fireEvent.click(screen.getByRole('button', { name: /reconcile policy drift/i }))
      await waitFor(() => {
        expect(toast.error).toHaveBeenCalledWith('boom')
      })
    })

    it('Reconcile button is disabled while update mutation is pending', () => {
      setupMocks(Role.OWNER)
      setupDriftState(true)
      ;(useUpdateDeployment as Mock).mockReturnValue({
        mutateAsync: vi.fn(),
        isPending: true,
        reset: vi.fn(),
      })
      render(<DeploymentDetailPage />)
      const btn = screen.getByRole('button', { name: /reconcile policy drift/i })
      expect(btn).toBeDisabled()
    })
  })

  // ── Preview tab tests (HOL-971) ─────────────────────────────────────────
  //
  // The Preview tab shows the rendered Kubernetes manifests from
  // GetDeploymentRenderPreview. platformResourcesYaml surfaces resources
  // contributed by organisation/folder-level templates via
  // TemplatePolicyBinding. projectResourcesYaml surfaces the deployment
  // template's own project resources.

  describe('Preview tab', () => {
    const mockRenderPreview = {
      $typeName: 'holos.console.v1.GetDeploymentRenderPreviewResponse' as const,
      cueTemplate: '// template',
      cuePlatformInput: '// platform',
      cueProjectInput: '// project',
      renderedYaml: 'apiVersion: v1\nkind: Service',
      renderedJson: '[]',
      platformResourcesYaml: 'apiVersion: networking.k8s.io/v1\nkind: NetworkPolicy\nmetadata:\n  name: platform-policy',
      platformResourcesJson: '[]',
      projectResourcesYaml: 'apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: api',
      projectResourcesJson: '[]',
      defaultsJson: '{}',
    }

    it('renders a Preview tab trigger', () => {
      setupMocks()
      render(<DeploymentDetailPage />)
      expect(screen.getByRole('tab', { name: /preview/i })).toBeInTheDocument()
    })

    it('does not activate the Preview tab by default', () => {
      setupMocks()
      render(<DeploymentDetailPage />)
      const previewTab = screen.getByRole('tab', { name: /preview/i })
      expect(previewTab).toHaveAttribute('data-state', 'inactive')
    })

    it('activates the Preview tab when URL has ?tab=preview', () => {
      mockUseSearch.mockReturnValue({ tab: 'preview' })
      setupMocks()
      ;(useGetDeploymentRenderPreview as Mock).mockReturnValue({
        data: mockRenderPreview,
        isPending: false,
        error: null,
      })
      render(<DeploymentDetailPage />)
      const previewTab = screen.getByRole('tab', { name: /preview/i })
      expect(previewTab).toHaveAttribute('data-state', 'active')
    })

    it('renders platform resources YAML when preview data is available', async () => {
      const user = userEvent.setup()
      setupMocks()
      ;(useGetDeploymentRenderPreview as Mock).mockReturnValue({
        data: mockRenderPreview,
        isPending: false,
        error: null,
      })
      render(<DeploymentDetailPage />)
      await user.click(screen.getByRole('tab', { name: /preview/i }))
      expect(screen.getByTestId('preview-platform-resources')).toBeInTheDocument()
      expect(screen.getByText(/NetworkPolicy/)).toBeInTheDocument()
    })

    it('renders project resources YAML when preview data is available', async () => {
      const user = userEvent.setup()
      setupMocks()
      ;(useGetDeploymentRenderPreview as Mock).mockReturnValue({
        data: mockRenderPreview,
        isPending: false,
        error: null,
      })
      render(<DeploymentDetailPage />)
      await user.click(screen.getByRole('tab', { name: /preview/i }))
      expect(screen.getByTestId('preview-project-resources')).toBeInTheDocument()
      expect(screen.getByText(/kind: Deployment/)).toBeInTheDocument()
    })

    it('renders empty platform resources message when platformResourcesYaml is empty', async () => {
      const user = userEvent.setup()
      setupMocks()
      ;(useGetDeploymentRenderPreview as Mock).mockReturnValue({
        data: { ...mockRenderPreview, platformResourcesYaml: '' },
        isPending: false,
        error: null,
      })
      render(<DeploymentDetailPage />)
      await user.click(screen.getByRole('tab', { name: /preview/i }))
      const emptyMsg = screen.getByTestId('preview-platform-resources-empty')
      expect(emptyMsg).toBeInTheDocument()
      expect(emptyMsg.textContent).toContain('TemplatePolicyBinding')
    })

    it('renders loading skeleton while preview is pending', async () => {
      const user = userEvent.setup()
      setupMocks()
      ;(useGetDeploymentRenderPreview as Mock).mockReturnValue({
        data: undefined,
        isPending: true,
        error: null,
      })
      render(<DeploymentDetailPage />)
      await user.click(screen.getByRole('tab', { name: /preview/i }))
      expect(screen.getByTestId('preview-loading')).toBeInTheDocument()
    })

    it('renders error when preview fetch fails', async () => {
      const user = userEvent.setup()
      setupMocks()
      ;(useGetDeploymentRenderPreview as Mock).mockReturnValue({
        data: undefined,
        isPending: false,
        error: new Error('preview fetch failed'),
      })
      render(<DeploymentDetailPage />)
      await user.click(screen.getByRole('tab', { name: /preview/i }))
      expect(screen.getByText(/preview fetch failed/i)).toBeInTheDocument()
    })

    it('calls useGetDeploymentRenderPreview with project and deployment name', () => {
      setupMocks()
      render(<DeploymentDetailPage />)
      expect(useGetDeploymentRenderPreview).toHaveBeenCalledWith('test-project', 'api')
    })
  })
})

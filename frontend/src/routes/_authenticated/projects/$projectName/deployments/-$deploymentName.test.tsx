import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

// Default useSearch stub returns 'status' tab
const mockUseSearch = vi.fn(() => ({ tab: 'status' as 'status' | 'logs' | 'template' }))
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
  useGetDeploymentRenderPreview: vi.fn(),
}))

vi.mock('@/queries/projects', () => ({
  useGetProject: vi.fn(),
}))

vi.mock('@/queries/templates', () => ({
  useRenderTemplate: vi.fn(),
  makeProjectScope: vi.fn().mockReturnValue({ scope: 1, scopeName: 'test-project' }),
}))

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

import { useGetDeployment, useGetDeploymentStatus, useGetDeploymentLogs, useUpdateDeployment, useDeleteDeployment, useListNamespaceSecrets, useListNamespaceConfigMaps, useGetDeploymentRenderPreview } from '@/queries/deployments'
import { useGetProject } from '@/queries/projects'
import { useRenderTemplate } from '@/queries/templates'
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

const mockPreview = {
  cueTemplate: 'input: #ProjectInput\nplatform: #PlatformInput\n',
  cuePlatformInput: 'platform: {\n  project: "test-project"\n  namespace: "holos-prj-test-project"\n}',
  cueProjectInput: 'input: {\n  name: "api"\n  image: "ghcr.io/org/api"\n  tag: "v1.2.3"\n  port: 8080\n}',
  renderedYaml: 'apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: api\n',
  renderedJson: '[]',
  output: undefined as { url: string } | undefined,
}

function setupMocks(userRole = Role.OWNER) {
  ;(useGetDeployment as Mock).mockReturnValue({ data: mockDeployment, isPending: false, error: null })
  ;(useGetDeploymentStatus as Mock).mockReturnValue({ data: mockStatus, isPending: false, error: null })
  ;(useGetDeploymentLogs as Mock).mockReturnValue({ data: mockLogs, isPending: false, error: null })
  ;(useUpdateDeployment as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({}), isPending: false, reset: vi.fn() })
  ;(useDeleteDeployment as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({}), isPending: false, error: null, reset: vi.fn() })
  ;(useGetProject as Mock).mockReturnValue({ data: { name: 'test-project', userRole }, isLoading: false })
  ;(useListNamespaceSecrets as Mock).mockReturnValue({ data: [], isLoading: false })
  ;(useListNamespaceConfigMaps as Mock).mockReturnValue({ data: [], isLoading: false })
  ;(useGetDeploymentRenderPreview as Mock).mockReturnValue({ data: mockPreview, isPending: false, error: null })
  ;(useRenderTemplate as Mock).mockReturnValue({ data: { renderedYaml: mockPreview.renderedYaml, renderedJson: '' }, error: null, isFetching: false })
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
    ;(useGetDeploymentRenderPreview as Mock).mockReturnValue({ data: mockPreview, isPending: false, error: null })
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

  it('switches to Template tab when clicked and shows CUE editor', async () => {
    const user = userEvent.setup()
    setupMocks()
    render(<DeploymentDetailPage />)
    await user.click(screen.getByRole('tab', { name: /template/i }))
    const editor = screen.getByLabelText('CUE Template') as HTMLTextAreaElement
    expect(editor).toBeInTheDocument()
    expect(editor.readOnly).toBe(true)
    expect(editor.value).toContain('#ProjectInput')
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

    // Visible on Template tab
    await user.click(screen.getByRole('tab', { name: /template/i }))
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

  it('activates Template tab when URL has ?tab=template', () => {
    mockUseSearch.mockReturnValue({ tab: 'template' })
    setupMocks()
    render(<DeploymentDetailPage />)
    const templateTab = screen.getByRole('tab', { name: /template/i })
    expect(templateTab).toHaveAttribute('data-state', 'active')
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

  describe('Template tab section', () => {
    beforeEach(() => {
      setupMocks()
    })

    it('renders Template tab trigger', () => {
      render(<DeploymentDetailPage />)
      expect(screen.getByRole('tab', { name: /template/i })).toBeInTheDocument()
    })

    it('renders CUE template source in read-only editor after clicking Template tab', async () => {
      const user = userEvent.setup()
      render(<DeploymentDetailPage />)
      await user.click(screen.getByRole('tab', { name: /template/i }))
      const editor = screen.getByLabelText('CUE Template') as HTMLTextAreaElement
      expect(editor).toBeInTheDocument()
      expect(editor.readOnly).toBe(true)
      expect(editor.value).toContain('#ProjectInput')
    })

    it('renders API Access section after clicking Template tab', async () => {
      const user = userEvent.setup()
      render(<DeploymentDetailPage />)
      await user.click(screen.getByRole('tab', { name: /template/i }))
      expect(screen.getByText('API Access')).toBeInTheDocument()
    })

    it('renders a working curl command using HOLOS_ID_TOKEN after clicking Template tab', async () => {
      const user = userEvent.setup()
      render(<DeploymentDetailPage />)
      await user.click(screen.getByRole('tab', { name: /template/i }))
      const curl = screen.getByText(/curl -s --cacert/)
      expect(curl.textContent).toContain('Connect-Protocol-Version: 1')
      expect(curl.textContent).toContain('$HOLOS_ID_TOKEN')
      expect(curl.textContent).toContain('holos.console.v1.DeploymentService/GetDeploymentRenderPreview')
      expect(curl.textContent).toContain('"project": "test-project"')
      expect(curl.textContent).toContain('"name": "api"')
      expect(curl.textContent).not.toContain('-k')
    })

    it('renders a grpcurl command using HOLOS_ID_TOKEN after clicking Template tab', async () => {
      const user = userEvent.setup()
      render(<DeploymentDetailPage />)
      await user.click(screen.getByRole('tab', { name: /template/i }))
      const grpcurl = screen.getByText(/grpcurl -cacert/)
      expect(grpcurl.textContent).toContain('$HOLOS_ID_TOKEN')
      expect(grpcurl.textContent).toContain('holos.console.v1.DeploymentService/GetDeploymentRenderPreview')
      expect(grpcurl.textContent).not.toContain('-insecure')
    })

    it('does not render the broken grpcurl -plaintext form', async () => {
      const user = userEvent.setup()
      render(<DeploymentDetailPage />)
      await user.click(screen.getByRole('tab', { name: /template/i }))
      expect(screen.queryByText(/grpcurl -plaintext/)).not.toBeInTheDocument()
    })

    it('copy button for curl command copies correct command after clicking Template tab', async () => {
      const writeText = vi.fn().mockResolvedValue(undefined)
      // userEvent intercepts clipboard; wire up the spy on the userEvent clipboard mock
      const user = userEvent.setup({ writeToClipboard: true })
      Object.defineProperty(navigator, 'clipboard', {
        value: { writeText },
        configurable: true,
      })
      render(<DeploymentDetailPage />)
      await user.click(screen.getByRole('tab', { name: /template/i }))
      await user.click(screen.getByLabelText(/copy curl command/i))
      await waitFor(() => expect(writeText).toHaveBeenCalled())
      const copied = writeText.mock.calls[0][0] as string
      expect(copied).toContain('Connect-Protocol-Version: 1')
      expect(copied).toContain('$HOLOS_ID_TOKEN')
      expect(copied).toContain('GetDeploymentRenderPreview')
      expect(copied).toContain('test-project')
      expect(copied).toContain('api')
    })

    it('copy button for grpcurl command copies correct command after clicking Template tab', async () => {
      const writeText = vi.fn().mockResolvedValue(undefined)
      const user = userEvent.setup({ writeToClipboard: true })
      Object.defineProperty(navigator, 'clipboard', {
        value: { writeText },
        configurable: true,
      })
      render(<DeploymentDetailPage />)
      await user.click(screen.getByRole('tab', { name: /template/i }))
      await user.click(screen.getByLabelText(/copy grpcurl command/i))
      await waitFor(() => expect(writeText).toHaveBeenCalled())
      const copied = writeText.mock.calls[0][0] as string
      expect(copied).toContain('-cacert')
      expect(copied).not.toContain('-insecure')
      expect(copied).toContain('$HOLOS_ID_TOKEN')
      expect(copied).toContain('GetDeploymentRenderPreview')
      expect(copied).toContain('test-project')
      expect(copied).toContain('api')
    })

    it('shows skeleton while preview is loading after clicking Template tab', async () => {
      const user = userEvent.setup()
      ;(useGetDeployment as Mock).mockReturnValue({ data: mockDeployment, isPending: false, error: null })
      ;(useGetDeploymentStatus as Mock).mockReturnValue({ data: mockStatus, isPending: false, error: null })
      ;(useGetDeploymentLogs as Mock).mockReturnValue({ data: mockLogs, isPending: false, error: null })
      ;(useUpdateDeployment as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false, reset: vi.fn() })
      ;(useDeleteDeployment as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false, error: null, reset: vi.fn() })
      ;(useGetProject as Mock).mockReturnValue({ data: { name: 'test-project', userRole: Role.OWNER }, isLoading: false })
      ;(useListNamespaceSecrets as Mock).mockReturnValue({ data: [], isLoading: false })
      ;(useListNamespaceConfigMaps as Mock).mockReturnValue({ data: [], isLoading: false })
      ;(useGetDeploymentRenderPreview as Mock).mockReturnValue({ data: undefined, isPending: true, error: null })
      render(<DeploymentDetailPage />)
      await user.click(screen.getByRole('tab', { name: /template/i }))
      const skeletons = document.querySelectorAll('[data-slot="skeleton"]')
      expect(skeletons.length).toBeGreaterThan(0)
    })
  })

  describe('Template tab per-collection resource display', () => {
    it('shows both platform and project resource sections when per-collection fields are present', async () => {
      const user = userEvent.setup()
      setupMocks()
      ;(useRenderTemplate as Mock).mockReturnValue({
        data: {
          renderedYaml: 'unified-yaml',
          renderedJson: '',
          platformResourcesYaml: 'apiVersion: v1\nkind: ReferenceGrant',
          platformResourcesJson: '',
          projectResourcesYaml: 'apiVersion: apps/v1\nkind: Deployment',
          projectResourcesJson: '',
        },
        error: null,
        isFetching: false,
      })
      render(<DeploymentDetailPage />)
      await user.click(screen.getByRole('tab', { name: /template/i }))
      // Switch to preview sub-tab inside CueTemplateEditor
      await user.click(screen.getByRole('tab', { name: /preview/i }))

      expect(screen.getByText('Platform Resources')).toBeInTheDocument()
      expect(screen.getByText('Project Resources')).toBeInTheDocument()
      expect(screen.getByLabelText('Platform Resources YAML')).toHaveTextContent('ReferenceGrant')
      expect(screen.getByLabelText('Project Resources YAML')).toHaveTextContent('Deployment')
    })

    it('shows empty-state message when platform resources are empty but project resources exist', async () => {
      const user = userEvent.setup()
      setupMocks()
      ;(useRenderTemplate as Mock).mockReturnValue({
        data: {
          renderedYaml: 'unified-yaml',
          renderedJson: '',
          platformResourcesYaml: '',
          platformResourcesJson: '',
          projectResourcesYaml: 'apiVersion: apps/v1\nkind: Service',
          projectResourcesJson: '',
        },
        error: null,
        isFetching: false,
      })
      render(<DeploymentDetailPage />)
      await user.click(screen.getByRole('tab', { name: /template/i }))
      await user.click(screen.getByRole('tab', { name: /preview/i }))

      expect(screen.getByText('Platform Resources')).toBeInTheDocument()
      expect(screen.getByText('Project Resources')).toBeInTheDocument()
      expect(screen.getByText('No platform resources rendered by this template.')).toBeInTheDocument()
      expect(screen.getByLabelText('Project Resources YAML')).toHaveTextContent('Service')
    })

    it('falls back to unified renderedYaml when no per-collection fields present', async () => {
      const user = userEvent.setup()
      setupMocks()
      ;(useRenderTemplate as Mock).mockReturnValue({
        data: { renderedYaml: 'apiVersion: v1\nkind: ConfigMap', renderedJson: '' },
        error: null,
        isFetching: false,
      })
      render(<DeploymentDetailPage />)
      await user.click(screen.getByRole('tab', { name: /template/i }))
      await user.click(screen.getByRole('tab', { name: /preview/i }))

      expect(screen.getByText('Rendered YAML')).toBeInTheDocument()
      expect(screen.getByLabelText('Rendered YAML')).toHaveTextContent('ConfigMap')
      expect(screen.queryByText('Platform Resources')).not.toBeInTheDocument()
    })
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
      ;(useGetDeploymentRenderPreview as Mock).mockReturnValue({ data: mockPreview, isPending: false, error: null })
      ;(useRenderTemplate as Mock).mockReturnValue({ data: { renderedYaml: '', renderedJson: '' }, error: null, isFetching: false })
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
      ;(useGetDeploymentRenderPreview as Mock).mockReturnValue({ data: mockPreview, isPending: false, error: null })
      ;(useRenderTemplate as Mock).mockReturnValue({ data: { renderedYaml: '', renderedJson: '' }, error: null, isFetching: false })
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
      ;(useGetDeploymentRenderPreview as Mock).mockReturnValue({ data: mockPreview, isPending: false, error: null })
      ;(useRenderTemplate as Mock).mockReturnValue({ data: { renderedYaml: '', renderedJson: '' }, error: null, isFetching: false })
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
      ;(useGetDeploymentRenderPreview as Mock).mockReturnValue({ data: mockPreview, isPending: false, error: null })
      ;(useRenderTemplate as Mock).mockReturnValue({ data: { renderedYaml: '', renderedJson: '' }, error: null, isFetching: false })
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
      ;(useGetDeploymentRenderPreview as Mock).mockReturnValue({ data: mockPreview, isPending: false, error: null })
      ;(useRenderTemplate as Mock).mockReturnValue({ data: { renderedYaml: '', renderedJson: '' }, error: null, isFetching: false })
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
      ;(useGetDeploymentRenderPreview as Mock).mockReturnValue({ data: mockPreview, isPending: false, error: null })
      ;(useRenderTemplate as Mock).mockReturnValue({ data: { renderedYaml: '', renderedJson: '' }, error: null, isFetching: false })
      render(<DeploymentDetailPage />)
      expect(screen.getByText('api-pod-1')).toBeInTheDocument()
      expect(screen.getByText('api-pod-2')).toBeInTheDocument()
      expect(screen.getByText('CrashLoopBackOff')).toBeInTheDocument()
    })
  })

  // ── Output URL row tests ─────────────────────────────────────────────────
  //
  // The Status tab surfaces the template-authored deployment URL from the
  // render preview response (`output.url`). The URL row is visible when the
  // preview has resolved and `output.url` is a non-empty string; otherwise
  // nothing is rendered. This mirrors the acceptance criteria on HOL-546.

  describe('Output URL row', () => {
    it('renders an App URL link when preview.output.url is a non-empty string', async () => {
      setupMocks()
      ;(useGetDeploymentRenderPreview as Mock).mockReturnValue({
        data: { ...mockPreview, output: { url: 'https://example.com/app' } },
        isPending: false,
        error: null,
      })
      render(<DeploymentDetailPage />)
      const link = await screen.findByRole('link', { name: /https:\/\/example\.com\/app/ })
      expect(link).toBeInTheDocument()
      expect(link.getAttribute('href')).toBe('https://example.com/app')
      expect(link.getAttribute('target')).toBe('_blank')
      const rel = link.getAttribute('rel') ?? ''
      expect(rel).toContain('noopener')
      expect(rel).toContain('noreferrer')
      expect(screen.getByText(/^App URL$/i)).toBeInTheDocument()
    })

    it('does not render the App URL row when preview.output is undefined', () => {
      setupMocks()
      ;(useGetDeploymentRenderPreview as Mock).mockReturnValue({
        data: { ...mockPreview, output: undefined },
        isPending: false,
        error: null,
      })
      render(<DeploymentDetailPage />)
      expect(screen.queryByTestId('deployment-output-url')).not.toBeInTheDocument()
      expect(screen.queryByText(/^App URL$/i)).not.toBeInTheDocument()
    })

    it('does not render the App URL row when preview.output.url is an empty string', () => {
      setupMocks()
      ;(useGetDeploymentRenderPreview as Mock).mockReturnValue({
        data: { ...mockPreview, output: { url: '' } },
        isPending: false,
        error: null,
      })
      render(<DeploymentDetailPage />)
      expect(screen.queryByTestId('deployment-output-url')).not.toBeInTheDocument()
      expect(screen.queryByText(/^App URL$/i)).not.toBeInTheDocument()
    })

    it('does not render the App URL row while the preview query is pending', () => {
      setupMocks()
      ;(useGetDeploymentRenderPreview as Mock).mockReturnValue({
        data: undefined,
        isPending: true,
        error: null,
      })
      render(<DeploymentDetailPage />)
      expect(screen.queryByTestId('deployment-output-url')).not.toBeInTheDocument()
      expect(screen.queryByText(/^App URL$/i)).not.toBeInTheDocument()
    })
  })
})

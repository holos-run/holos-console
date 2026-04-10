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

vi.mock('@/queries/deployment-templates', () => ({
  useRenderDeploymentTemplate: vi.fn(),
}))

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

import { useGetDeployment, useGetDeploymentStatus, useGetDeploymentLogs, useUpdateDeployment, useDeleteDeployment, useListNamespaceSecrets, useListNamespaceConfigMaps, useGetDeploymentRenderPreview } from '@/queries/deployments'
import { useGetProject } from '@/queries/projects'
import { useRenderDeploymentTemplate } from '@/queries/deployment-templates'
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
    { name: 'api-7d8f9b6c4-xk2m3', phase: 'Running', ready: true, restartCount: 0 },
  ],
}

const mockLogs = '2024-01-15T10:30:01Z Starting server...\n2024-01-15T10:30:02Z Listening on :8080'

const mockPreview = {
  cueTemplate: 'input: #ProjectInput\nplatform: #PlatformInput\n',
  cuePlatformInput: 'platform: {\n  project: "test-project"\n  namespace: "holos-prj-test-project"\n}',
  cueProjectInput: 'input: {\n  name: "api"\n  image: "ghcr.io/org/api"\n  tag: "v1.2.3"\n  port: 8080\n}',
  renderedYaml: 'apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: api\n',
  renderedJson: '[]',
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
  ;(useRenderDeploymentTemplate as Mock).mockReturnValue({ data: { renderedYaml: mockPreview.renderedYaml, renderedJson: '' }, error: null, isFetching: false })
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
})

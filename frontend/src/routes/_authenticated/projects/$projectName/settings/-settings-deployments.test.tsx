import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => ({ projectName: 'test-project' }),
    }),
    useNavigate: () => vi.fn(),
  }
})

vi.mock('@/queries/projects', () => ({
  useGetProject: vi.fn(),
  useUpdateProject: vi.fn(),
  useUpdateProjectSharing: vi.fn(),
  useUpdateProjectDefaultSharing: vi.fn(),
  useDeleteProject: vi.fn(),
}))

vi.mock('@/queries/project-settings', () => ({
  useGetProjectSettings: vi.fn(),
  useGetProjectSettingsRaw: vi.fn(),
  useUpdateProjectSettings: vi.fn(),
}))

vi.mock('@/queries/organizations', () => ({
  useGetOrganization: vi.fn(),
}))

vi.mock('@/lib/auth', () => ({ useAuth: vi.fn() }))
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

import { useGetProject, useUpdateProject, useUpdateProjectSharing, useUpdateProjectDefaultSharing, useDeleteProject } from '@/queries/projects'
import { useGetProjectSettings, useGetProjectSettingsRaw, useUpdateProjectSettings } from '@/queries/project-settings'
import { useGetOrganization } from '@/queries/organizations'
import { useAuth } from '@/lib/auth'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { ProjectSettingsPage } from './index'

const mockProject = {
  name: 'test-project',
  displayName: 'Test Project',
  description: 'A test project',
  organization: 'my-org',
  userGrants: [{ principal: 'alice@example.com', role: Role.OWNER }],
  roleGrants: [],
  defaultUserGrants: [],
  defaultRoleGrants: [],
  userRole: Role.OWNER,
}

function setupMocks(overrides: {
  projectOverrides?: Partial<typeof mockProject>
  deploymentsEnabled?: boolean
  orgUserRole?: number
} = {}) {
  const project = { ...mockProject, ...overrides.projectOverrides }
  ;(useGetProject as Mock).mockReturnValue({ data: project, isPending: false, error: null })
  ;(useUpdateProject as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({}), isPending: false })
  ;(useUpdateProjectSharing as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({}), isPending: false })
  ;(useUpdateProjectDefaultSharing as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({}), isPending: false })
  ;(useDeleteProject as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({}), isPending: false, error: null })
  ;(useGetProjectSettings as Mock).mockReturnValue({
    data: { project: 'test-project', deploymentsEnabled: overrides.deploymentsEnabled ?? false },
    isPending: false,
    error: null,
  })
  ;(useGetProjectSettingsRaw as Mock).mockReturnValue({
    data: '{"apiVersion":"v1","kind":"Namespace","metadata":{"name":"prj-test-project"}}',
    isPending: false,
    error: null,
  })
  ;(useUpdateProjectSettings as Mock).mockReturnValue({
    mutateAsync: vi.fn().mockResolvedValue({}),
    isPending: false,
  })
  ;(useGetOrganization as Mock).mockReturnValue({
    data: { name: 'my-org', userRole: overrides.orgUserRole ?? Role.OWNER },
    isPending: false,
    error: null,
  })
  ;(useAuth as Mock).mockReturnValue({
    isAuthenticated: true,
    isLoading: false,
    user: { profile: { email: 'alice@example.com', groups: [] } },
  })
}

describe('ProjectSettingsPage -- Features section', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders Features section with Deployments toggle', () => {
    setupMocks()
    render(<ProjectSettingsPage />)
    expect(screen.getByText('Features')).toBeInTheDocument()
    expect(screen.getByText('Deployments')).toBeInTheDocument()
  })

  it('renders toggle in off state when deploymentsEnabled is false', () => {
    setupMocks({ deploymentsEnabled: false })
    render(<ProjectSettingsPage />)
    const toggle = screen.getByRole('switch', { name: /deployments/i })
    expect(toggle).toBeInTheDocument()
    expect(toggle).toHaveAttribute('data-state', 'unchecked')
  })

  it('renders toggle in on state when deploymentsEnabled is true', () => {
    setupMocks({ deploymentsEnabled: true })
    render(<ProjectSettingsPage />)
    const toggle = screen.getByRole('switch', { name: /deployments/i })
    expect(toggle).toHaveAttribute('data-state', 'checked')
  })

  it('clicking toggle calls useUpdateProjectSettings mutation', async () => {
    setupMocks({ deploymentsEnabled: false })
    render(<ProjectSettingsPage />)
    const toggle = screen.getByRole('switch', { name: /deployments/i })
    fireEvent.click(toggle)
    const mutateAsync = (useUpdateProjectSettings as Mock).mock.results[0].value.mutateAsync
    await waitFor(() => {
      expect(mutateAsync).toHaveBeenCalledWith({ deploymentsEnabled: true })
    })
  })

  it('toggle is disabled when user is not org-level owner', () => {
    setupMocks({ orgUserRole: Role.EDITOR })
    render(<ProjectSettingsPage />)
    const toggle = screen.getByRole('switch', { name: /deployments/i })
    expect(toggle).toBeDisabled()
  })

  it('toggle is disabled when user is org-level viewer', () => {
    setupMocks({ orgUserRole: Role.VIEWER })
    render(<ProjectSettingsPage />)
    const toggle = screen.getByRole('switch', { name: /deployments/i })
    expect(toggle).toBeDisabled()
  })

  it('toggle is enabled when user is org-level owner', () => {
    setupMocks({ orgUserRole: Role.OWNER })
    render(<ProjectSettingsPage />)
    const toggle = screen.getByRole('switch', { name: /deployments/i })
    expect(toggle).not.toBeDisabled()
  })

  it('toggle is disabled when org data is not available', () => {
    setupMocks()
    ;(useGetOrganization as Mock).mockReturnValue({ data: undefined, isPending: true, error: null })
    render(<ProjectSettingsPage />)
    const toggle = screen.getByRole('switch', { name: /deployments/i })
    expect(toggle).toBeDisabled()
  })
})

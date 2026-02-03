import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { TransportProvider } from '@connectrpc/connect-query'
import { createRouterTransport, ConnectError, Code } from '@connectrpc/connect'
import { create } from '@bufbuild/protobuf'
import { ProjectPage } from './ProjectPage'
import { AuthContext, type AuthContextValue } from '../auth'
import type { User } from 'oidc-client-ts'
import { vi } from 'vitest'
import { Role } from '../gen/holos/console/v1/rbac_pb'
import {
  DeleteProjectResponseSchema,
  GetProjectRawResponseSchema,
  GetProjectResponseSchema,
  ListProjectsResponseSchema,
  ProjectSchema,
  ProjectService,
  UpdateProjectResponseSchema,
  UpdateProjectSharingResponseSchema,
} from '../gen/holos/console/v1/projects_pb.js'
import type { Transport } from '@connectrpc/connect'

function createMockUser(profile: Record<string, unknown>): User {
  return {
    profile: {
      sub: 'test-subject',
      name: 'Test User',
      email: 'test@example.com',
      email_verified: true,
      ...profile,
    },
    id_token: 'mock-id-token',
    access_token: 'mock-access-token',
    token_type: 'Bearer',
    expired: false,
  } as User
}

function createAuthContext(overrides: Partial<AuthContextValue> = {}): AuthContextValue {
  return {
    user: null,
    isLoading: false,
    error: null,
    isAuthenticated: false,
    login: vi.fn(),
    logout: vi.fn(),
    getAccessToken: vi.fn(() => 'mock-access-token'),
    refreshTokens: vi.fn(),
    lastRefreshStatus: 'idle',
    lastRefreshTime: null,
    lastRefreshError: null,
    ...overrides,
  }
}

function createProjectTransport(project: {
  name: string
  displayName: string
  description: string
  userRole: Role
  userGrants?: Array<{ principal: string; role: Role }>
  roleGrants?: Array<{ principal: string; role: Role }>
  organization?: string
}, rawJson?: string) {
  return createRouterTransport(({ service }) => {
    service(ProjectService, {
      listProjects: () => create(ListProjectsResponseSchema, { projects: [] }),
      getProject: () =>
        create(GetProjectResponseSchema, {
          project: create(ProjectSchema, {
            ...project,
            userGrants: project.userGrants ?? [],
            roleGrants: project.roleGrants ?? [],
            organization: project.organization ?? '',
          }),
        }),
      deleteProject: () => create(DeleteProjectResponseSchema),
      updateProject: () => create(UpdateProjectResponseSchema),
      updateProjectSharing: () => create(UpdateProjectSharingResponseSchema),
      getProjectRaw: () => create(GetProjectRawResponseSchema, { raw: rawJson ?? '' }),
    })
  })
}

function renderProjectPage(authValue: AuthContextValue, projectName = 'test-project', transport?: Transport) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  })
  const t = transport ?? createProjectTransport({
    name: projectName,
    displayName: '',
    description: '',
    userRole: Role.VIEWER,
  })
  return render(
    <TransportProvider transport={t}>
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={[`/projects/${projectName}`]}>
          <AuthContext.Provider value={authValue}>
            <Routes>
              <Route path="/projects/:projectName" element={<ProjectPage />} />
              <Route path="/projects" element={<div>Projects List</div>} />
            </Routes>
          </AuthContext.Provider>
        </MemoryRouter>
      </QueryClientProvider>
    </TransportProvider>,
  )
}

describe('ProjectPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('project display', () => {
    it('renders project metadata', async () => {
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      const transport = createProjectTransport({
        name: 'prod',
        displayName: 'Production',
        description: 'Production environment',
        userGrants: [{ principal: 'test@example.com', role: Role.OWNER }],
        userRole: Role.OWNER,
      })

      renderProjectPage(authValue, 'prod', transport)

      await waitFor(() => {
        expect(screen.getByText('Production')).toBeInTheDocument()
        expect(screen.getByText('Production environment')).toBeInTheDocument()
        expect(screen.getByText('prod')).toBeInTheDocument()
      })
    })

    it('shows "No description" when description is empty', async () => {
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      const transport = createProjectTransport({
        name: 'prod',
        displayName: 'Production',
        description: '',
        userRole: Role.VIEWER,
      })

      renderProjectPage(authValue, 'prod', transport)

      await waitFor(() => {
        expect(screen.getByText('No description')).toBeInTheDocument()
      })
    })

    it('shows edit buttons for editor/owner roles', async () => {
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      const transport = createProjectTransport({
        name: 'prod',
        displayName: 'Production',
        description: 'Desc',
        userRole: Role.EDITOR,
      })

      renderProjectPage(authValue, 'prod', transport)

      await waitFor(() => {
        expect(screen.getByLabelText('edit display name')).toBeInTheDocument()
        expect(screen.getByLabelText('edit description')).toBeInTheDocument()
      })
    })

    it('hides edit buttons for viewer role', async () => {
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      const transport = createProjectTransport({
        name: 'prod',
        displayName: 'Production',
        description: 'Desc',
        userRole: Role.VIEWER,
      })

      renderProjectPage(authValue, 'prod', transport)

      await waitFor(() => {
        expect(screen.getByText('Production')).toBeInTheDocument()
      })

      expect(screen.queryByLabelText('edit display name')).not.toBeInTheDocument()
      expect(screen.queryByLabelText('edit description')).not.toBeInTheDocument()
    })

    it('shows delete button for owners only', async () => {
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      const transport = createProjectTransport({
        name: 'prod',
        displayName: 'Production',
        description: '',
        userRole: Role.OWNER,
      })

      renderProjectPage(authValue, 'prod', transport)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /delete/i })).toBeInTheDocument()
      })
    })

    it('hides delete button for non-owner roles', async () => {
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      const transport = createProjectTransport({
        name: 'prod',
        displayName: 'Production',
        description: '',
        userRole: Role.EDITOR,
      })

      renderProjectPage(authValue, 'prod', transport)

      await waitFor(() => {
        expect(screen.getByText('Production')).toBeInTheDocument()
      })

      expect(screen.queryByRole('button', { name: /delete/i })).not.toBeInTheDocument()
    })
  })

  describe('raw view toggle', () => {
    it('shows Editor/Raw toggle buttons', async () => {
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      const transport = createProjectTransport({
        name: 'prod',
        displayName: 'Production',
        description: 'Prod env',
        userRole: Role.VIEWER,
      })

      renderProjectPage(authValue, 'prod', transport)

      await waitFor(() => {
        expect(screen.getByText('Production')).toBeInTheDocument()
      })

      expect(screen.getByRole('button', { name: /editor/i })).toBeInTheDocument()
      expect(screen.getByRole('button', { name: /raw/i })).toBeInTheDocument()
    })

    it('fetches and displays raw JSON when Raw toggle is clicked', async () => {
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      const rawJson = JSON.stringify({
        apiVersion: 'v1',
        kind: 'Namespace',
        metadata: { name: 'prj-prod' },
      })

      const transport = createProjectTransport({
        name: 'prod',
        displayName: 'Production',
        description: 'Prod env',
        userRole: Role.VIEWER,
      }, rawJson)

      renderProjectPage(authValue, 'prod', transport)

      await waitFor(() => {
        expect(screen.getByText('Production')).toBeInTheDocument()
      })

      fireEvent.click(screen.getByRole('button', { name: /raw/i }))

      await waitFor(() => {
        const pre = screen.getByRole('code')
        expect(pre).toBeInTheDocument()
        const parsed = JSON.parse(pre.textContent || '')
        expect(parsed.kind).toBe('Namespace')
      })
    })

    it('switches back to editor view when Editor toggle is clicked', async () => {
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      const rawJson = JSON.stringify({
        apiVersion: 'v1',
        kind: 'Namespace',
        metadata: { name: 'prj-prod' },
      })

      const transport = createProjectTransport({
        name: 'prod',
        displayName: 'Production',
        description: 'Prod env',
        userRole: Role.EDITOR,
      }, rawJson)

      renderProjectPage(authValue, 'prod', transport)

      await waitFor(() => {
        expect(screen.getByText('Production')).toBeInTheDocument()
      })

      fireEvent.click(screen.getByRole('button', { name: /raw/i }))

      await waitFor(() => {
        expect(screen.getByRole('code')).toBeInTheDocument()
      })

      fireEvent.click(screen.getByRole('button', { name: /editor/i }))

      await waitFor(() => {
        expect(screen.queryByRole('code')).not.toBeInTheDocument()
        expect(screen.getByText('Production')).toBeInTheDocument()
      })
    })
  })

  describe('error handling', () => {
    it('shows not-found error message', async () => {
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      const transport = createRouterTransport(({ service }) => {
        service(ProjectService, {
          listProjects: () => create(ListProjectsResponseSchema, { projects: [] }),
          getProject: () => {
            throw new ConnectError('not found', Code.NotFound)
          },
          deleteProject: () => create(DeleteProjectResponseSchema),
          updateProject: () => create(UpdateProjectResponseSchema),
          updateProjectSharing: () => create(UpdateProjectSharingResponseSchema),
          getProjectRaw: () => create(GetProjectRawResponseSchema),
        })
      })

      renderProjectPage(authValue, 'missing', transport)

      await waitFor(() => {
        expect(screen.getByText(/project "missing" not found/i)).toBeInTheDocument()
      })
    })

    it('shows permission denied error message', async () => {
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      const transport = createRouterTransport(({ service }) => {
        service(ProjectService, {
          listProjects: () => create(ListProjectsResponseSchema, { projects: [] }),
          getProject: () => {
            throw new ConnectError('permission denied', Code.PermissionDenied)
          },
          deleteProject: () => create(DeleteProjectResponseSchema),
          updateProject: () => create(UpdateProjectResponseSchema),
          updateProjectSharing: () => create(UpdateProjectSharingResponseSchema),
          getProjectRaw: () => create(GetProjectRawResponseSchema),
        })
      })

      renderProjectPage(authValue, 'restricted', transport)

      await waitFor(() => {
        expect(screen.getByText(/permission denied/i)).toBeInTheDocument()
      })
    })
  })

  describe('inline editing', () => {
    it('calls updateProject when saving display name', async () => {
      const updateFn = vi.fn(() => create(UpdateProjectResponseSchema))
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      const transport = createRouterTransport(({ service }) => {
        service(ProjectService, {
          listProjects: () => create(ListProjectsResponseSchema, { projects: [] }),
          getProject: () =>
            create(GetProjectResponseSchema, {
              project: create(ProjectSchema, {
                name: 'prod',
                displayName: 'Production',
                description: '',
                userRole: Role.EDITOR,
              }),
            }),
          deleteProject: () => create(DeleteProjectResponseSchema),
          updateProject: (req) => {
            updateFn()
            expect(req.name).toBe('prod')
            expect(req.displayName).toBe('Prod Environment')
            return create(UpdateProjectResponseSchema)
          },
          updateProjectSharing: () => create(UpdateProjectSharingResponseSchema),
          getProjectRaw: () => create(GetProjectRawResponseSchema),
        })
      })

      renderProjectPage(authValue, 'prod', transport)

      await waitFor(() => {
        expect(screen.getByLabelText('edit display name')).toBeInTheDocument()
      })

      fireEvent.click(screen.getByLabelText('edit display name'))

      const input = screen.getByLabelText('Display Name')
      fireEvent.change(input, { target: { value: 'Prod Environment' } })
      fireEvent.click(screen.getByLabelText('save display name'))

      await waitFor(() => {
        expect(updateFn).toHaveBeenCalled()
      })
    })

    it('calls updateProject when saving description', async () => {
      const updateFn = vi.fn(() => create(UpdateProjectResponseSchema))
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      const transport = createRouterTransport(({ service }) => {
        service(ProjectService, {
          listProjects: () => create(ListProjectsResponseSchema, { projects: [] }),
          getProject: () =>
            create(GetProjectResponseSchema, {
              project: create(ProjectSchema, {
                name: 'prod',
                displayName: 'Production',
                description: '',
                userRole: Role.EDITOR,
              }),
            }),
          deleteProject: () => create(DeleteProjectResponseSchema),
          updateProject: (req) => {
            updateFn()
            expect(req.name).toBe('prod')
            expect(req.description).toBe('Updated description')
            return create(UpdateProjectResponseSchema)
          },
          updateProjectSharing: () => create(UpdateProjectSharingResponseSchema),
          getProjectRaw: () => create(GetProjectRawResponseSchema),
        })
      })

      renderProjectPage(authValue, 'prod', transport)

      await waitFor(() => {
        expect(screen.getByLabelText('edit description')).toBeInTheDocument()
      })

      fireEvent.click(screen.getByLabelText('edit description'))

      const input = screen.getByLabelText('Description')
      fireEvent.change(input, { target: { value: 'Updated description' } })
      fireEvent.click(screen.getByLabelText('save description'))

      await waitFor(() => {
        expect(updateFn).toHaveBeenCalled()
      })
    })
  })

  describe('delete project', () => {
    it('opens delete dialog and calls deleteProject on confirm', async () => {
      const deleteFn = vi.fn(() => create(DeleteProjectResponseSchema))
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      const transport = createRouterTransport(({ service }) => {
        service(ProjectService, {
          listProjects: () => create(ListProjectsResponseSchema, { projects: [] }),
          getProject: () =>
            create(GetProjectResponseSchema, {
              project: create(ProjectSchema, {
                name: 'prod',
                displayName: 'Production',
                description: '',
                userRole: Role.OWNER,
              }),
            }),
          deleteProject: () => {
            deleteFn()
            return create(DeleteProjectResponseSchema)
          },
          updateProject: () => create(UpdateProjectResponseSchema),
          updateProjectSharing: () => create(UpdateProjectSharingResponseSchema),
          getProjectRaw: () => create(GetProjectRawResponseSchema),
        })
      })

      renderProjectPage(authValue, 'prod', transport)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /delete/i })).toBeInTheDocument()
      })

      fireEvent.click(screen.getByRole('button', { name: /delete/i }))
      expect(screen.getByText(/are you sure/i)).toBeInTheDocument()

      const dialogDeleteButton = screen.getAllByRole('button', { name: /delete/i }).find(
        (btn) => btn.closest('[role="dialog"]'),
      )
      fireEvent.click(dialogDeleteButton!)

      await waitFor(() => {
        expect(deleteFn).toHaveBeenCalled()
      })
    })

    it('navigates to projects list after successful delete', async () => {
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      const transport = createProjectTransport({
        name: 'prod',
        displayName: 'Production',
        description: '',
        userRole: Role.OWNER,
      })

      renderProjectPage(authValue, 'prod', transport)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /delete/i })).toBeInTheDocument()
      })

      fireEvent.click(screen.getByRole('button', { name: /delete/i }))

      const dialogDeleteButton = screen.getAllByRole('button', { name: /delete/i }).find(
        (btn) => btn.closest('[role="dialog"]'),
      )
      fireEvent.click(dialogDeleteButton!)

      await waitFor(() => {
        expect(screen.getByText('Projects List')).toBeInTheDocument()
      })
    })
  })
})

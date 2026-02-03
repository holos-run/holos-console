import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { TransportProvider } from '@connectrpc/connect-query'
import { createRouterTransport, ConnectError, Code } from '@connectrpc/connect'
import { create } from '@bufbuild/protobuf'
import { ProjectsListPage } from './ProjectsListPage'
import { AuthContext, type AuthContextValue } from '../auth'
import { OrgContext, type OrgContextValue } from '../OrgProvider'
import type { User } from 'oidc-client-ts'
import { vi } from 'vitest'
import { Role } from '../gen/holos/console/v1/rbac_pb'
import {
  CreateProjectResponseSchema,
  DeleteProjectResponseSchema,
  ListProjectsResponseSchema,
  ProjectSchema,
  ProjectService,
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

// Track navigation
let navigatedTo: string | null = null

function CaptureNavigate() {
  const { useLocation } = require('react-router-dom')
  const location = useLocation()
  navigatedTo = location.pathname
  return null
}

const defaultOrgValue: OrgContextValue = {
  organizations: [],
  selectedOrg: null,
  setSelectedOrg: vi.fn(),
  isLoading: false,
}

function renderProjectsListPage(
  authValue: AuthContextValue,
  orgValue: OrgContextValue = defaultOrgValue,
  transport?: Transport,
) {
  navigatedTo = null
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  })
  const t = transport ?? createRouterTransport(({ service }) => {
    service(ProjectService, {
      listProjects: () => create(ListProjectsResponseSchema, { projects: [] }),
      deleteProject: () => create(DeleteProjectResponseSchema),
      createProject: () => create(CreateProjectResponseSchema, { name: 'test' }),
    })
  })
  return render(
    <TransportProvider transport={t}>
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/projects']}>
          <AuthContext.Provider value={authValue}>
            <OrgContext.Provider value={orgValue}>
              <Routes>
                <Route path="/projects" element={<ProjectsListPage />} />
                <Route path="/projects/:projectName" element={<CaptureNavigate />} />
              </Routes>
            </OrgContext.Provider>
          </AuthContext.Provider>
        </MemoryRouter>
      </QueryClientProvider>
    </TransportProvider>,
  )
}

function createListTransport(projects: Array<{
  name: string
  displayName: string
  description: string
  userRole: Role
}>) {
  return createRouterTransport(({ service }) => {
    service(ProjectService, {
      listProjects: () =>
        create(ListProjectsResponseSchema, {
          projects: projects.map((p) => create(ProjectSchema, p)),
        }),
      deleteProject: () => create(DeleteProjectResponseSchema),
      createProject: (req) => create(CreateProjectResponseSchema, { name: req.name }),
    })
  })
}

describe('ProjectsListPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('URL independence', () => {
    it('uses selectedOrg from OrgProvider, not organizationName from URL params', async () => {
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      const orgValue: OrgContextValue = {
        ...defaultOrgValue,
        selectedOrg: 'from-provider',
      }

      const transport = createListTransport([])

      render(
        <TransportProvider transport={transport}>
          <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
            <MemoryRouter initialEntries={['/projects']}>
              <AuthContext.Provider value={authValue}>
                <OrgContext.Provider value={orgValue}>
                  <Routes>
                    <Route path="/projects" element={<ProjectsListPage />} />
                  </Routes>
                </OrgContext.Provider>
              </AuthContext.Provider>
            </MemoryRouter>
          </QueryClientProvider>
        </TransportProvider>,
      )

      await waitFor(() => {
        expect(screen.getByText(/projects in from-provider/i)).toBeInTheDocument()
      })
    })
  })

  describe('project list', () => {
    it('renders project list from listProjects RPC', async () => {
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      const transport = createListTransport([
        {
          name: 'prod',
          displayName: 'Production',
          description: 'Production environment',
          userRole: Role.OWNER,
        },
        {
          name: 'staging',
          displayName: 'Staging',
          description: 'Staging environment',
          userRole: Role.VIEWER,
        },
      ])

      renderProjectsListPage(authValue, defaultOrgValue, transport)

      await waitFor(() => {
        expect(screen.getByText('Production')).toBeInTheDocument()
        expect(screen.getByText('Staging')).toBeInTheDocument()
      })
    })

    it('shows empty state message when no projects exist', async () => {
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      renderProjectsListPage(authValue)

      await waitFor(() => {
        expect(screen.getByText(/no projects available/i)).toBeInTheDocument()
      })
    })

    it('shows Create Project button', async () => {
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      renderProjectsListPage(authValue)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /create project/i })).toBeInTheDocument()
      })
    })

    it('disables Create Project button when no organization is selected', async () => {
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      renderProjectsListPage(authValue, { ...defaultOrgValue, selectedOrg: null })

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /create project/i })).toBeDisabled()
      })
    })

    it('shows user role as chip on each project row', async () => {
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      const transport = createListTransport([
        { name: 'prod', displayName: 'Production', description: '', userRole: Role.OWNER },
        { name: 'staging', displayName: 'Staging', description: '', userRole: Role.VIEWER },
      ])

      renderProjectsListPage(authValue, defaultOrgValue, transport)

      await waitFor(() => {
        expect(screen.getByText('Owner')).toBeInTheDocument()
        expect(screen.getByText('Viewer')).toBeInTheDocument()
      })
    })

    it('shows description as secondary text', async () => {
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      const transport = createListTransport([
        { name: 'prod', displayName: 'Production', description: 'Production environment', userRole: Role.OWNER },
      ])

      renderProjectsListPage(authValue, defaultOrgValue, transport)

      await waitFor(() => {
        expect(screen.getByText('Production environment')).toBeInTheDocument()
      })
    })

    it('shows delete icon only for owner role', async () => {
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      const transport = createListTransport([
        { name: 'prod', displayName: 'Production', description: '', userRole: Role.OWNER },
        { name: 'staging', displayName: 'Staging', description: '', userRole: Role.VIEWER },
      ])

      renderProjectsListPage(authValue, defaultOrgValue, transport)

      await waitFor(() => {
        expect(screen.getByLabelText(/delete prod/i)).toBeInTheDocument()
        expect(screen.queryByLabelText(/delete staging/i)).not.toBeInTheDocument()
      })
    })
  })

  describe('create project', () => {
    const orgWithSelection: OrgContextValue = {
      ...defaultOrgValue,
      selectedOrg: 'test-org',
    }

    it('opens create dialog when Create Project is clicked', async () => {
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      renderProjectsListPage(authValue, orgWithSelection)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /create project/i })).toBeInTheDocument()
      })

      fireEvent.click(screen.getByRole('button', { name: /create project/i }))

      expect(screen.getByText('Create Project', { selector: 'h2' })).toBeInTheDocument()
      expect(screen.getByLabelText('Name')).toBeInTheDocument()
      expect(screen.getByLabelText('Display Name')).toBeInTheDocument()
      expect(screen.getByLabelText('Description')).toBeInTheDocument()
    })

    it('calls createProject RPC on submit', async () => {
      const mockUser = createMockUser({ email: 'alice@example.com' })
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      const transport = createListTransport([])

      renderProjectsListPage(authValue, orgWithSelection, transport)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /create project/i })).toBeInTheDocument()
      })

      fireEvent.click(screen.getByRole('button', { name: /create project/i }))

      fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'new-project' } })
      fireEvent.change(screen.getByLabelText('Display Name'), { target: { value: 'New Project' } })
      fireEvent.change(screen.getByLabelText('Description'), { target: { value: 'A new project' } })

      fireEvent.click(screen.getByRole('button', { name: /^create$/i }))

      await waitFor(() => {
        expect(navigatedTo).toBe('/projects/new-project')
      })
    })

    it('validates project name is non-empty', async () => {
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      renderProjectsListPage(authValue, orgWithSelection)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /create project/i })).toBeInTheDocument()
      })

      fireEvent.click(screen.getByRole('button', { name: /create project/i }))
      fireEvent.click(screen.getByRole('button', { name: /^create$/i }))

      await waitFor(() => {
        expect(screen.getByText(/name is required/i)).toBeInTheDocument()
      })
    })

    it('shows error message on RPC failure', async () => {
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      const transport = createRouterTransport(({ service }) => {
        service(ProjectService, {
          listProjects: () => create(ListProjectsResponseSchema, { projects: [] }),
          createProject: () => {
            throw new ConnectError('already exists', Code.AlreadyExists)
          },
          deleteProject: () => create(DeleteProjectResponseSchema),
        })
      })

      renderProjectsListPage(authValue, orgWithSelection, transport)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /create project/i })).toBeInTheDocument()
      })

      fireEvent.click(screen.getByRole('button', { name: /create project/i }))
      fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'existing' } })
      fireEvent.click(screen.getByRole('button', { name: /^create$/i }))

      await waitFor(() => {
        expect(screen.getByText(/already exists/i)).toBeInTheDocument()
      })
    })

    it('renders Display Name field before Name field', async () => {
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      renderProjectsListPage(authValue, orgWithSelection)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /create project/i })).toBeInTheDocument()
      })

      fireEvent.click(screen.getByRole('button', { name: /create project/i }))

      const dialog = screen.getByRole('dialog')
      const inputs = dialog.querySelectorAll('input')
      const displayNameInput = screen.getByLabelText(/display name/i)
      const nameInput = screen.getByLabelText(/^name$/i)
      const inputArray = Array.from(inputs)
      expect(inputArray.indexOf(displayNameInput as HTMLInputElement))
        .toBeLessThan(inputArray.indexOf(nameInput as HTMLInputElement))
    })

    it('auto-generates slug from display name', async () => {
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      renderProjectsListPage(authValue, orgWithSelection)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /create project/i })).toBeInTheDocument()
      })

      fireEvent.click(screen.getByRole('button', { name: /create project/i }))

      const displayNameInput = screen.getByLabelText(/display name/i)
      const nameInput = screen.getByLabelText(/^name$/i)
      fireEvent.change(displayNameInput, { target: { value: 'My Cool Project' } })
      expect(nameInput).toHaveValue('my-cool-project')
    })

    it('stops auto-generation when user manually edits slug', async () => {
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      renderProjectsListPage(authValue, orgWithSelection)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /create project/i })).toBeInTheDocument()
      })

      fireEvent.click(screen.getByRole('button', { name: /create project/i }))

      const displayNameInput = screen.getByLabelText(/display name/i)
      const nameInput = screen.getByLabelText(/^name$/i)

      fireEvent.change(displayNameInput, { target: { value: 'My Project' } })
      expect(nameInput).toHaveValue('my-project')

      fireEvent.change(nameInput, { target: { value: 'custom-slug' } })
      expect(nameInput).toHaveValue('custom-slug')

      fireEvent.change(displayNameInput, { target: { value: 'Another Name' } })
      expect(nameInput).toHaveValue('custom-slug')
    })

    it('resumes auto-generation when slug is cleared', async () => {
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      renderProjectsListPage(authValue, orgWithSelection)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /create project/i })).toBeInTheDocument()
      })

      fireEvent.click(screen.getByRole('button', { name: /create project/i }))

      const displayNameInput = screen.getByLabelText(/display name/i)
      const nameInput = screen.getByLabelText(/^name$/i)

      fireEvent.change(displayNameInput, { target: { value: 'My Project' } })
      fireEvent.change(nameInput, { target: { value: 'custom-slug' } })
      fireEvent.change(nameInput, { target: { value: '' } })

      fireEvent.change(displayNameInput, { target: { value: 'New Name' } })
      expect(nameInput).toHaveValue('new-name')
    })

    it('gives Display Name field autoFocus', async () => {
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      renderProjectsListPage(authValue, orgWithSelection)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /create project/i })).toBeInTheDocument()
      })

      fireEvent.click(screen.getByRole('button', { name: /create project/i }))

      const displayNameInput = screen.getByLabelText(/display name/i)
      expect(displayNameInput).toHaveFocus()
    })

    it('navigates to new project page on success', async () => {
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      renderProjectsListPage(authValue, orgWithSelection)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /create project/i })).toBeInTheDocument()
      })

      fireEvent.click(screen.getByRole('button', { name: /create project/i }))
      fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'new-project' } })
      fireEvent.click(screen.getByRole('button', { name: /^create$/i }))

      await waitFor(() => {
        expect(navigatedTo).toBe('/projects/new-project')
      })
    })
  })

  describe('delete project', () => {
    it('opens confirmation dialog on delete click', async () => {
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      const transport = createListTransport([
        { name: 'prod', displayName: 'Production', description: '', userRole: Role.OWNER },
      ])

      renderProjectsListPage(authValue, defaultOrgValue, transport)

      await waitFor(() => {
        expect(screen.getByLabelText(/delete prod/i)).toBeInTheDocument()
      })

      fireEvent.click(screen.getByLabelText(/delete prod/i))

      expect(screen.getByText(/are you sure/i)).toBeInTheDocument()
    })

    it('calls deleteProject RPC on confirm', async () => {
      const deleteFn = vi.fn(() => create(DeleteProjectResponseSchema))
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      const transport = createRouterTransport(({ service }) => {
        service(ProjectService, {
          listProjects: () =>
            create(ListProjectsResponseSchema, {
              projects: [
                create(ProjectSchema, {
                  name: 'prod',
                  displayName: 'Production',
                  description: '',
                  userRole: Role.OWNER,
                }),
              ],
            }),
          deleteProject: (req) => {
            deleteFn()
            expect(req.name).toBe('prod')
            return create(DeleteProjectResponseSchema)
          },
          createProject: (req) => create(CreateProjectResponseSchema, { name: req.name }),
        })
      })

      renderProjectsListPage(authValue, defaultOrgValue, transport)

      await waitFor(() => {
        expect(screen.getByLabelText(/delete prod/i)).toBeInTheDocument()
      })

      fireEvent.click(screen.getByLabelText(/delete prod/i))

      const dialogDeleteButton = screen.getAllByRole('button', { name: /delete/i }).find(
        (btn) => btn.closest('[role="dialog"]'),
      )
      fireEvent.click(dialogDeleteButton!)

      await waitFor(() => {
        expect(deleteFn).toHaveBeenCalled()
      })
    })

    it('removes project from list after successful delete (optimistic update)', async () => {
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      let deleted = false
      const transport = createRouterTransport(({ service }) => {
        service(ProjectService, {
          listProjects: () => {
            const projects = deleted
              ? [create(ProjectSchema, { name: 'staging', displayName: 'Staging', description: '', userRole: Role.OWNER })]
              : [
                  create(ProjectSchema, { name: 'prod', displayName: 'Production', description: '', userRole: Role.OWNER }),
                  create(ProjectSchema, { name: 'staging', displayName: 'Staging', description: '', userRole: Role.OWNER }),
                ]
            return create(ListProjectsResponseSchema, { projects })
          },
          deleteProject: () => {
            deleted = true
            return create(DeleteProjectResponseSchema)
          },
          createProject: (req) => create(CreateProjectResponseSchema, { name: req.name }),
        })
      })

      renderProjectsListPage(authValue, defaultOrgValue, transport)

      await waitFor(() => {
        expect(screen.getByText('Production')).toBeInTheDocument()
        expect(screen.getByText('Staging')).toBeInTheDocument()
      })

      fireEvent.click(screen.getByLabelText(/delete prod/i))
      const dialogDeleteButton = screen.getAllByRole('button', { name: /delete/i }).find(
        (btn) => btn.closest('[role="dialog"]'),
      )
      fireEvent.click(dialogDeleteButton!)

      await waitFor(() => {
        expect(screen.queryByText('Production')).not.toBeInTheDocument()
      })
      expect(screen.getByText('Staging')).toBeInTheDocument()
    })
  })
})

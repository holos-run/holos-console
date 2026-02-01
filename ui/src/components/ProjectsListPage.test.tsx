import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { ProjectsListPage } from './ProjectsListPage'
import { AuthContext, type AuthContextValue } from '../auth'
import type { User } from 'oidc-client-ts'
import { vi } from 'vitest'
import { Role } from '../gen/holos/console/v1/rbac_pb'

// Mock the client module
vi.mock('../client', () => ({
  projectsClient: {
    listProjects: vi.fn(),
    createProject: vi.fn(),
    deleteProject: vi.fn(),
  },
}))

import { projectsClient } from '../client'
const mockListProjects = vi.mocked(projectsClient.listProjects)
const mockCreateProject = vi.mocked(projectsClient.createProject)
const mockDeleteProject = vi.mocked(projectsClient.deleteProject)

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
    bffUser: null,
    isBFF: false,
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

function renderProjectsListPage(authValue: AuthContextValue) {
  navigatedTo = null
  return render(
    <MemoryRouter initialEntries={['/projects']}>
      <AuthContext.Provider value={authValue}>
        <Routes>
          <Route path="/projects" element={<ProjectsListPage />} />
          <Route path="/projects/:projectName" element={<CaptureNavigate />} />
        </Routes>
      </AuthContext.Provider>
    </MemoryRouter>,
  )
}

describe('ProjectsListPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('project list', () => {
    it('renders project list from listProjects RPC', async () => {
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      mockListProjects.mockResolvedValue({
        projects: [
          {
            name: 'prod',
            displayName: 'Production',
            description: 'Production environment',
            userGrants: [],
            groupGrants: [],
            userRole: Role.OWNER,
          },
          {
            name: 'staging',
            displayName: 'Staging',
            description: 'Staging environment',
            userGrants: [],
            groupGrants: [],
            userRole: Role.VIEWER,
          },
        ],
      } as unknown as Awaited<ReturnType<typeof projectsClient.listProjects>>)

      renderProjectsListPage(authValue)

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

      mockListProjects.mockResolvedValue({
        projects: [],
      } as unknown as Awaited<ReturnType<typeof projectsClient.listProjects>>)

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

      mockListProjects.mockResolvedValue({
        projects: [],
      } as unknown as Awaited<ReturnType<typeof projectsClient.listProjects>>)

      renderProjectsListPage(authValue)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /create project/i })).toBeInTheDocument()
      })
    })

    it('shows user role as chip on each project row', async () => {
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      mockListProjects.mockResolvedValue({
        projects: [
          {
            name: 'prod',
            displayName: 'Production',
            description: '',
            userGrants: [],
            groupGrants: [],
            userRole: Role.OWNER,
          },
          {
            name: 'staging',
            displayName: 'Staging',
            description: '',
            userGrants: [],
            groupGrants: [],
            userRole: Role.VIEWER,
          },
        ],
      } as unknown as Awaited<ReturnType<typeof projectsClient.listProjects>>)

      renderProjectsListPage(authValue)

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

      mockListProjects.mockResolvedValue({
        projects: [
          {
            name: 'prod',
            displayName: 'Production',
            description: 'Production environment',
            userGrants: [],
            groupGrants: [],
            userRole: Role.OWNER,
          },
        ],
      } as unknown as Awaited<ReturnType<typeof projectsClient.listProjects>>)

      renderProjectsListPage(authValue)

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

      mockListProjects.mockResolvedValue({
        projects: [
          {
            name: 'prod',
            displayName: 'Production',
            description: '',
            userGrants: [],
            groupGrants: [],
            userRole: Role.OWNER,
          },
          {
            name: 'staging',
            displayName: 'Staging',
            description: '',
            userGrants: [],
            groupGrants: [],
            userRole: Role.VIEWER,
          },
        ],
      } as unknown as Awaited<ReturnType<typeof projectsClient.listProjects>>)

      renderProjectsListPage(authValue)

      await waitFor(() => {
        expect(screen.getByLabelText(/delete prod/i)).toBeInTheDocument()
        expect(screen.queryByLabelText(/delete staging/i)).not.toBeInTheDocument()
      })
    })
  })

  describe('create project', () => {
    it('opens create dialog when Create Project is clicked', async () => {
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      mockListProjects.mockResolvedValue({
        projects: [],
      } as unknown as Awaited<ReturnType<typeof projectsClient.listProjects>>)

      renderProjectsListPage(authValue)

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
        getAccessToken: vi.fn(() => 'test-token'),
      })

      mockListProjects.mockResolvedValue({
        projects: [],
      } as unknown as Awaited<ReturnType<typeof projectsClient.listProjects>>)

      mockCreateProject.mockResolvedValue({
        name: 'new-project',
      } as unknown as Awaited<ReturnType<typeof projectsClient.createProject>>)

      renderProjectsListPage(authValue)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /create project/i })).toBeInTheDocument()
      })

      fireEvent.click(screen.getByRole('button', { name: /create project/i }))

      fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'new-project' } })
      fireEvent.change(screen.getByLabelText('Display Name'), { target: { value: 'New Project' } })
      fireEvent.change(screen.getByLabelText('Description'), { target: { value: 'A new project' } })

      fireEvent.click(screen.getByRole('button', { name: /^create$/i }))

      await waitFor(() => {
        expect(mockCreateProject).toHaveBeenCalledWith(
          expect.objectContaining({
            name: 'new-project',
            displayName: 'New Project',
            description: 'A new project',
            userGrants: expect.arrayContaining([
              expect.objectContaining({ principal: 'alice@example.com', role: Role.OWNER }),
            ]),
          }),
          expect.objectContaining({
            headers: expect.objectContaining({
              Authorization: 'Bearer test-token',
            }),
          }),
        )
      })
    })

    it('validates project name is non-empty', async () => {
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      mockListProjects.mockResolvedValue({
        projects: [],
      } as unknown as Awaited<ReturnType<typeof projectsClient.listProjects>>)

      renderProjectsListPage(authValue)

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /create project/i })).toBeInTheDocument()
      })

      fireEvent.click(screen.getByRole('button', { name: /create project/i }))
      fireEvent.click(screen.getByRole('button', { name: /^create$/i }))

      await waitFor(() => {
        expect(screen.getByText(/name is required/i)).toBeInTheDocument()
      })

      expect(mockCreateProject).not.toHaveBeenCalled()
    })

    it('shows error message on RPC failure', async () => {
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      mockListProjects.mockResolvedValue({
        projects: [],
      } as unknown as Awaited<ReturnType<typeof projectsClient.listProjects>>)

      mockCreateProject.mockRejectedValue(new Error('already exists'))

      renderProjectsListPage(authValue)

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

    it('navigates to new project page on success', async () => {
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      mockListProjects.mockResolvedValue({
        projects: [],
      } as unknown as Awaited<ReturnType<typeof projectsClient.listProjects>>)

      mockCreateProject.mockResolvedValue({
        name: 'new-project',
      } as unknown as Awaited<ReturnType<typeof projectsClient.createProject>>)

      renderProjectsListPage(authValue)

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

      mockListProjects.mockResolvedValue({
        projects: [
          {
            name: 'prod',
            displayName: 'Production',
            description: '',
            userGrants: [],
            groupGrants: [],
            userRole: Role.OWNER,
          },
        ],
      } as unknown as Awaited<ReturnType<typeof projectsClient.listProjects>>)

      renderProjectsListPage(authValue)

      await waitFor(() => {
        expect(screen.getByLabelText(/delete prod/i)).toBeInTheDocument()
      })

      fireEvent.click(screen.getByLabelText(/delete prod/i))

      expect(screen.getByText(/are you sure/i)).toBeInTheDocument()
    })

    it('calls deleteProject RPC on confirm', async () => {
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
        getAccessToken: vi.fn(() => 'test-token'),
      })

      mockListProjects.mockResolvedValue({
        projects: [
          {
            name: 'prod',
            displayName: 'Production',
            description: '',
            userGrants: [],
            groupGrants: [],
            userRole: Role.OWNER,
          },
        ],
      } as unknown as Awaited<ReturnType<typeof projectsClient.listProjects>>)

      mockDeleteProject.mockResolvedValue({} as unknown as Awaited<ReturnType<typeof projectsClient.deleteProject>>)

      renderProjectsListPage(authValue)

      await waitFor(() => {
        expect(screen.getByLabelText(/delete prod/i)).toBeInTheDocument()
      })

      fireEvent.click(screen.getByLabelText(/delete prod/i))

      const dialogDeleteButton = screen.getAllByRole('button', { name: /delete/i }).find(
        (btn) => btn.closest('[role="dialog"]'),
      )
      fireEvent.click(dialogDeleteButton!)

      await waitFor(() => {
        expect(mockDeleteProject).toHaveBeenCalledWith(
          expect.objectContaining({ name: 'prod' }),
          expect.objectContaining({
            headers: expect.objectContaining({
              Authorization: 'Bearer test-token',
            }),
          }),
        )
      })
    })
  })
})

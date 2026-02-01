import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { ProjectPage } from './ProjectPage'
import { AuthContext, type AuthContextValue } from '../auth'
import type { User } from 'oidc-client-ts'
import { vi } from 'vitest'
import { Role } from '../gen/holos/console/v1/rbac_pb'

// Mock the client module
vi.mock('../client', () => ({
  projectsClient: {
    getProject: vi.fn(),
    updateProject: vi.fn(),
    updateProjectSharing: vi.fn(),
    deleteProject: vi.fn(),
  },
}))

import { projectsClient } from '../client'
const mockGetProject = vi.mocked(projectsClient.getProject)
const mockUpdateProject = vi.mocked(projectsClient.updateProject)
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

function renderProjectPage(authValue: AuthContextValue, projectName = 'test-project') {
  return render(
    <MemoryRouter initialEntries={[`/projects/${projectName}`]}>
      <AuthContext.Provider value={authValue}>
        <Routes>
          <Route path="/projects/:name" element={<ProjectPage />} />
          <Route path="/projects" element={<div>Projects List</div>} />
        </Routes>
      </AuthContext.Provider>
    </MemoryRouter>,
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

      mockGetProject.mockResolvedValue({
        project: {
          name: 'prod',
          displayName: 'Production',
          description: 'Production environment',
          userGrants: [{ principal: 'test@example.com', role: Role.OWNER }],
          groupGrants: [],
          userRole: Role.OWNER,
        },
      } as unknown as Awaited<ReturnType<typeof projectsClient.getProject>>)

      renderProjectPage(authValue, 'prod')

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

      mockGetProject.mockResolvedValue({
        project: {
          name: 'prod',
          displayName: 'Production',
          description: '',
          userGrants: [],
          groupGrants: [],
          userRole: Role.VIEWER,
        },
      } as unknown as Awaited<ReturnType<typeof projectsClient.getProject>>)

      renderProjectPage(authValue, 'prod')

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

      mockGetProject.mockResolvedValue({
        project: {
          name: 'prod',
          displayName: 'Production',
          description: 'Desc',
          userGrants: [],
          groupGrants: [],
          userRole: Role.EDITOR,
        },
      } as unknown as Awaited<ReturnType<typeof projectsClient.getProject>>)

      renderProjectPage(authValue, 'prod')

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

      mockGetProject.mockResolvedValue({
        project: {
          name: 'prod',
          displayName: 'Production',
          description: 'Desc',
          userGrants: [],
          groupGrants: [],
          userRole: Role.VIEWER,
        },
      } as unknown as Awaited<ReturnType<typeof projectsClient.getProject>>)

      renderProjectPage(authValue, 'prod')

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

      mockGetProject.mockResolvedValue({
        project: {
          name: 'prod',
          displayName: 'Production',
          description: '',
          userGrants: [],
          groupGrants: [],
          userRole: Role.OWNER,
        },
      } as unknown as Awaited<ReturnType<typeof projectsClient.getProject>>)

      renderProjectPage(authValue, 'prod')

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

      mockGetProject.mockResolvedValue({
        project: {
          name: 'prod',
          displayName: 'Production',
          description: '',
          userGrants: [],
          groupGrants: [],
          userRole: Role.EDITOR,
        },
      } as unknown as Awaited<ReturnType<typeof projectsClient.getProject>>)

      renderProjectPage(authValue, 'prod')

      await waitFor(() => {
        expect(screen.getByText('Production')).toBeInTheDocument()
      })

      expect(screen.queryByRole('button', { name: /delete/i })).not.toBeInTheDocument()
    })
  })

  describe('error handling', () => {
    it('shows not-found error message', async () => {
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      const err = new Error('not found')
      ;(err as Error & { code?: string }).code = 'not_found'
      mockGetProject.mockRejectedValue(err)

      renderProjectPage(authValue, 'missing')

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

      const err = new Error('permission denied')
      ;(err as Error & { code?: string }).code = 'permission_denied'
      mockGetProject.mockRejectedValue(err)

      renderProjectPage(authValue, 'restricted')

      await waitFor(() => {
        expect(screen.getByText(/permission denied/i)).toBeInTheDocument()
      })
    })
  })

  describe('inline editing', () => {
    it('calls updateProject when saving display name', async () => {
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
        getAccessToken: vi.fn(() => 'test-token'),
      })

      mockGetProject.mockResolvedValue({
        project: {
          name: 'prod',
          displayName: 'Production',
          description: '',
          userGrants: [],
          groupGrants: [],
          userRole: Role.EDITOR,
        },
      } as unknown as Awaited<ReturnType<typeof projectsClient.getProject>>)

      mockUpdateProject.mockResolvedValue({} as unknown as Awaited<ReturnType<typeof projectsClient.updateProject>>)

      renderProjectPage(authValue, 'prod')

      await waitFor(() => {
        expect(screen.getByLabelText('edit display name')).toBeInTheDocument()
      })

      fireEvent.click(screen.getByLabelText('edit display name'))

      const input = screen.getByLabelText('Display Name')
      fireEvent.change(input, { target: { value: 'Prod Environment' } })
      fireEvent.click(screen.getByLabelText('save display name'))

      await waitFor(() => {
        expect(mockUpdateProject).toHaveBeenCalledWith(
          expect.objectContaining({
            name: 'prod',
            displayName: 'Prod Environment',
          }),
          expect.objectContaining({
            headers: expect.objectContaining({
              Authorization: 'Bearer test-token',
            }),
          }),
        )
      })
    })

    it('calls updateProject when saving description', async () => {
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
        getAccessToken: vi.fn(() => 'test-token'),
      })

      mockGetProject.mockResolvedValue({
        project: {
          name: 'prod',
          displayName: 'Production',
          description: '',
          userGrants: [],
          groupGrants: [],
          userRole: Role.EDITOR,
        },
      } as unknown as Awaited<ReturnType<typeof projectsClient.getProject>>)

      mockUpdateProject.mockResolvedValue({} as unknown as Awaited<ReturnType<typeof projectsClient.updateProject>>)

      renderProjectPage(authValue, 'prod')

      await waitFor(() => {
        expect(screen.getByLabelText('edit description')).toBeInTheDocument()
      })

      fireEvent.click(screen.getByLabelText('edit description'))

      const input = screen.getByLabelText('Description')
      fireEvent.change(input, { target: { value: 'Updated description' } })
      fireEvent.click(screen.getByLabelText('save description'))

      await waitFor(() => {
        expect(mockUpdateProject).toHaveBeenCalledWith(
          expect.objectContaining({
            name: 'prod',
            description: 'Updated description',
          }),
          expect.objectContaining({
            headers: expect.objectContaining({
              Authorization: 'Bearer test-token',
            }),
          }),
        )
      })
    })
  })

  describe('delete project', () => {
    it('opens delete dialog and calls deleteProject on confirm', async () => {
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
        getAccessToken: vi.fn(() => 'test-token'),
      })

      mockGetProject.mockResolvedValue({
        project: {
          name: 'prod',
          displayName: 'Production',
          description: '',
          userGrants: [],
          groupGrants: [],
          userRole: Role.OWNER,
        },
      } as unknown as Awaited<ReturnType<typeof projectsClient.getProject>>)

      mockDeleteProject.mockResolvedValue({} as unknown as Awaited<ReturnType<typeof projectsClient.deleteProject>>)

      renderProjectPage(authValue, 'prod')

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

    it('navigates to projects list after successful delete', async () => {
      const mockUser = createMockUser({})
      const authValue = createAuthContext({
        user: mockUser,
        isAuthenticated: true,
      })

      mockGetProject.mockResolvedValue({
        project: {
          name: 'prod',
          displayName: 'Production',
          description: '',
          userGrants: [],
          groupGrants: [],
          userRole: Role.OWNER,
        },
      } as unknown as Awaited<ReturnType<typeof projectsClient.getProject>>)

      mockDeleteProject.mockResolvedValue({} as unknown as Awaited<ReturnType<typeof projectsClient.deleteProject>>)

      renderProjectPage(authValue, 'prod')

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

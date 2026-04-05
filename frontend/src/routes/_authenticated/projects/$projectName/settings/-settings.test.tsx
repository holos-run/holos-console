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
  useUpdateProjectSettings: vi.fn(),
}))

vi.mock('@/lib/auth', () => ({ useAuth: vi.fn() }))
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

import { useGetProject, useUpdateProject, useUpdateProjectSharing, useUpdateProjectDefaultSharing, useDeleteProject } from '@/queries/projects'
import { useGetProjectSettings, useUpdateProjectSettings } from '@/queries/project-settings'
import { useAuth } from '@/lib/auth'
import { ProjectSettingsPage } from './index'

const mockProject = {
  name: 'test-project',
  displayName: 'Test Project',
  description: 'A test project',
  organization: 'my-org',
  creatorEmail: 'creator@example.com',
  createdAt: '2024-01-15T10:30:00Z',
  userGrants: [{ principal: 'alice@example.com', role: 3 }],
  roleGrants: [],
  defaultUserGrants: [{ principal: 'bob@example.com', role: 1 }],
  defaultRoleGrants: [],
  userRole: 3, // OWNER
}

function setupMocks(overrides: Partial<typeof mockProject> = {}) {
  const project = { ...mockProject, ...overrides }

  ;(useGetProject as Mock).mockReturnValue({
    data: project,
    isPending: false,
    error: null,
  })
  ;(useUpdateProject as Mock).mockReturnValue({
    mutateAsync: vi.fn().mockResolvedValue({}),
    isPending: false,
  })
  ;(useUpdateProjectSharing as Mock).mockReturnValue({
    mutateAsync: vi.fn().mockResolvedValue({}),
    isPending: false,
  })
  ;(useUpdateProjectDefaultSharing as Mock).mockReturnValue({
    mutateAsync: vi.fn().mockResolvedValue({}),
    isPending: false,
  })
  ;(useDeleteProject as Mock).mockReturnValue({
    mutateAsync: vi.fn().mockResolvedValue({}),
    isPending: false,
    error: null,
  })
  ;(useGetProjectSettings as Mock).mockReturnValue({
    data: { project: 'test-project', deploymentsEnabled: false },
    isPending: false,
    error: null,
  })
  ;(useUpdateProjectSettings as Mock).mockReturnValue({
    mutateAsync: vi.fn().mockResolvedValue({}),
    isPending: false,
  })
  ;(useAuth as Mock).mockReturnValue({
    isAuthenticated: true,
    isLoading: false,
    user: { profile: { email: 'alice@example.com', groups: [] } },
  })
}

describe('ProjectSettingsPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders display name and description from project data', () => {
    setupMocks()
    render(<ProjectSettingsPage />)
    expect(screen.getByText('Test Project')).toBeInTheDocument()
    expect(screen.getByText('A test project')).toBeInTheDocument()
  })

  it('renders name (slug) as read-only', () => {
    setupMocks()
    render(<ProjectSettingsPage />)
    expect(screen.getByText('test-project')).toBeInTheDocument()
  })

  it('shows skeleton rows while query is pending', () => {
    ;(useGetProject as Mock).mockReturnValue({ data: undefined, isPending: true, error: null })
    ;(useUpdateProject as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false })
    ;(useUpdateProjectSharing as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false })
    ;(useUpdateProjectDefaultSharing as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false })
    ;(useDeleteProject as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false, error: null })
    ;(useGetProjectSettings as Mock).mockReturnValue({ data: undefined, isPending: false, error: null })
    ;(useUpdateProjectSettings as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false })
    ;(useAuth as Mock).mockReturnValue({ isAuthenticated: true, isLoading: false, user: null })

    render(<ProjectSettingsPage />)
    const skeletons = document.querySelectorAll('[data-slot="skeleton"]')
    expect(skeletons.length).toBeGreaterThan(0)
  })

  it('shows error alert when query fails', () => {
    ;(useGetProject as Mock).mockReturnValue({ data: undefined, isPending: false, error: new Error('Not found') })
    ;(useUpdateProject as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false })
    ;(useUpdateProjectSharing as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false })
    ;(useUpdateProjectDefaultSharing as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false })
    ;(useDeleteProject as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false, error: null })
    ;(useGetProjectSettings as Mock).mockReturnValue({ data: undefined, isPending: false, error: null })
    ;(useUpdateProjectSettings as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false })
    ;(useAuth as Mock).mockReturnValue({ isAuthenticated: true, isLoading: false, user: null })

    render(<ProjectSettingsPage />)
    expect(screen.getByText('Not found')).toBeInTheDocument()
  })

  describe('Display Name inline edit', () => {
    it('clicking pencil switches to input with current value', () => {
      setupMocks()
      render(<ProjectSettingsPage />)
      const editButtons = screen.getAllByRole('button', { name: /edit display name/i })
      fireEvent.click(editButtons[0])
      const input = screen.getByRole('textbox', { name: /display name/i })
      expect(input).toBeInTheDocument()
      expect((input as HTMLInputElement).value).toBe('Test Project')
    })

    it('saving calls useUpdateProject with new displayName', async () => {
      setupMocks()
      render(<ProjectSettingsPage />)
      const editButtons = screen.getAllByRole('button', { name: /edit display name/i })
      fireEvent.click(editButtons[0])
      const input = screen.getByRole('textbox', { name: /display name/i })
      fireEvent.change(input, { target: { value: 'New Name' } })
      fireEvent.click(screen.getByRole('button', { name: /save display name/i }))
      const mutateAsync = (useUpdateProject as Mock).mock.results[0].value.mutateAsync
      await waitFor(() => {
        expect(mutateAsync).toHaveBeenCalledWith({ name: 'test-project', displayName: 'New Name' })
      })
    })

    it('cancel restores previous value without calling useUpdateProject', async () => {
      setupMocks()
      render(<ProjectSettingsPage />)
      const editButtons = screen.getAllByRole('button', { name: /edit display name/i })
      fireEvent.click(editButtons[0])
      const input = screen.getByRole('textbox', { name: /display name/i })
      fireEvent.change(input, { target: { value: 'Changed Name' } })
      fireEvent.click(screen.getByRole('button', { name: /cancel display name/i }))
      expect(screen.getByText('Test Project')).toBeInTheDocument()
      const mutateAsync = (useUpdateProject as Mock).mock.results[0].value.mutateAsync
      expect(mutateAsync).not.toHaveBeenCalled()
    })
  })

  describe('Description inline edit', () => {
    it('clicking pencil switches to textarea with current value', () => {
      setupMocks()
      render(<ProjectSettingsPage />)
      const editButtons = screen.getAllByRole('button', { name: /edit description/i })
      fireEvent.click(editButtons[0])
      const textarea = screen.getByRole('textbox', { name: /description/i })
      expect(textarea).toBeInTheDocument()
      expect((textarea as HTMLTextAreaElement).value).toBe('A test project')
    })

    it('saving calls useUpdateProject with new description', async () => {
      setupMocks()
      render(<ProjectSettingsPage />)
      const editButtons = screen.getAllByRole('button', { name: /edit description/i })
      fireEvent.click(editButtons[0])
      const textarea = screen.getByRole('textbox', { name: /description/i })
      fireEvent.change(textarea, { target: { value: 'New description' } })
      fireEvent.click(screen.getByRole('button', { name: /save description/i }))
      const mutateAsync = (useUpdateProject as Mock).mock.results[0].value.mutateAsync
      await waitFor(() => {
        expect(mutateAsync).toHaveBeenCalledWith({ name: 'test-project', description: 'New description' })
      })
    })

    it('cancel restores previous value without calling useUpdateProject', async () => {
      setupMocks()
      render(<ProjectSettingsPage />)
      const editButtons = screen.getAllByRole('button', { name: /edit description/i })
      fireEvent.click(editButtons[0])
      const textarea = screen.getByRole('textbox', { name: /description/i })
      fireEvent.change(textarea, { target: { value: 'Changed desc' } })
      fireEvent.click(screen.getByRole('button', { name: /cancel description/i }))
      expect(screen.getByText('A test project')).toBeInTheDocument()
      const mutateAsync = (useUpdateProject as Mock).mock.results[0].value.mutateAsync
      expect(mutateAsync).not.toHaveBeenCalled()
    })
  })

  describe('Sharing section', () => {
    it('renders SharingPanel with user grants', () => {
      setupMocks()
      render(<ProjectSettingsPage />)
      expect(screen.getByText('alice@example.com')).toBeInTheDocument()
    })

    it('saving sharing calls useUpdateProjectSharing', async () => {
      setupMocks()
      render(<ProjectSettingsPage />)
      // First Edit button is the Sharing section (index 0), second is Default Secret Sharing (index 1)
      const editButtons = screen.getAllByRole('button', { name: /^edit$/i })
      fireEvent.click(editButtons[0])
      fireEvent.click(screen.getByRole('button', { name: /^save$/i }))
      const mutateAsync = (useUpdateProjectSharing as Mock).mock.results[0].value.mutateAsync
      await waitFor(() => {
        expect(mutateAsync).toHaveBeenCalledWith(
          expect.objectContaining({ name: 'test-project' }),
        )
      })
    })
  })

  describe('Default Secret Sharing section', () => {
    it('renders default grants from project data', () => {
      setupMocks()
      render(<ProjectSettingsPage />)
      expect(screen.getByText('Default Secret Sharing')).toBeInTheDocument()
      expect(screen.getByText('bob@example.com')).toBeInTheDocument()
    })

    it('shows explanatory description text', () => {
      setupMocks()
      render(<ProjectSettingsPage />)
      expect(screen.getByText(/automatically applied to every new secret/i)).toBeInTheDocument()
    })

    it('save calls UpdateProjectDefaultSharing mutation', async () => {
      setupMocks()
      render(<ProjectSettingsPage />)
      // Second Edit button is the Default Secret Sharing section (index 1)
      const editButtons = screen.getAllByRole('button', { name: /^edit$/i })
      fireEvent.click(editButtons[1])
      fireEvent.click(screen.getByRole('button', { name: /^save$/i }))
      const mutateAsync = (useUpdateProjectDefaultSharing as Mock).mock.results[0].value.mutateAsync
      await waitFor(() => {
        expect(mutateAsync).toHaveBeenCalledWith(
          expect.objectContaining({ name: 'test-project' }),
        )
      })
    })

    it('non-owners cannot edit default sharing grants', () => {
      setupMocks({ userRole: 1 }) // VIEWER
      render(<ProjectSettingsPage />)
      // With userRole=VIEWER, there are no Edit buttons since isOwner=false
      const editButtons = screen.queryAllByRole('button', { name: /^edit$/i })
      expect(editButtons.length).toBe(0)
    })
  })

  describe('Creator and Created At fields', () => {
    it('renders creator email as read-only text', () => {
      setupMocks()
      render(<ProjectSettingsPage />)
      expect(screen.getByText('creator@example.com')).toBeInTheDocument()
    })

    it('renders formatted created timestamp as read-only text', () => {
      setupMocks()
      render(<ProjectSettingsPage />)
      const formatted = new Date('2024-01-15T10:30:00Z').toLocaleString()
      expect(screen.getByText(formatted)).toBeInTheDocument()
    })

    it('shows "Unknown" for creator email when empty', () => {
      setupMocks({ creatorEmail: '' })
      render(<ProjectSettingsPage />)
      expect(screen.getByText('Unknown')).toBeInTheDocument()
    })

    it('shows "Unknown" for created timestamp when empty', () => {
      setupMocks({ creatorEmail: '', createdAt: '' })
      render(<ProjectSettingsPage />)
      const unknownElements = screen.getAllByText('Unknown')
      expect(unknownElements.length).toBeGreaterThanOrEqual(2)
    })

    it('no input element is present for creator email', () => {
      setupMocks()
      render(<ProjectSettingsPage />)
      // Verify creator email is static text, not an input
      const inputs = document.querySelectorAll('input[aria-label*="creator"], input[aria-label*="created"]')
      expect(inputs.length).toBe(0)
    })
  })

  describe('Delete button', () => {
    it('delete button is visible for Owner', () => {
      setupMocks({ userRole: 3 }) // OWNER
      render(<ProjectSettingsPage />)
      expect(screen.getByRole('button', { name: /delete project/i })).toBeInTheDocument()
    })

    it('delete button is hidden for Viewer', () => {
      setupMocks({ userRole: 1 }) // VIEWER
      render(<ProjectSettingsPage />)
      expect(screen.queryByRole('button', { name: /delete project/i })).not.toBeInTheDocument()
    })

    it('delete button is hidden for Editor', () => {
      setupMocks({ userRole: 2 }) // EDITOR
      render(<ProjectSettingsPage />)
      expect(screen.queryByRole('button', { name: /delete project/i })).not.toBeInTheDocument()
    })

    it('clicking delete opens confirmation dialog', () => {
      setupMocks({ userRole: 3 })
      render(<ProjectSettingsPage />)
      fireEvent.click(screen.getByRole('button', { name: /delete project/i }))
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })

    it('confirming dialog calls useDeleteProject and navigates away', async () => {
      setupMocks({ userRole: 3 })
      render(<ProjectSettingsPage />)
      fireEvent.click(screen.getByRole('button', { name: /delete project/i }))
      fireEvent.click(screen.getByRole('button', { name: /^delete$/i }))
      const mutateAsync = (useDeleteProject as Mock).mock.results[0].value.mutateAsync
      await waitFor(() => {
        expect(mutateAsync).toHaveBeenCalledWith({ name: 'test-project' })
      })
    })
  })
})

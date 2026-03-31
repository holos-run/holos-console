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
  useDeleteProject: vi.fn(),
}))

vi.mock('@/lib/auth', () => ({ useAuth: vi.fn() }))
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

import { useGetProject, useUpdateProject, useUpdateProjectSharing, useDeleteProject } from '@/queries/projects'
import { useAuth } from '@/lib/auth'
import { ProjectSettingsPage } from './index'

const mockProject = {
  name: 'test-project',
  displayName: 'Test Project',
  description: 'A test project',
  organization: 'my-org',
  userGrants: [{ principal: 'alice@example.com', role: 3 }],
  roleGrants: [],
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
  ;(useDeleteProject as Mock).mockReturnValue({
    mutateAsync: vi.fn().mockResolvedValue({}),
    isPending: false,
    error: null,
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
    ;(useDeleteProject as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false, error: null })
    ;(useAuth as Mock).mockReturnValue({ isAuthenticated: true, isLoading: false, user: null })

    render(<ProjectSettingsPage />)
    const skeletons = document.querySelectorAll('[data-slot="skeleton"]')
    expect(skeletons.length).toBeGreaterThan(0)
  })

  it('shows error alert when query fails', () => {
    ;(useGetProject as Mock).mockReturnValue({ data: undefined, isPending: false, error: new Error('Not found') })
    ;(useUpdateProject as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false })
    ;(useUpdateProjectSharing as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false })
    ;(useDeleteProject as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false, error: null })
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
      const editButtons = screen.getAllByRole('button', { name: /^edit$/i })
      fireEvent.click(editButtons[editButtons.length - 1])
      fireEvent.click(screen.getByRole('button', { name: /^save$/i }))
      const mutateAsync = (useUpdateProjectSharing as Mock).mock.results[0].value.mutateAsync
      await waitFor(() => {
        expect(mutateAsync).toHaveBeenCalledWith(
          expect.objectContaining({ name: 'test-project' }),
        )
      })
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

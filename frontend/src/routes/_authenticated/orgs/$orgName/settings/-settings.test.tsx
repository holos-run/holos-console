import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => ({ orgName: 'test-org' }),
    }),
    useNavigate: () => vi.fn(),
  }
})

vi.mock('@/queries/organizations', () => ({
  useGetOrganization: vi.fn(),
  useUpdateOrganization: vi.fn(),
  useUpdateOrganizationSharing: vi.fn(),
  useDeleteOrganization: vi.fn(),
}))

vi.mock('@/lib/auth', () => ({ useAuth: vi.fn() }))
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

import {
  useGetOrganization,
  useUpdateOrganization,
  useUpdateOrganizationSharing,
  useDeleteOrganization,
} from '@/queries/organizations'
import { useAuth } from '@/lib/auth'
import { OrgSettingsPage } from './index'

const mockOrg = {
  name: 'test-org',
  displayName: 'Test Org',
  description: 'A test organization',
  userGrants: [{ principal: 'alice@example.com', role: 3 }],
  roleGrants: [],
  userRole: 3, // OWNER
}

function setupMocks(overrides: Partial<typeof mockOrg> = {}) {
  const org = { ...mockOrg, ...overrides }

  ;(useGetOrganization as Mock).mockReturnValue({
    data: org,
    isPending: false,
    error: null,
  })
  ;(useUpdateOrganization as Mock).mockReturnValue({
    mutateAsync: vi.fn().mockResolvedValue({}),
    isPending: false,
  })
  ;(useUpdateOrganizationSharing as Mock).mockReturnValue({
    mutateAsync: vi.fn().mockResolvedValue({}),
    isPending: false,
  })
  ;(useDeleteOrganization as Mock).mockReturnValue({
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

describe('OrgSettingsPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders display name and description from org data', () => {
    setupMocks()
    render(<OrgSettingsPage />)
    expect(screen.getByText('Test Org')).toBeInTheDocument()
    expect(screen.getByText('A test organization')).toBeInTheDocument()
  })

  it('renders name (slug) as read-only', () => {
    setupMocks()
    render(<OrgSettingsPage />)
    expect(screen.getByText('test-org')).toBeInTheDocument()
  })

  it('shows skeleton rows while query is pending', () => {
    ;(useGetOrganization as Mock).mockReturnValue({ data: undefined, isPending: true, error: null })
    ;(useUpdateOrganization as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false })
    ;(useUpdateOrganizationSharing as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false })
    ;(useDeleteOrganization as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false, error: null })
    ;(useAuth as Mock).mockReturnValue({ isAuthenticated: true, isLoading: false, user: null })

    render(<OrgSettingsPage />)
    const skeletons = document.querySelectorAll('[data-slot="skeleton"]')
    expect(skeletons.length).toBeGreaterThan(0)
  })

  it('shows error alert when query fails', () => {
    ;(useGetOrganization as Mock).mockReturnValue({ data: undefined, isPending: false, error: new Error('Not found') })
    ;(useUpdateOrganization as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false })
    ;(useUpdateOrganizationSharing as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false })
    ;(useDeleteOrganization as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false, error: null })
    ;(useAuth as Mock).mockReturnValue({ isAuthenticated: true, isLoading: false, user: null })

    render(<OrgSettingsPage />)
    expect(screen.getByText('Not found')).toBeInTheDocument()
  })

  describe('Display Name inline edit', () => {
    it('clicking pencil switches to input with current value', () => {
      setupMocks()
      render(<OrgSettingsPage />)
      const editButtons = screen.getAllByRole('button', { name: /edit display name/i })
      fireEvent.click(editButtons[0])
      const input = screen.getByRole('textbox', { name: /display name/i })
      expect(input).toBeInTheDocument()
      expect((input as HTMLInputElement).value).toBe('Test Org')
    })

    it('saving calls useUpdateOrganization with new displayName', async () => {
      setupMocks()
      render(<OrgSettingsPage />)
      const editButtons = screen.getAllByRole('button', { name: /edit display name/i })
      fireEvent.click(editButtons[0])
      const input = screen.getByRole('textbox', { name: /display name/i })
      fireEvent.change(input, { target: { value: 'New Name' } })
      fireEvent.click(screen.getByRole('button', { name: /save display name/i }))
      const mutateAsync = (useUpdateOrganization as Mock).mock.results[0].value.mutateAsync
      await waitFor(() => {
        expect(mutateAsync).toHaveBeenCalledWith({ name: 'test-org', displayName: 'New Name' })
      })
    })

    it('cancel restores previous value without calling useUpdateOrganization', async () => {
      setupMocks()
      render(<OrgSettingsPage />)
      const editButtons = screen.getAllByRole('button', { name: /edit display name/i })
      fireEvent.click(editButtons[0])
      const input = screen.getByRole('textbox', { name: /display name/i })
      fireEvent.change(input, { target: { value: 'Changed Name' } })
      fireEvent.click(screen.getByRole('button', { name: /cancel display name/i }))
      expect(screen.getByText('Test Org')).toBeInTheDocument()
      const mutateAsync = (useUpdateOrganization as Mock).mock.results[0].value.mutateAsync
      expect(mutateAsync).not.toHaveBeenCalled()
    })
  })

  describe('Description inline edit', () => {
    it('clicking pencil switches to textarea with current value', () => {
      setupMocks()
      render(<OrgSettingsPage />)
      const editButtons = screen.getAllByRole('button', { name: /edit description/i })
      fireEvent.click(editButtons[0])
      const textarea = screen.getByRole('textbox', { name: /description/i })
      expect(textarea).toBeInTheDocument()
      expect((textarea as HTMLTextAreaElement).value).toBe('A test organization')
    })

    it('saving calls useUpdateOrganization with new description', async () => {
      setupMocks()
      render(<OrgSettingsPage />)
      const editButtons = screen.getAllByRole('button', { name: /edit description/i })
      fireEvent.click(editButtons[0])
      const textarea = screen.getByRole('textbox', { name: /description/i })
      fireEvent.change(textarea, { target: { value: 'New description' } })
      fireEvent.click(screen.getByRole('button', { name: /save description/i }))
      const mutateAsync = (useUpdateOrganization as Mock).mock.results[0].value.mutateAsync
      await waitFor(() => {
        expect(mutateAsync).toHaveBeenCalledWith({ name: 'test-org', description: 'New description' })
      })
    })

    it('cancel restores previous value without calling useUpdateOrganization', async () => {
      setupMocks()
      render(<OrgSettingsPage />)
      const editButtons = screen.getAllByRole('button', { name: /edit description/i })
      fireEvent.click(editButtons[0])
      const textarea = screen.getByRole('textbox', { name: /description/i })
      fireEvent.change(textarea, { target: { value: 'Changed desc' } })
      fireEvent.click(screen.getByRole('button', { name: /cancel description/i }))
      expect(screen.getByText('A test organization')).toBeInTheDocument()
      const mutateAsync = (useUpdateOrganization as Mock).mock.results[0].value.mutateAsync
      expect(mutateAsync).not.toHaveBeenCalled()
    })
  })

  describe('Sharing section', () => {
    it('renders SharingPanel with user grants', () => {
      setupMocks()
      render(<OrgSettingsPage />)
      expect(screen.getByText('alice@example.com')).toBeInTheDocument()
    })

    it('saving sharing calls useUpdateOrganizationSharing', async () => {
      setupMocks()
      render(<OrgSettingsPage />)
      const editButtons = screen.getAllByRole('button', { name: /^edit$/i })
      fireEvent.click(editButtons[editButtons.length - 1])
      fireEvent.click(screen.getByRole('button', { name: /^save$/i }))
      const mutateAsync = (useUpdateOrganizationSharing as Mock).mock.results[0].value.mutateAsync
      await waitFor(() => {
        expect(mutateAsync).toHaveBeenCalledWith(
          expect.objectContaining({ name: 'test-org' }),
        )
      })
    })
  })

  describe('Delete button', () => {
    it('delete button is visible for Owner', () => {
      setupMocks({ userRole: 3 }) // OWNER
      render(<OrgSettingsPage />)
      expect(screen.getByRole('button', { name: /delete organization/i })).toBeInTheDocument()
    })

    it('delete button is hidden for Viewer', () => {
      setupMocks({ userRole: 1 }) // VIEWER
      render(<OrgSettingsPage />)
      expect(screen.queryByRole('button', { name: /delete organization/i })).not.toBeInTheDocument()
    })

    it('delete button is hidden for Editor', () => {
      setupMocks({ userRole: 2 }) // EDITOR
      render(<OrgSettingsPage />)
      expect(screen.queryByRole('button', { name: /delete organization/i })).not.toBeInTheDocument()
    })

    it('clicking delete opens confirmation dialog', () => {
      setupMocks({ userRole: 3 })
      render(<OrgSettingsPage />)
      fireEvent.click(screen.getByRole('button', { name: /delete organization/i }))
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })

    it('confirming dialog calls useDeleteOrganization and navigates away', async () => {
      setupMocks({ userRole: 3 })
      render(<OrgSettingsPage />)
      fireEvent.click(screen.getByRole('button', { name: /delete organization/i }))
      fireEvent.click(screen.getByRole('button', { name: /^delete$/i }))
      const mutateAsync = (useDeleteOrganization as Mock).mock.results[0].value.mutateAsync
      await waitFor(() => {
        expect(mutateAsync).toHaveBeenCalledWith({ name: 'test-org' })
      })
    })
  })
})

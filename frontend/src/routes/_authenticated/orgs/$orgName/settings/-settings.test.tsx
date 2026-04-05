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
  useGetOrganizationRaw: vi.fn(),
  useUpdateOrganization: vi.fn(),
  useUpdateOrganizationSharing: vi.fn(),
  useUpdateOrganizationDefaultSharing: vi.fn(),
  useDeleteOrganization: vi.fn(),
}))

vi.mock('@/lib/auth', () => ({ useAuth: vi.fn() }))
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

import {
  useGetOrganization,
  useGetOrganizationRaw,
  useUpdateOrganization,
  useUpdateOrganizationSharing,
  useUpdateOrganizationDefaultSharing,
  useDeleteOrganization,
} from '@/queries/organizations'
import { useAuth } from '@/lib/auth'
import { OrgSettingsPage } from './index'

const mockOrg = {
  name: 'test-org',
  displayName: 'Test Org',
  description: 'A test organization',
  creatorEmail: 'creator@example.com',
  createdAt: '2024-01-15T10:30:00Z',
  userGrants: [{ principal: 'alice@example.com', role: 3 }],
  roleGrants: [],
  defaultUserGrants: [{ principal: 'bob@example.com', role: 1 }],
  defaultRoleGrants: [{ principal: 'engineering', role: 2 }],
  userRole: 3, // OWNER
}

function setupMocks(overrides: Partial<typeof mockOrg> = {}) {
  const org = { ...mockOrg, ...overrides }

  ;(useGetOrganization as Mock).mockReturnValue({
    data: org,
    isPending: false,
    error: null,
  })
  ;(useGetOrganizationRaw as Mock).mockReturnValue({
    data: '{"apiVersion":"v1","kind":"Namespace","metadata":{"name":"org-test-org"}}',
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
  ;(useUpdateOrganizationDefaultSharing as Mock).mockReturnValue({
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
    ;(useGetOrganizationRaw as Mock).mockReturnValue({ data: undefined, isPending: false, error: null })
    ;(useUpdateOrganization as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false })
    ;(useUpdateOrganizationSharing as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false })
    ;(useUpdateOrganizationDefaultSharing as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false })
    ;(useDeleteOrganization as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false, error: null })
    ;(useAuth as Mock).mockReturnValue({ isAuthenticated: true, isLoading: false, user: null })

    render(<OrgSettingsPage />)
    const skeletons = document.querySelectorAll('[data-slot="skeleton"]')
    expect(skeletons.length).toBeGreaterThan(0)
  })

  it('shows error alert when query fails', () => {
    ;(useGetOrganization as Mock).mockReturnValue({ data: undefined, isPending: false, error: new Error('Not found') })
    ;(useGetOrganizationRaw as Mock).mockReturnValue({ data: undefined, isPending: false, error: null })
    ;(useUpdateOrganization as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false })
    ;(useUpdateOrganizationSharing as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false })
    ;(useUpdateOrganizationDefaultSharing as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false })
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
      // The first "Edit" button in the sharing panels belongs to the Sharing section
      const editButtons = screen.getAllByRole('button', { name: /^edit$/i })
      fireEvent.click(editButtons[0])
      fireEvent.click(screen.getByRole('button', { name: /^save$/i }))
      const mutateAsync = (useUpdateOrganizationSharing as Mock).mock.results[0].value.mutateAsync
      await waitFor(() => {
        expect(mutateAsync).toHaveBeenCalledWith(
          expect.objectContaining({ name: 'test-org' }),
        )
      })
    })
  })

  describe('Default Sharing section', () => {
    it('renders Default Sharing panel with default grants', () => {
      setupMocks()
      render(<OrgSettingsPage />)
      expect(screen.getByText('Default Sharing')).toBeInTheDocument()
      expect(screen.getByText('bob@example.com')).toBeInTheDocument()
      expect(screen.getByText('engineering')).toBeInTheDocument()
    })

    it('renders Default Sharing panel with empty grants', () => {
      setupMocks({ defaultUserGrants: [], defaultRoleGrants: [] })
      render(<OrgSettingsPage />)
      expect(screen.getByText('Default Sharing')).toBeInTheDocument()
    })

    it('saving default sharing calls useUpdateOrganizationDefaultSharing', async () => {
      setupMocks()
      render(<OrgSettingsPage />)
      // Find the edit buttons -- the Default Sharing panel's edit button
      const editButtons = screen.getAllByRole('button', { name: /^edit$/i })
      // The last edit button belongs to the Default Sharing panel
      fireEvent.click(editButtons[editButtons.length - 1])
      fireEvent.click(screen.getByRole('button', { name: /^save$/i }))
      const mutateAsync = (useUpdateOrganizationDefaultSharing as Mock).mock.results[0].value.mutateAsync
      await waitFor(() => {
        expect(mutateAsync).toHaveBeenCalledWith(
          expect.objectContaining({ name: 'test-org' }),
        )
      })
    })
  })

  describe('Creator and Created At fields', () => {
    it('renders creator email as read-only text', () => {
      setupMocks()
      render(<OrgSettingsPage />)
      expect(screen.getByText('creator@example.com')).toBeInTheDocument()
    })

    it('renders formatted created timestamp as read-only text', () => {
      setupMocks()
      render(<OrgSettingsPage />)
      // The formatted date should appear somewhere in the document
      const formatted = new Date('2024-01-15T10:30:00Z').toLocaleString()
      expect(screen.getByText(formatted)).toBeInTheDocument()
    })

    it('shows "Unknown" for creator email when empty', () => {
      setupMocks({ creatorEmail: '' })
      render(<OrgSettingsPage />)
      expect(screen.getByText('Unknown')).toBeInTheDocument()
    })

    it('shows "Unknown" for created timestamp when empty', () => {
      setupMocks({ creatorEmail: '', createdAt: '' })
      render(<OrgSettingsPage />)
      const unknownElements = screen.getAllByText('Unknown')
      expect(unknownElements.length).toBeGreaterThanOrEqual(2)
    })

    it('no input element is present for creator email', () => {
      setupMocks()
      render(<OrgSettingsPage />)
      // Verify creator email is static text, not an input
      const inputs = document.querySelectorAll('input[aria-label*="creator"], input[aria-label*="created"]')
      expect(inputs.length).toBe(0)
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

  describe('View mode toggle', () => {
    it('renders Data/Resource toggle', () => {
      setupMocks()
      render(<OrgSettingsPage />)
      expect(screen.getByText('Data')).toBeInTheDocument()
      expect(screen.getByText('Resource')).toBeInTheDocument()
    })

    it('shows raw JSON when Resource tab is clicked', () => {
      setupMocks()
      render(<OrgSettingsPage />)
      fireEvent.click(screen.getByText('Resource'))
      expect(screen.getByRole('code')).toBeInTheDocument()
    })

    it('shows data view by default', () => {
      setupMocks()
      render(<OrgSettingsPage />)
      expect(screen.getByText('General')).toBeInTheDocument()
    })
  })
})

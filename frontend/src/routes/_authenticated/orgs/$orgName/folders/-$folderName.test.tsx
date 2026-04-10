import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => ({ orgName: 'test-org', folderName: 'payments' }),
    }),
    Link: ({
      children,
      className,
      to,
      params,
    }: {
      children: React.ReactNode
      className?: string
      to?: string
      params?: Record<string, string>
    }) => (
      <a href={to} data-params={JSON.stringify(params)} className={className}>
        {children}
      </a>
    ),
    useNavigate: () => vi.fn(),
  }
})

vi.mock('@/queries/folders', () => ({
  useGetFolder: vi.fn(),
  useUpdateFolder: vi.fn(),
}))

vi.mock('@/queries/organizations', () => ({
  useGetOrganization: vi.fn(),
}))

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

import { useGetFolder, useUpdateFolder } from '@/queries/folders'
import { useGetOrganization } from '@/queries/organizations'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { FolderDetailPage } from './$folderName/index'

const mockFolder = {
  name: 'payments',
  displayName: 'Payments Team',
  description: 'Payment processing projects',
  organization: 'test-org',
  creatorEmail: 'admin@example.com',
  createdAt: '',
  userRole: Role.OWNER,
  userGrants: [],
  roleGrants: [],
}

function setupMocks(userRole = Role.OWNER, folderOverride?: object) {
  const folder = { ...mockFolder, ...folderOverride }
  ;(useGetFolder as Mock).mockReturnValue({
    data: folder,
    isPending: false,
    error: null,
  })
  ;(useGetOrganization as Mock).mockReturnValue({
    data: { name: 'test-org', userRole },
    isPending: false,
    error: null,
  })
  ;(useUpdateFolder as Mock).mockReturnValue({
    mutateAsync: vi.fn().mockResolvedValue({}),
    isPending: false,
  })
}

describe('FolderDetailPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders folder display name', () => {
    setupMocks()
    render(<FolderDetailPage orgName="test-org" folderName="payments" />)
    // Display name appears in the h2 heading and in the display name field
    const matches = screen.getAllByText('Payments Team')
    expect(matches.length).toBeGreaterThanOrEqual(1)
  })

  it('renders folder slug', () => {
    setupMocks()
    render(<FolderDetailPage orgName="test-org" folderName="payments" />)
    expect(screen.getByText('payments')).toBeInTheDocument()
  })

  it('renders organization name', () => {
    setupMocks()
    render(<FolderDetailPage orgName="test-org" folderName="payments" />)
    // Multiple occurrences (breadcrumb + org field)
    const matches = screen.getAllByText('test-org')
    expect(matches.length).toBeGreaterThanOrEqual(1)
  })

  it('renders creator email', () => {
    setupMocks()
    render(<FolderDetailPage orgName="test-org" folderName="payments" />)
    expect(screen.getByText('admin@example.com')).toBeInTheDocument()
  })

  it('renders folder description', () => {
    setupMocks()
    render(<FolderDetailPage orgName="test-org" folderName="payments" />)
    expect(screen.getByText('Payment processing projects')).toBeInTheDocument()
  })

  it('shows loading skeletons while pending', () => {
    ;(useGetFolder as Mock).mockReturnValue({ data: undefined, isPending: true, error: null })
    ;(useGetOrganization as Mock).mockReturnValue({ data: { userRole: Role.OWNER }, isPending: true, error: null })
    ;(useUpdateFolder as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false })
    render(<FolderDetailPage orgName="test-org" folderName="payments" />)
    expect(screen.queryByText('Payments Team')).not.toBeInTheDocument()
  })

  it('shows error alert when fetch fails', () => {
    ;(useGetFolder as Mock).mockReturnValue({ data: undefined, isPending: false, error: new Error('not found') })
    ;(useGetOrganization as Mock).mockReturnValue({ data: { userRole: Role.OWNER }, isPending: false, error: null })
    ;(useUpdateFolder as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false })
    render(<FolderDetailPage orgName="test-org" folderName="payments" />)
    expect(screen.getByText('not found')).toBeInTheDocument()
  })

  describe('display name editing', () => {
    it('shows edit pencil button for OWNER', () => {
      setupMocks(Role.OWNER)
      render(<FolderDetailPage orgName="test-org" folderName="payments" />)
      expect(screen.getByRole('button', { name: /edit display name/i })).toBeInTheDocument()
    })

    it('shows edit pencil button for EDITOR', () => {
      setupMocks(Role.EDITOR)
      render(<FolderDetailPage orgName="test-org" folderName="payments" />)
      expect(screen.getByRole('button', { name: /edit display name/i })).toBeInTheDocument()
    })

    it('does not show edit pencil button for VIEWER', () => {
      setupMocks(Role.VIEWER)
      render(<FolderDetailPage orgName="test-org" folderName="payments" />)
      expect(screen.queryByRole('button', { name: /edit display name/i })).not.toBeInTheDocument()
    })

    it('clicking edit display name shows input', async () => {
      setupMocks(Role.OWNER)
      const user = userEvent.setup()
      render(<FolderDetailPage orgName="test-org" folderName="payments" />)
      await user.click(screen.getByRole('button', { name: /edit display name/i }))
      expect(screen.getByRole('textbox', { name: /display name/i })).toBeInTheDocument()
    })

    it('saving display name calls updateFolder', async () => {
      setupMocks(Role.OWNER)
      const user = userEvent.setup()
      render(<FolderDetailPage orgName="test-org" folderName="payments" />)
      await user.click(screen.getByRole('button', { name: /edit display name/i }))
      const input = screen.getByRole('textbox', { name: /display name/i })
      await user.clear(input)
      await user.type(input, 'New Name')
      await user.click(screen.getByRole('button', { name: /save display name/i }))
      const mutateAsync = (useUpdateFolder as Mock).mock.results[0].value.mutateAsync
      await waitFor(() => {
        expect(mutateAsync).toHaveBeenCalledWith(expect.objectContaining({ displayName: 'New Name' }))
      })
    })

    it('cancel restores view mode without saving', async () => {
      setupMocks(Role.OWNER)
      const user = userEvent.setup()
      render(<FolderDetailPage orgName="test-org" folderName="payments" />)
      await user.click(screen.getByRole('button', { name: /edit display name/i }))
      await user.click(screen.getByRole('button', { name: /cancel display name/i }))
      expect(screen.queryByRole('textbox', { name: /display name/i })).not.toBeInTheDocument()
      const mutateAsync = (useUpdateFolder as Mock).mock.results[0].value.mutateAsync
      expect(mutateAsync).not.toHaveBeenCalled()
    })
  })

  describe('description editing', () => {
    it('shows edit pencil button for OWNER', () => {
      setupMocks(Role.OWNER)
      render(<FolderDetailPage orgName="test-org" folderName="payments" />)
      expect(screen.getByRole('button', { name: /edit description/i })).toBeInTheDocument()
    })

    it('clicking edit description shows textarea', async () => {
      setupMocks(Role.OWNER)
      const user = userEvent.setup()
      render(<FolderDetailPage orgName="test-org" folderName="payments" />)
      await user.click(screen.getByRole('button', { name: /edit description/i }))
      expect(screen.getByRole('textbox', { name: /description/i })).toBeInTheDocument()
    })
  })

  describe('breadcrumb navigation', () => {
    it('renders org link in breadcrumb', () => {
      setupMocks()
      render(<FolderDetailPage orgName="test-org" folderName="payments" />)
      const orgLink = screen.getByRole('link', { name: 'test-org' })
      expect(orgLink).toBeInTheDocument()
    })

    it('renders Folders link in breadcrumb', () => {
      setupMocks()
      render(<FolderDetailPage orgName="test-org" folderName="payments" />)
      expect(screen.getByRole('link', { name: 'Folders' })).toBeInTheDocument()
    })
  })

  describe('delete dialog', () => {
    it('shows Delete Folder button for OWNER', () => {
      setupMocks(Role.OWNER)
      render(<FolderDetailPage orgName="test-org" folderName="payments" />)
      expect(screen.getByRole('button', { name: /delete folder/i })).toBeInTheDocument()
    })

    it('does not show Delete Folder button for VIEWER', () => {
      setupMocks(Role.VIEWER)
      render(<FolderDetailPage orgName="test-org" folderName="payments" />)
      expect(screen.queryByRole('button', { name: /delete folder/i })).not.toBeInTheDocument()
    })

    it('clicking Delete Folder opens confirmation dialog', async () => {
      setupMocks(Role.OWNER)
      const user = userEvent.setup()
      render(<FolderDetailPage orgName="test-org" folderName="payments" />)
      await user.click(screen.getByRole('button', { name: /delete folder/i }))
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })

    it('cancel closes dialog without deleting', async () => {
      setupMocks(Role.OWNER)
      const user = userEvent.setup()
      render(<FolderDetailPage orgName="test-org" folderName="payments" />)
      await user.click(screen.getByRole('button', { name: /delete folder/i }))
      await user.click(screen.getByRole('button', { name: /cancel/i }))
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
  })
})

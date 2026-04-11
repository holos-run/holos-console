import { render, screen, fireEvent } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

const mockNavigate = vi.fn()

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => ({ orgName: 'test-org' }),
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
    useNavigate: () => mockNavigate,
  }
})

vi.mock('@/queries/folders', () => ({
  useListFolders: vi.fn(),
  useCreateFolder: vi.fn(),
}))

vi.mock('@/queries/organizations', () => ({
  useGetOrganization: vi.fn(),
}))

vi.mock('@/components/create-folder-dialog', () => ({
  CreateFolderDialog: ({ open }: { open: boolean }) =>
    open ? <div data-testid="create-folder-dialog" /> : null,
}))

import { useListFolders } from '@/queries/folders'
import { useGetOrganization } from '@/queries/organizations'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { ParentType } from '@/gen/holos/console/v1/folders_pb'
import { FoldersIndexPage } from './index'

function makeFolder(name: string, displayName = '', description = '') {
  return {
    name,
    displayName,
    description,
    organization: 'test-org',
    parentType: ParentType.ORGANIZATION,
    parentName: 'test-org',
    userRole: Role.OWNER,
    userGrants: [],
    roleGrants: [],
    creatorEmail: '',
    createdAt: '',
  }
}

function setupMocks(folders = [makeFolder('payments', 'Payments Team')], userRole = Role.OWNER) {
  ;(useListFolders as Mock).mockReturnValue({
    data: folders,
    isLoading: false,
    error: null,
  })
  ;(useGetOrganization as Mock).mockReturnValue({
    data: { name: 'test-org', userRole },
    isPending: false,
    error: null,
  })
}

describe('FoldersIndexPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders loading skeletons while query is pending', () => {
    ;(useListFolders as Mock).mockReturnValue({
      data: undefined,
      isLoading: true,
      error: null,
    })
    ;(useGetOrganization as Mock).mockReturnValue({
      data: { name: 'test-org', userRole: Role.OWNER },
      isPending: false,
      error: null,
    })
    render(<FoldersIndexPage orgName="test-org" />)
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
  })

  it('renders empty state when no folders exist', () => {
    setupMocks([])
    render(<FoldersIndexPage orgName="test-org" />)
    expect(screen.getByText(/no folders/i)).toBeInTheDocument()
  })

  it('renders folder names in the table', () => {
    setupMocks([
      makeFolder('payments', 'Payments Team'),
      makeFolder('platform', 'Platform Team'),
    ])
    render(<FoldersIndexPage orgName="test-org" />)
    expect(screen.getByText('Payments Team')).toBeInTheDocument()
    expect(screen.getByText('Platform Team')).toBeInTheDocument()
  })

  it('renders folder slugs in the table', () => {
    setupMocks([makeFolder('payments', 'Payments Team')])
    render(<FoldersIndexPage orgName="test-org" />)
    expect(screen.getByText('payments')).toBeInTheDocument()
  })

  it('shows Create Folder button for OWNER', () => {
    setupMocks([makeFolder('payments', 'Payments')], Role.OWNER)
    render(<FoldersIndexPage orgName="test-org" />)
    expect(screen.getByRole('button', { name: /create folder/i })).toBeInTheDocument()
  })

  it('shows Create Folder button for EDITOR', () => {
    setupMocks([makeFolder('payments', 'Payments')], Role.EDITOR)
    render(<FoldersIndexPage orgName="test-org" />)
    expect(screen.getByRole('button', { name: /create folder/i })).toBeInTheDocument()
  })

  it('does not show Create Folder button for VIEWER', () => {
    setupMocks([makeFolder('payments', 'Payments')], Role.VIEWER)
    render(<FoldersIndexPage orgName="test-org" />)
    expect(screen.queryByRole('button', { name: /create folder/i })).not.toBeInTheDocument()
  })

  it('clicking Create Folder button opens dialog', () => {
    setupMocks([makeFolder('payments', 'Payments')], Role.OWNER)
    render(<FoldersIndexPage orgName="test-org" />)
    fireEvent.click(screen.getByRole('button', { name: /create folder/i }))
    expect(screen.getByTestId('create-folder-dialog')).toBeInTheDocument()
  })

  it('renders error alert when query fails', () => {
    ;(useListFolders as Mock).mockReturnValue({
      data: undefined,
      isLoading: false,
      error: new Error('failed to load folders'),
    })
    ;(useGetOrganization as Mock).mockReturnValue({
      data: { name: 'test-org', userRole: Role.OWNER },
      isPending: false,
      error: null,
    })
    render(<FoldersIndexPage orgName="test-org" />)
    expect(screen.getByText(/failed to load folders/i)).toBeInTheDocument()
  })

  it('clicking a folder row navigates to folder detail', () => {
    setupMocks([makeFolder('payments', 'Payments Team')])
    render(<FoldersIndexPage orgName="test-org" />)
    const row = screen.getByText('Payments Team').closest('tr')!
    fireEvent.click(row)
    expect(mockNavigate).toHaveBeenCalledWith({
      to: '/folders/$folderName',
      params: { folderName: 'payments' },
    })
  })

  it('search input filters visible rows', () => {
    setupMocks([
      makeFolder('payments', 'Payments Team'),
      makeFolder('platform', 'Platform Team'),
    ])
    render(<FoldersIndexPage orgName="test-org" />)
    const searchInput = screen.getByPlaceholderText(/search/i)
    fireEvent.change(searchInput, { target: { value: 'payments' } })
    expect(screen.getByText('Payments Team')).toBeInTheDocument()
    expect(screen.queryByText('Platform Team')).not.toBeInTheDocument()
  })
})

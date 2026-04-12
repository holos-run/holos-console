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
      useParams: () => ({ folderName: 'payments' }),
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
  useGetFolder: vi.fn(),
  useListFolders: vi.fn(),
}))

vi.mock('@/queries/projects', () => ({
  useListProjectsByParent: vi.fn(),
}))

vi.mock('@/queries/organizations', () => ({
  useGetOrganization: vi.fn(),
}))

vi.mock('@/components/create-folder-dialog', () => ({
  CreateFolderDialog: ({ open }: { open: boolean }) =>
    open ? <div data-testid="create-folder-dialog" /> : null,
}))

import { useGetFolder, useListFolders } from '@/queries/folders'
import { useListProjectsByParent } from '@/queries/projects'
import { useGetOrganization } from '@/queries/organizations'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { FolderIndexPage } from './index'

const mockFolder = {
  name: 'payments',
  displayName: 'Payments Team',
  description: 'Payment processing projects',
  organization: 'test-org',
  creatorEmail: 'admin@example.com',
  createdAt: '2025-01-15T10:00:00Z',
  userRole: Role.OWNER,
  userGrants: [],
  roleGrants: [],
}

function makeChildFolder(name: string, displayName: string, creatorEmail = 'admin@example.com') {
  return {
    name,
    displayName,
    description: '',
    organization: 'test-org',
    creatorEmail,
    createdAt: '2025-02-01T08:00:00Z',
    userRole: Role.OWNER,
  }
}

function makeChildProject(name: string, displayName: string, creatorEmail = 'admin@example.com') {
  return {
    name,
    displayName,
    description: '',
    organization: 'test-org',
    creatorEmail,
    createdAt: '2025-03-01T12:00:00Z',
    userRole: Role.OWNER,
  }
}

function setupMocks(
  opts: {
    folder?: typeof mockFolder
    childFolders?: ReturnType<typeof makeChildFolder>[]
    childProjects?: ReturnType<typeof makeChildProject>[]
    userRole?: Role
    folderLoading?: boolean
    folderError?: Error | null
    foldersError?: Error | null
    projectsError?: Error | null
  } = {},
) {
  const {
    folder = mockFolder,
    childFolders = [],
    childProjects = [],
    userRole = Role.OWNER,
    folderLoading = false,
    folderError = null,
    foldersError = null,
    projectsError = null,
  } = opts

  ;(useGetFolder as Mock).mockReturnValue({
    data: folderLoading ? undefined : folder,
    isPending: folderLoading,
    error: folderError,
  })
  ;(useListFolders as Mock).mockReturnValue({
    data: foldersError ? undefined : childFolders,
    isPending: false,
    error: foldersError,
  })
  ;(useListProjectsByParent as Mock).mockReturnValue({
    data: projectsError ? undefined : childProjects,
    isPending: false,
    error: projectsError,
  })
  ;(useGetOrganization as Mock).mockReturnValue({
    data: { name: 'test-org', userRole },
    isPending: false,
    error: null,
  })
}

describe('FolderIndexPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders loading skeletons while folder is pending', () => {
    setupMocks({ folderLoading: true })
    render(<FolderIndexPage folderName="payments" />)
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
  })

  it('renders error alert when folder fetch fails', () => {
    setupMocks({ folderError: new Error('folder not found') })
    render(<FolderIndexPage folderName="payments" />)
    expect(screen.getByText(/folder not found/i)).toBeInTheDocument()
  })

  it('renders empty state when folder has no children', () => {
    setupMocks()
    render(<FolderIndexPage folderName="payments" />)
    expect(screen.getByText(/no items/i)).toBeInTheDocument()
  })

  it('renders data grid with combined folder and project rows', () => {
    setupMocks({
      childFolders: [makeChildFolder('billing', 'Billing')],
      childProjects: [makeChildProject('checkout', 'Checkout')],
    })
    render(<FolderIndexPage folderName="payments" />)
    expect(screen.getByText('Billing')).toBeInTheDocument()
    expect(screen.getByText('Checkout')).toBeInTheDocument()
  })

  it('renders type badges for folders and projects', () => {
    setupMocks({
      childFolders: [makeChildFolder('billing', 'Billing')],
      childProjects: [makeChildProject('checkout', 'Checkout')],
    })
    render(<FolderIndexPage folderName="payments" />)
    expect(screen.getByText('Folder')).toBeInTheDocument()
    expect(screen.getByText('Project')).toBeInTheDocument()
  })

  it('search filters rows by display name', () => {
    setupMocks({
      childFolders: [makeChildFolder('billing', 'Billing')],
      childProjects: [makeChildProject('checkout', 'Checkout')],
    })
    render(<FolderIndexPage folderName="payments" />)
    const searchInput = screen.getByPlaceholderText(/search/i)
    fireEvent.change(searchInput, { target: { value: 'billing' } })
    expect(screen.getByText('Billing')).toBeInTheDocument()
    expect(screen.queryByText('Checkout')).not.toBeInTheDocument()
  })

  it('clicking a folder row navigates to /folders/$childFolderName', () => {
    setupMocks({
      childFolders: [makeChildFolder('billing', 'Billing')],
    })
    render(<FolderIndexPage folderName="payments" />)
    const row = screen.getByText('Billing').closest('tr')!
    fireEvent.click(row)
    expect(mockNavigate).toHaveBeenCalledWith({
      to: '/folders/$folderName',
      params: { folderName: 'billing' },
    })
  })

  it('clicking a project row navigates to /projects/$projectName', () => {
    setupMocks({
      childProjects: [makeChildProject('checkout', 'Checkout')],
    })
    render(<FolderIndexPage folderName="payments" />)
    const row = screen.getByText('Checkout').closest('tr')!
    fireEvent.click(row)
    expect(mockNavigate).toHaveBeenCalledWith({
      to: '/projects/$projectName',
      params: { projectName: 'checkout' },
    })
  })

  it('settings link points to /folders/$folderName/settings', () => {
    setupMocks()
    render(<FolderIndexPage folderName="payments" />)
    const settingsLink = screen.getByRole('link', { name: /settings/i })
    expect(settingsLink).toBeInTheDocument()
    expect(settingsLink).toHaveAttribute('href', '/folders/$folderName/settings')
  })

  it('shows Create Folder button for OWNER', () => {
    setupMocks({ userRole: Role.OWNER })
    render(<FolderIndexPage folderName="payments" />)
    const buttons = screen.getAllByRole('button', { name: /create folder/i })
    expect(buttons.length).toBeGreaterThanOrEqual(1)
  })

  it('shows Create Folder button for EDITOR', () => {
    setupMocks({ userRole: Role.EDITOR })
    render(<FolderIndexPage folderName="payments" />)
    const buttons = screen.getAllByRole('button', { name: /create folder/i })
    expect(buttons.length).toBeGreaterThanOrEqual(1)
  })

  it('does not show Create Folder button for VIEWER', () => {
    setupMocks({ userRole: Role.VIEWER })
    render(<FolderIndexPage folderName="payments" />)
    expect(screen.queryByRole('button', { name: /create folder/i })).not.toBeInTheDocument()
  })

  it('clicking Create Folder button opens dialog', () => {
    setupMocks({
      userRole: Role.OWNER,
      childFolders: [makeChildFolder('billing', 'Billing')],
    })
    render(<FolderIndexPage folderName="payments" />)
    fireEvent.click(screen.getByRole('button', { name: /create folder/i }))
    expect(screen.getByTestId('create-folder-dialog')).toBeInTheDocument()
  })

  it('renders creator email in the table', () => {
    setupMocks({
      childFolders: [makeChildFolder('billing', 'Billing', 'creator@example.com')],
    })
    render(<FolderIndexPage folderName="payments" />)
    expect(screen.getByText('creator@example.com')).toBeInTheDocument()
  })

  it('renders error alert when child folders fetch fails', () => {
    setupMocks({ foldersError: new Error('failed to load child folders') })
    render(<FolderIndexPage folderName="payments" />)
    expect(screen.getByText(/failed to load child folders/i)).toBeInTheDocument()
  })

  it('renders error alert when child projects fetch fails', () => {
    setupMocks({ projectsError: new Error('failed to load child projects') })
    render(<FolderIndexPage folderName="payments" />)
    expect(screen.getByText(/failed to load child projects/i)).toBeInTheDocument()
  })
})

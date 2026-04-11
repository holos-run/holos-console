import { render, screen, fireEvent } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => ({ orgName: 'test-org', folderName: 'test-folder' }),
    }),
    useNavigate: () => vi.fn(),
    Link: ({ children, ...props }: React.AnchorHTMLAttributes<HTMLAnchorElement> & { children: React.ReactNode }) =>
      <a {...props}>{children}</a>,
  }
})

vi.mock('@/queries/folders', () => ({
  useGetFolder: vi.fn(),
  useGetFolderRaw: vi.fn(),
  useUpdateFolder: vi.fn(),
}))

vi.mock('@/queries/organizations', () => ({
  useGetOrganization: vi.fn(),
}))

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

import { useGetFolder, useGetFolderRaw, useUpdateFolder } from '@/queries/folders'
import { useGetOrganization } from '@/queries/organizations'
import { FolderDetailPage } from './index'

const mockFolder = {
  name: 'test-folder',
  displayName: 'Test Folder',
  description: 'A test folder',
  creatorEmail: 'creator@example.com',
  organization: 'test-org',
}

const mockOrg = {
  name: 'test-org',
  displayName: 'Test Org',
  userRole: 3, // OWNER
}

function setupMocks(overrides: { folder?: Partial<typeof mockFolder>; org?: Partial<typeof mockOrg> } = {}) {
  const folder = { ...mockFolder, ...overrides.folder }
  const org = { ...mockOrg, ...overrides.org }

  ;(useGetFolder as Mock).mockReturnValue({
    data: folder,
    isPending: false,
    error: null,
  })
  ;(useGetFolderRaw as Mock).mockReturnValue({
    data: '{"apiVersion":"v1","kind":"Namespace","metadata":{"name":"fld-test-folder"}}',
    isPending: false,
    error: null,
  })
  ;(useGetOrganization as Mock).mockReturnValue({
    data: org,
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

  describe('View mode toggle', () => {
    it('renders Data/Resource toggle buttons', () => {
      setupMocks()
      render(<FolderDetailPage orgName="test-org" folderName="test-folder" />)
      expect(screen.getByText('Data')).toBeInTheDocument()
      expect(screen.getByText('Resource')).toBeInTheDocument()
    })

    it('shows Data view (General section) by default', () => {
      setupMocks()
      render(<FolderDetailPage orgName="test-org" folderName="test-folder" />)
      expect(screen.getByText('General')).toBeInTheDocument()
    })

    it('shows raw JSON when Resource tab is clicked', () => {
      setupMocks()
      render(<FolderDetailPage orgName="test-org" folderName="test-folder" />)
      fireEvent.click(screen.getByText('Resource'))
      expect(screen.getByRole('code')).toBeInTheDocument()
    })

    it('shows Data view content when Data is clicked after switching to Resource', () => {
      setupMocks()
      render(<FolderDetailPage orgName="test-org" folderName="test-folder" />)
      fireEvent.click(screen.getByText('Resource'))
      expect(screen.queryByText('General')).not.toBeInTheDocument()
      fireEvent.click(screen.getByText('Data'))
      expect(screen.getByText('General')).toBeInTheDocument()
    })
  })
})

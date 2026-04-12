import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => ({ folderName: 'test-folder' }),
    }),
    useNavigate: () => vi.fn(),
    Navigate: () => null,
    Link: ({ children, ...props }: React.AnchorHTMLAttributes<HTMLAnchorElement> & { children: React.ReactNode }) =>
      <a {...props}>{children}</a>,
  }
})

vi.mock('@/queries/folders', () => ({
  useGetFolder: vi.fn(),
  useGetFolderRaw: vi.fn(),
  useUpdateFolder: vi.fn(),
  useListFolders: vi.fn(),
}))

vi.mock('@/queries/organizations', () => ({
  useGetOrganization: vi.fn(),
}))

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

import { useGetFolder, useGetFolderRaw, useUpdateFolder, useListFolders } from '@/queries/folders'
import { useGetOrganization } from '@/queries/organizations'
import { FolderDetailPage } from '@/routes/_authenticated/folders/$folderName/settings/index'

const mockFolder = {
  name: 'test-folder',
  displayName: 'Test Folder',
  description: 'A test folder',
  creatorEmail: 'creator@example.com',
  organization: 'test-org',
  parentType: 1, // ORGANIZATION
  parentName: 'test-org',
  userRole: 3, // OWNER
}

const mockOrg = {
  name: 'test-org',
  displayName: 'Test Org',
  userRole: 3, // OWNER
}

const mockFolders = [
  { name: 'test-folder', displayName: 'Test Folder', parentType: 1, parentName: 'test-org' },
  { name: 'other-folder', displayName: 'Other Folder', parentType: 1, parentName: 'test-org' },
  { name: 'child-folder', displayName: 'Child Folder', parentType: 2, parentName: 'test-folder' },
]

function setupMocks(overrides: { folder?: Partial<typeof mockFolder>; org?: Partial<typeof mockOrg>; folders?: typeof mockFolders } = {}) {
  const folder = { ...mockFolder, ...overrides.folder }
  const org = { ...mockOrg, ...overrides.org }
  const folders = overrides.folders ?? mockFolders

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
  ;(useListFolders as Mock).mockReturnValue({
    data: folders,
    isPending: false,
    error: null,
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

  describe('Parent section', () => {
    it('displays current parent as Organization when parentType is ORGANIZATION', () => {
      setupMocks({ folder: { parentType: 1, parentName: 'test-org' } })
      render(<FolderDetailPage orgName="test-org" folderName="test-folder" />)
      expect(screen.getByText('Parent')).toBeInTheDocument()
      expect(screen.getByText(/Organization: Test Org/)).toBeInTheDocument()
    })

    it('displays current parent as Folder when parentType is FOLDER', () => {
      setupMocks({ folder: { parentType: 2, parentName: 'other-folder' } })
      render(<FolderDetailPage orgName="test-org" folderName="test-folder" />)
      expect(screen.getByText(/Folder: Other Folder/)).toBeInTheDocument()
    })

    it('renders Change Parent button for OWNERs', () => {
      setupMocks({ folder: { userRole: 3 } })
      render(<FolderDetailPage orgName="test-org" folderName="test-folder" />)
      expect(screen.getByRole('button', { name: /change parent/i })).toBeInTheDocument()
    })

    it('does not render Change Parent button for VIEWERs', () => {
      setupMocks({ folder: { userRole: 1 }, org: { userRole: 1 } })
      render(<FolderDetailPage orgName="test-org" folderName="test-folder" />)
      expect(screen.queryByRole('button', { name: /change parent/i })).not.toBeInTheDocument()
    })

    it('shows confirmation dialog when selecting a new parent', async () => {
      setupMocks()
      render(<FolderDetailPage orgName="test-org" folderName="test-folder" />)
      fireEvent.click(screen.getByRole('button', { name: /change parent/i }))
      // Open the combobox popover
      fireEvent.click(screen.getByRole('combobox', { name: /parent picker/i }))
      // Select "Other Folder" from the combobox list
      await waitFor(() => {
        expect(screen.getByText('Other Folder')).toBeInTheDocument()
      })
      fireEvent.click(screen.getByText('Other Folder'))
      await waitFor(() => {
        expect(screen.getByText(/Move folder/i)).toBeInTheDocument()
      })
    })

    it('calls updateFolder with parentType and parentName on confirmation', async () => {
      setupMocks()
      render(<FolderDetailPage orgName="test-org" folderName="test-folder" />)
      fireEvent.click(screen.getByRole('button', { name: /change parent/i }))
      fireEvent.click(screen.getByRole('combobox', { name: /parent picker/i }))
      await waitFor(() => {
        expect(screen.getByText('Other Folder')).toBeInTheDocument()
      })
      fireEvent.click(screen.getByText('Other Folder'))
      await waitFor(() => {
        expect(screen.getByText(/Move folder/i)).toBeInTheDocument()
      })
      fireEvent.click(screen.getByRole('button', { name: /^move$/i }))
      const mutateAsync = (useUpdateFolder as Mock).mock.results[0].value.mutateAsync
      await waitFor(() => {
        expect(mutateAsync).toHaveBeenCalledWith(
          expect.objectContaining({ parentType: 2, parentName: 'other-folder' }),
        )
      })
    })

    it('excludes self and descendants from parent options', async () => {
      setupMocks()
      render(<FolderDetailPage orgName="test-org" folderName="test-folder" />)
      fireEvent.click(screen.getByRole('button', { name: /change parent/i }))
      // Open the combobox popover to inspect rendered items.
      fireEvent.click(screen.getByRole('combobox', { name: /parent picker/i }))
      await waitFor(() => {
        // Valid option: other-folder must be present in the popover.
        expect(screen.getByText('Other Folder')).toBeInTheDocument()
      })
      // Descendant (child-folder) must be excluded from the options.
      // child-folder does not appear anywhere else on the page, so absence
      // from the DOM confirms it was filtered out of the combobox items.
      expect(screen.queryByText('Child Folder')).not.toBeInTheDocument()
    })

    it('excludes folders that would exceed max folder depth', async () => {
      // Hierarchy: org -> level1 -> level2 -> level3
      // Moving test-folder (which has child-folder, subtreeDepth=2) under
      // level2 would produce depth 2+2=4 > maxFolderDepth(3). So level2
      // must be excluded. level1 (depth 1+2=3) is allowed.
      const deepFolders = [
        { name: 'test-folder', displayName: 'Test Folder', parentType: 1, parentName: 'test-org' },
        { name: 'child-folder', displayName: 'Child Folder', parentType: 2, parentName: 'test-folder' },
        { name: 'level1', displayName: 'Level 1', parentType: 1, parentName: 'test-org' },
        { name: 'level2', displayName: 'Level 2', parentType: 2, parentName: 'level1' },
        { name: 'level3', displayName: 'Level 3', parentType: 2, parentName: 'level2' },
      ]
      setupMocks({ folders: deepFolders })
      render(<FolderDetailPage orgName="test-org" folderName="test-folder" />)
      fireEvent.click(screen.getByRole('button', { name: /change parent/i }))
      fireEvent.click(screen.getByRole('combobox', { name: /parent picker/i }))
      await waitFor(() => {
        // level1 (depth=1) is valid: 1+2=3 <= 3
        expect(screen.getByText('Level 1')).toBeInTheDocument()
        // level2 (depth=2) is invalid: 2+2=4 > 3
        expect(screen.queryByText('Level 2')).not.toBeInTheDocument()
        // level3 is a descendant of level2, excluded by depth (3+2=5 > 3)
        expect(screen.queryByText('Level 3')).not.toBeInTheDocument()
      })
    })
  })
})

import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

vi.mock('@tanstack/react-router', () => ({
  useNavigate: () => vi.fn(),
}))

vi.mock('@/queries/organizations', () => ({
  useListOrganizations: vi.fn(),
  useGetOrganization: vi.fn(),
}))
vi.mock('@/queries/projects', () => ({
  useCreateProject: vi.fn(),
}))
vi.mock('@/queries/folders', () => ({
  useListFolders: vi.fn(),
}))
vi.mock('@/gen/holos/console/v1/folders_pb', () => ({
  ParentType: { UNSPECIFIED: 0, ORGANIZATION: 1, FOLDER: 2 },
}))
vi.mock('@/lib/auth', () => ({ useAuth: vi.fn() }))

import { useListOrganizations, useGetOrganization } from '@/queries/organizations'
import { useCreateProject } from '@/queries/projects'
import { useListFolders } from '@/queries/folders'
import { CreateProjectDialog } from './create-project-dialog'

const mockOrgs = [
  { name: 'acme', displayName: 'Acme Corp' },
  { name: 'other', displayName: 'Other Org' },
]

const mockFolders = [
  { name: 'default', displayName: 'Default', organization: 'acme' },
  { name: 'staging', displayName: 'Staging', organization: 'acme' },
]

const mockMutateAsync = vi.fn().mockResolvedValue({ name: 'my-project' })

function setup(defaultOrganization?: string) {
  ;(useListOrganizations as Mock).mockReturnValue({
    data: { organizations: mockOrgs },
  })
  ;(useGetOrganization as Mock).mockReturnValue({
    data: { defaultFolder: 'default' },
    isPending: false,
    error: null,
  })
  ;(useListFolders as Mock).mockReturnValue({
    data: mockFolders,
    isPending: false,
    error: null,
  })
  ;(useCreateProject as Mock).mockReturnValue({
    mutateAsync: mockMutateAsync,
    isPending: false,
  })

  return render(
    <CreateProjectDialog
      open={true}
      onOpenChange={vi.fn()}
      defaultOrganization={defaultOrganization}
    />,
  )
}

describe('CreateProjectDialog', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockMutateAsync.mockResolvedValue({ name: 'my-project' })
  })

  it('renders the Folder combobox', () => {
    setup('acme')
    const folderCombobox = screen.getByRole('combobox', { name: /folder/i })
    expect(folderCombobox).toBeInTheDocument()
  })

  it('defaults folder to the org default folder', async () => {
    setup('acme')
    // The org has defaultFolder='default', so the combobox should show "Default"
    await waitFor(() => {
      const folderCombobox = screen.getByRole('combobox', { name: /folder/i })
      expect(folderCombobox).toHaveTextContent('Default')
    })
  })

  it('includes "None (organization root)" option in folder picker', () => {
    setup('acme')
    const folderCombobox = screen.getByRole('combobox', { name: /folder/i })
    fireEvent.click(folderCombobox)
    expect(screen.getByText('None (organization root)')).toBeInTheDocument()
  })

  it('shows folder display names in the picker', () => {
    setup('acme')
    const folderCombobox = screen.getByRole('combobox', { name: /folder/i })
    fireEvent.click(folderCombobox)
    expect(screen.getByText('Staging')).toBeInTheDocument()
  })

  it('sends parentType=FOLDER and parentName when a folder is selected', async () => {
    setup('acme')
    // Wait for the default folder to be set
    await waitFor(() => {
      const folderCombobox = screen.getByRole('combobox', { name: /folder/i })
      expect(folderCombobox).toHaveTextContent('Default')
    })

    // Fill in required fields
    fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'My Project' } })

    // Submit the form
    const form = screen.getByRole('form')
    fireEvent.submit(form)

    await waitFor(() => {
      expect(mockMutateAsync).toHaveBeenCalledWith(
        expect.objectContaining({
          parentType: 2, // FOLDER
          parentName: 'default',
          organization: 'acme',
        }),
      )
    })
  })

  it('sends parentType=ORGANIZATION when "None" is selected', async () => {
    // Set up with no default folder
    ;(useGetOrganization as Mock).mockReturnValue({
      data: { defaultFolder: '' },
      isPending: false,
      error: null,
    })

    render(
      <CreateProjectDialog
        open={true}
        onOpenChange={vi.fn()}
        defaultOrganization="acme"
      />,
    )

    // Fill in required fields
    fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'My Project' } })

    // Submit the form
    const form = screen.getByRole('form')
    fireEvent.submit(form)

    await waitFor(() => {
      expect(mockMutateAsync).toHaveBeenCalledWith(
        expect.objectContaining({
          parentType: 1, // ORGANIZATION
          parentName: 'acme',
          organization: 'acme',
        }),
      )
    })
  })
})

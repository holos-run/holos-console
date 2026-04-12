import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
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
    Link: ({
      children,
      ...props
    }: React.AnchorHTMLAttributes<HTMLAnchorElement> & { children: React.ReactNode }) => (
      <a {...props}>{children}</a>
    ),
  }
})

vi.mock('@/queries/templates', () => ({
  useListTemplates: vi.fn(),
  useCreateTemplate: vi.fn(),
  makeFolderScope: vi.fn().mockReturnValue({ scope: 3, scopeName: 'test-folder' }),
}))

vi.mock('@/queries/folders', () => ({
  useGetFolder: vi.fn(),
}))

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

import { useListTemplates, useCreateTemplate } from '@/queries/templates'
import { useGetFolder } from '@/queries/folders'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { FolderTemplatesIndexPage } from './index'

const mockTemplates = [
  {
    name: 'httproute-ingress',
    displayName: 'HTTPRoute Ingress',
    description: 'Provides an HTTPRoute for the istio-ingress gateway',
    cueTemplate: '// httproute template',
    mandatory: true,
    enabled: false,
  },
  {
    name: 'optional-template',
    displayName: 'Optional Template',
    description: 'An optional platform template',
    cueTemplate: '// optional template',
    mandatory: false,
    enabled: true,
  },
]

function setupMocks(userRole = Role.OWNER) {
  ;(useListTemplates as Mock).mockReturnValue({
    data: mockTemplates,
    isPending: false,
    error: null,
  })
  ;(useGetFolder as Mock).mockReturnValue({
    data: { name: 'test-folder', organization: 'test-org', userRole },
    isPending: false,
    error: null,
  })
  ;(useCreateTemplate as Mock).mockReturnValue({
    mutateAsync: vi.fn().mockResolvedValue({}),
    isPending: false,
  })
}

describe('FolderTemplatesIndexPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders template names', () => {
    setupMocks()
    render(<FolderTemplatesIndexPage folderName="test-folder" />)
    expect(screen.getByText('httproute-ingress')).toBeInTheDocument()
    expect(screen.getByText('optional-template')).toBeInTheDocument()
  })

  it('shows mandatory badge for mandatory templates', () => {
    setupMocks()
    render(<FolderTemplatesIndexPage folderName="test-folder" />)
    expect(screen.getByText('Mandatory')).toBeInTheDocument()
  })

  it('shows enabled badge for enabled templates', () => {
    setupMocks()
    render(<FolderTemplatesIndexPage folderName="test-folder" />)
    expect(screen.getByText('Enabled')).toBeInTheDocument()
  })

  it('shows disabled badge for disabled templates', () => {
    setupMocks()
    render(<FolderTemplatesIndexPage folderName="test-folder" />)
    expect(screen.getByText('Disabled')).toBeInTheDocument()
  })

  it('shows skeleton while loading', () => {
    ;(useListTemplates as Mock).mockReturnValue({
      data: undefined,
      isPending: true,
      error: null,
    })
    ;(useGetFolder as Mock).mockReturnValue({
      data: { name: 'test-folder', organization: 'test-org', userRole: Role.OWNER },
      isPending: false,
      error: null,
    })
    ;(useCreateTemplate as Mock).mockReturnValue({
      mutateAsync: vi.fn(),
      isPending: false,
    })
    render(<FolderTemplatesIndexPage folderName="test-folder" />)
    expect(screen.queryByText('httproute-ingress')).not.toBeInTheDocument()
  })

  it('shows error message when fetch fails', () => {
    ;(useListTemplates as Mock).mockReturnValue({
      data: undefined,
      isPending: false,
      error: new Error('Failed to load templates'),
    })
    ;(useGetFolder as Mock).mockReturnValue({
      data: { name: 'test-folder', organization: 'test-org', userRole: Role.OWNER },
      isPending: false,
      error: null,
    })
    ;(useCreateTemplate as Mock).mockReturnValue({
      mutateAsync: vi.fn(),
      isPending: false,
    })
    render(<FolderTemplatesIndexPage folderName="test-folder" />)
    expect(screen.getByText('Failed to load templates')).toBeInTheDocument()
  })

  it('shows empty state when no templates exist', () => {
    ;(useListTemplates as Mock).mockReturnValue({
      data: [],
      isPending: false,
      error: null,
    })
    ;(useGetFolder as Mock).mockReturnValue({
      data: { name: 'test-folder', organization: 'test-org', userRole: Role.OWNER },
      isPending: false,
      error: null,
    })
    ;(useCreateTemplate as Mock).mockReturnValue({
      mutateAsync: vi.fn(),
      isPending: false,
    })
    render(<FolderTemplatesIndexPage folderName="test-folder" />)
    expect(
      screen.getByText('No platform templates found for this folder.'),
    ).toBeInTheDocument()
  })

  it('renders search input when templates exist', () => {
    setupMocks()
    render(<FolderTemplatesIndexPage folderName="test-folder" />)
    expect(screen.getByPlaceholderText('Search templates...')).toBeInTheDocument()
  })

  it('filters templates by search input', async () => {
    setupMocks()
    const user = userEvent.setup()
    render(<FolderTemplatesIndexPage folderName="test-folder" />)
    const searchInput = screen.getByPlaceholderText('Search templates...')
    await user.type(searchInput, 'optional')
    expect(screen.getByText('optional-template')).toBeInTheDocument()
    expect(screen.queryByText('httproute-ingress')).not.toBeInTheDocument()
  })

  describe('create template button and dialog', () => {
    it('shows Create Template button for folder OWNER', () => {
      setupMocks(Role.OWNER)
      render(<FolderTemplatesIndexPage folderName="test-folder" />)
      expect(
        screen.getByRole('button', { name: /create template/i }),
      ).toBeInTheDocument()
    })

    it('does not show Create Template button for folder VIEWER', () => {
      setupMocks(Role.VIEWER)
      render(<FolderTemplatesIndexPage folderName="test-folder" />)
      expect(
        screen.queryByRole('button', { name: /create template/i }),
      ).not.toBeInTheDocument()
    })

    it('does not show Create Template button for folder EDITOR', () => {
      setupMocks(Role.EDITOR)
      render(<FolderTemplatesIndexPage folderName="test-folder" />)
      expect(
        screen.queryByRole('button', { name: /create template/i }),
      ).not.toBeInTheDocument()
    })

    it('clicking Create Template opens dialog', async () => {
      setupMocks(Role.OWNER)
      const user = userEvent.setup()
      render(<FolderTemplatesIndexPage folderName="test-folder" />)
      await user.click(screen.getByRole('button', { name: /create template/i }))
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })

    it('create dialog has enabled toggle defaulting to disabled', async () => {
      setupMocks(Role.OWNER)
      const user = userEvent.setup()
      render(<FolderTemplatesIndexPage folderName="test-folder" />)
      await user.click(screen.getByRole('button', { name: /create template/i }))
      const toggle = screen.getByRole('switch', { name: /enabled/i })
      expect(toggle).toBeInTheDocument()
      expect(toggle).toHaveAttribute('data-state', 'unchecked')
    })

    it('confirming create calls createTemplate with enabled state', async () => {
      setupMocks(Role.OWNER)
      const user = userEvent.setup()
      render(<FolderTemplatesIndexPage folderName="test-folder" />)
      await user.click(screen.getByRole('button', { name: /create template/i }))
      const nameInput = screen.getByRole('textbox', { name: /^name$/i })
      await user.type(nameInput, 'my-template')
      await user.click(screen.getByRole('switch', { name: /enabled/i }))
      await user.click(screen.getByRole('button', { name: /^create$/i }))
      const mutateAsync = (useCreateTemplate as Mock).mock.results[0].value
        .mutateAsync
      await waitFor(() => {
        expect(mutateAsync).toHaveBeenCalledWith(
          expect.objectContaining({
            name: 'my-template',
            enabled: true,
          }),
        )
      })
    })

    it('create with enabled defaulting to false passes disabled', async () => {
      setupMocks(Role.OWNER)
      const user = userEvent.setup()
      render(<FolderTemplatesIndexPage folderName="test-folder" />)
      await user.click(screen.getByRole('button', { name: /create template/i }))
      const nameInput = screen.getByRole('textbox', { name: /^name$/i })
      await user.type(nameInput, 'my-template')
      await user.click(screen.getByRole('button', { name: /^create$/i }))
      const mutateAsync = (useCreateTemplate as Mock).mock.results[0].value
        .mutateAsync
      await waitFor(() => {
        expect(mutateAsync).toHaveBeenCalledWith(
          expect.objectContaining({
            name: 'my-template',
            enabled: false,
          }),
        )
      })
    })

    it('cancel closes create dialog without saving', async () => {
      setupMocks(Role.OWNER)
      const user = userEvent.setup()
      render(<FolderTemplatesIndexPage folderName="test-folder" />)
      await user.click(screen.getByRole('button', { name: /create template/i }))
      await user.click(screen.getByRole('button', { name: /cancel/i }))
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
      const mutateAsync = (useCreateTemplate as Mock).mock.results[0].value
        .mutateAsync
      expect(mutateAsync).not.toHaveBeenCalled()
    })

    it('disables Create button when name is empty', async () => {
      setupMocks(Role.OWNER)
      const user = userEvent.setup()
      render(<FolderTemplatesIndexPage folderName="test-folder" />)
      await user.click(screen.getByRole('button', { name: /create template/i }))
      const createBtn = screen.getByRole('button', { name: /^create$/i })
      expect(createBtn).toBeDisabled()
    })

    it('shows validation error when name is whitespace only', async () => {
      setupMocks(Role.OWNER)
      const user = userEvent.setup()
      render(<FolderTemplatesIndexPage folderName="test-folder" />)
      await user.click(screen.getByRole('button', { name: /create template/i }))
      const nameInput = screen.getByRole('textbox', { name: /^name$/i })
      await user.type(nameInput, '   ')
      await user.click(screen.getByRole('button', { name: /^create$/i }))
      await waitFor(() => {
        expect(screen.getByText('Name is required')).toBeInTheDocument()
      })
      const mutateAsync = (useCreateTemplate as Mock).mock.results[0].value
        .mutateAsync
      expect(mutateAsync).not.toHaveBeenCalled()
    })

    it('shows error alert when create mutation fails', async () => {
      setupMocks(Role.OWNER)
      ;(useCreateTemplate as Mock).mockReturnValue({
        mutateAsync: vi.fn().mockRejectedValue(new Error('Template already exists')),
        isPending: false,
      })
      const user = userEvent.setup()
      render(<FolderTemplatesIndexPage folderName="test-folder" />)
      await user.click(screen.getByRole('button', { name: /create template/i }))
      const nameInput = screen.getByRole('textbox', { name: /^name$/i })
      await user.type(nameInput, 'existing-template')
      await user.click(screen.getByRole('button', { name: /^create$/i }))
      await waitFor(() => {
        expect(screen.getByText('Template already exists')).toBeInTheDocument()
      })
      // Dialog should remain open on error
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })

    it('Load Example populates fields', async () => {
      setupMocks(Role.OWNER)
      const user = userEvent.setup()
      render(<FolderTemplatesIndexPage folderName="test-folder" />)
      await user.click(screen.getByRole('button', { name: /create template/i }))
      await user.click(screen.getByRole('button', { name: /load example/i }))
      const nameInput = screen.getByRole('textbox', {
        name: /^name$/i,
      }) as HTMLInputElement
      expect(nameInput.value).toBe('httproute-ingress')
    })
  })
})

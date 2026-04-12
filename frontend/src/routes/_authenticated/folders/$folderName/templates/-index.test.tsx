import { render, screen } from '@testing-library/react'
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
      to,
      params,
      ...props
    }: React.AnchorHTMLAttributes<HTMLAnchorElement> & { children: React.ReactNode; to?: string; params?: Record<string, string> }) => (
      <a href={to} data-params={JSON.stringify(params)} {...props}>{children}</a>
    ),
  }
})

vi.mock('@/queries/templates', () => ({
  useListTemplates: vi.fn(),
  makeFolderScope: vi.fn().mockReturnValue({ scope: 2, scopeName: 'test-folder' }),
}))

vi.mock('@/queries/folders', () => ({
  useGetFolder: vi.fn(),
}))

import { useListTemplates } from '@/queries/templates'
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

  describe('create template button', () => {
    it('shows Create Template link for folder OWNER', () => {
      setupMocks(Role.OWNER)
      render(<FolderTemplatesIndexPage folderName="test-folder" />)
      const link = screen.getByRole('link', { name: /create template/i })
      expect(link).toBeInTheDocument()
      expect(link).toHaveAttribute('href', '/folders/$folderName/templates/new')
    })

    it('does not show Create Template link for folder VIEWER', () => {
      setupMocks(Role.VIEWER)
      render(<FolderTemplatesIndexPage folderName="test-folder" />)
      expect(
        screen.queryByRole('link', { name: /create template/i }),
      ).not.toBeInTheDocument()
    })

    it('does not show Create Template link for folder EDITOR', () => {
      setupMocks(Role.EDITOR)
      render(<FolderTemplatesIndexPage folderName="test-folder" />)
      expect(
        screen.queryByRole('link', { name: /create template/i }),
      ).not.toBeInTheDocument()
    })

    it('does not render a dialog', () => {
      setupMocks(Role.OWNER)
      render(<FolderTemplatesIndexPage folderName="test-folder" />)
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
  })
})

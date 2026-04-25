import { render, screen } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import { ParentType } from '@/gen/holos/console/v1/folders_pb'
import { LinkMock } from '@/test/link-mock'

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => ({ folderName: 'payments' }),
    }),
    Link: LinkMock,
  }
})

vi.mock('@/queries/projects', () => ({
  useListProjectsByParent: vi.fn(),
}))

vi.mock('@/queries/folders', () => ({
  useGetFolder: vi.fn(),
}))

import { useListProjectsByParent } from '@/queries/projects'
import { useGetFolder } from '@/queries/folders'
import { FolderProjectsIndexPage } from './index'

type ProjectFixture = {
  name: string
  displayName?: string
  description?: string
  creatorEmail?: string
}

const mockFolder = {
  name: 'payments',
  displayName: 'Payments Team',
  organization: 'test-org',
  creatorEmail: 'admin@example.com',
}

function setupMocks(
  projects: ProjectFixture[] | undefined = [],
  options: {
    folder?: typeof mockFolder | undefined
    isPending?: boolean
    error?: Error | null
  } = {},
) {
  ;(useGetFolder as Mock).mockReturnValue({
    data: options.folder ?? mockFolder,
    isPending: false,
    error: null,
  })
  ;(useListProjectsByParent as Mock).mockReturnValue({
    data: options.isPending ? undefined : projects,
    isPending: options.isPending ?? false,
    error: options.error ?? null,
  })
}

describe('FolderProjectsIndexPage', () => {
  beforeEach(() => vi.clearAllMocks())

  it('renders the breadcrumb and page title', () => {
    setupMocks([])
    render(<FolderProjectsIndexPage folderName="payments" />)
    // The card title is "Projects"; the breadcrumb shows org / Projects /
    // folder / Projects so a user always has a link back up the hierarchy.
    const headings = screen.getAllByText('Projects')
    expect(headings.length).toBeGreaterThan(0)
    expect(screen.getByRole('link', { name: 'test-org' })).toHaveAttribute(
      'href',
      '/organizations/test-org/settings',
    )
    expect(screen.getByRole('link', { name: 'Projects' })).toHaveAttribute(
      'href',
      '/organizations/test-org/projects',
    )
    expect(screen.getByRole('link', { name: 'payments' })).toHaveAttribute(
      'href',
      '/folders/payments/settings',
    )
  })

  it('renders the empty state when no projects live in the folder', () => {
    setupMocks([])
    render(<FolderProjectsIndexPage folderName="payments" />)
    // The zero-state copy explains where projects come from: projects live
    // directly under a folder only when their parent is set to that folder.
    expect(screen.getByText(/no projects in this folder/i)).toBeInTheDocument()
    expect(
      screen.getByText(/parent is set to this folder/i),
    ).toBeInTheDocument()
    // The populated-list <ul> must not exist in the empty state — the
    // data-testid gives a tight regression pin in case the empty branch is
    // accidentally dropped.
    expect(screen.queryByTestId('projects-list')).not.toBeInTheDocument()
  })

  it('renders a populated list with per-item links into the project scope', () => {
    setupMocks([
      {
        name: 'checkout',
        displayName: 'Checkout',
        description: 'Cart and checkout flow',
        creatorEmail: 'jane@example.com',
      },
      { name: 'billing' },
    ])
    render(<FolderProjectsIndexPage folderName="payments" />)

    // Each item is a Link whose href targets /projects/$projectName —
    // navigation from the folder-scoped projects index lands on the
    // project's own detail page, not a folder-qualified project route
    // (projects have one canonical detail URL).
    expect(screen.getByRole('link', { name: /Checkout/ })).toHaveAttribute(
      'href',
      '/projects/checkout',
    )
    expect(screen.getByRole('link', { name: /billing/ })).toHaveAttribute(
      'href',
      '/projects/billing',
    )

    // displayName renders as the primary label; the slug appears alongside
    // it when it differs from the display name so two projects with the
    // same display name can still be disambiguated by their underlying name.
    expect(screen.getByText('Checkout')).toBeInTheDocument()
    expect(screen.getByText('checkout')).toBeInTheDocument()

    // Supplementary metadata (description, creator email) renders when
    // present; projects without those fields (billing) render only the
    // label.
    expect(screen.getByText('Cart and checkout flow')).toBeInTheDocument()
    expect(screen.getByText(/Created by jane@example.com/)).toBeInTheDocument()
  })

  it('does not duplicate the slug when displayName equals name', () => {
    // When a project's display name equals its slug, showing both creates
    // a visually duplicated label. The implementation suppresses the slug
    // span in that case.
    setupMocks([
      { name: 'billing', displayName: 'billing' },
    ])
    render(<FolderProjectsIndexPage folderName="payments" />)
    // Exactly one "billing" text node (inside the link) — no secondary
    // slug badge.
    const matches = screen.getAllByText('billing')
    expect(matches).toHaveLength(1)
  })

  it('renders a skeleton while the query is pending', () => {
    setupMocks([], { isPending: true })
    render(<FolderProjectsIndexPage folderName="payments" />)
    expect(screen.queryByTestId('projects-list')).not.toBeInTheDocument()
    expect(screen.queryByText(/no projects in this folder/i)).not.toBeInTheDocument()
  })

  it('surfaces an error when the list query fails', () => {
    setupMocks([], { error: new Error('backend unreachable') })
    render(<FolderProjectsIndexPage folderName="payments" />)
    expect(screen.getByText('backend unreachable')).toBeInTheDocument()
  })

  it('queries the RPC with ParentType.FOLDER so results are non-recursive', () => {
    // The folder index page's Projects summary (HOL-610) and this scoped
    // index must agree on what "projects in this folder" means: immediate
    // children only, never grandchildren. Assert the call shape so a
    // regression to ORGANIZATION or a missing parentName is caught at the
    // unit-test boundary.
    setupMocks([])
    render(<FolderProjectsIndexPage folderName="payments" />)
    expect(useListProjectsByParent).toHaveBeenCalledWith(
      'test-org',
      ParentType.FOLDER,
      'payments',
    )
  })

  it('falls back to an empty org when the folder query has not resolved yet', () => {
    // The folder is the source of truth for orgName. When useGetFolder is
    // still resolving the project query must still be made with an empty
    // organization so the RPC layer's enabled-guard suppresses the call —
    // no "undefined" slipping into the org field.
    ;(useGetFolder as Mock).mockReturnValue({
      data: undefined,
      isPending: true,
      error: null,
    })
    ;(useListProjectsByParent as Mock).mockReturnValue({
      data: [],
      isPending: false,
      error: null,
    })
    render(<FolderProjectsIndexPage folderName="payments" />)
    expect(useListProjectsByParent).toHaveBeenCalledWith(
      '',
      ParentType.FOLDER,
      'payments',
    )
  })
})

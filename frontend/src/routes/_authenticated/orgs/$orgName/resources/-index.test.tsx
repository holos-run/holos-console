import { render, screen, fireEvent, within } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import { LinkMock } from '@/test/link-mock'

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => ({ orgName: 'test-org' }),
    }),
    Link: LinkMock,
  }
})

vi.mock('@/queries/resources', () => ({
  useListResources: vi.fn(),
}))

import { useListResources } from '@/queries/resources'
import { ResourcesIndexPage } from './index'
import { ResourceType } from '@/gen/holos/console/v1/resources_pb'

type PathElement = { name: string; displayName: string; type: ResourceType }
type Resource = {
  type: ResourceType
  path: PathElement[]
  displayName: string
  name: string
}

function orgPath(): PathElement {
  return {
    name: 'test-org',
    displayName: 'Test Org',
    type: ResourceType.UNSPECIFIED,
  }
}

function folderPath(name: string, displayName = ''): PathElement {
  return { name, displayName, type: ResourceType.FOLDER }
}

function makeFolder(
  name: string,
  displayName = '',
  ancestors: PathElement[] = [orgPath()],
): Resource {
  return { type: ResourceType.FOLDER, path: ancestors, displayName, name }
}

function makeProject(
  name: string,
  displayName = '',
  ancestors: PathElement[] = [orgPath()],
): Resource {
  return { type: ResourceType.PROJECT, path: ancestors, displayName, name }
}

function setupMocks(resources: Resource[] = []) {
  ;(useListResources as Mock).mockReturnValue({
    data: { resources },
    isLoading: false,
    error: null,
  })
}

describe('ResourcesIndexPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders loading skeletons while the query is pending', () => {
    ;(useListResources as Mock).mockReturnValue({
      data: undefined,
      isLoading: true,
      error: null,
    })
    render(<ResourcesIndexPage />)
    expect(screen.getByTestId('resources-loading')).toBeInTheDocument()
  })

  it('renders the error alert when the query fails', () => {
    ;(useListResources as Mock).mockReturnValue({
      data: undefined,
      isLoading: false,
      error: new Error('boom'),
    })
    render(<ResourcesIndexPage />)
    expect(screen.getByText('boom')).toBeInTheDocument()
  })

  it('renders the empty state when the org has no resources', () => {
    setupMocks([])
    render(<ResourcesIndexPage />)
    expect(screen.getByText(/no resources yet/i)).toBeInTheDocument()
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
  })

  it('renders a row for each resource with the correct type badge', () => {
    setupMocks([
      makeFolder('finance', 'Finance'),
      makeProject('billing', 'Billing'),
    ])
    render(<ResourcesIndexPage />)

    const rows = screen.getAllByRole('row')
    // Header row + 2 data rows
    expect(rows).toHaveLength(3)

    expect(within(rows[1]).getByText('Folder')).toBeInTheDocument()
    expect(within(rows[1]).getByTestId('resource-type-icon-folder')).toBeInTheDocument()

    expect(within(rows[2]).getByText('Project')).toBeInTheDocument()
    expect(within(rows[2]).getByTestId('resource-type-icon-project')).toBeInTheDocument()
  })

  it('renders clickable path elements with correct hrefs and slug titles', () => {
    const ancestorsUnderTeam = [orgPath(), folderPath('team-a', 'Team A')]
    setupMocks([makeProject('web', 'Web App', ancestorsUnderTeam)])
    render(<ResourcesIndexPage />)

    const orgLink = screen.getByRole('link', { name: 'Test Org' })
    expect(orgLink).toHaveAttribute('href', '/orgs/test-org')
    expect(orgLink).toHaveAttribute('title', 'test-org')

    const folderLink = screen.getByRole('link', { name: /Team A/ })
    expect(folderLink).toHaveAttribute('href', '/folders/team-a')
    expect(folderLink).toHaveAttribute('title', 'team-a')
    // Folder ancestor link shows the folder icon; org root (UNSPECIFIED) does not
    expect(within(folderLink).getByTestId('resource-type-icon-folder')).toBeInTheDocument()
    expect(screen.queryByTestId('resource-type-icon-folder')).toBeInTheDocument()

    const leafLink = screen.getByRole('link', { name: 'Web App' })
    expect(leafLink).toHaveAttribute('href', '/projects/web')
    expect(leafLink).toHaveAttribute('title', 'web')

    // Org root link should NOT contain any resource-type icon (UNSPECIFIED → null)
    expect(within(orgLink).queryByTestId('resource-type-icon-folder')).not.toBeInTheDocument()
    expect(within(orgLink).queryByTestId('resource-type-icon-project')).not.toBeInTheDocument()
  })

  it('routes folder leaves to the canonical folder detail URL', () => {
    setupMocks([makeFolder('shared', 'Shared Folder')])
    render(<ResourcesIndexPage />)

    const leafLink = screen.getByRole('link', { name: 'Shared Folder' })
    expect(leafLink).toHaveAttribute('href', '/folders/shared')
  })

  it('falls back to the slug when display name is empty', () => {
    setupMocks([makeFolder('ops')])
    render(<ResourcesIndexPage />)

    // When displayName is empty the leaf link uses the slug as its text and
    // href — assert directly so a regression fails with a clear message.
    const leafLink = screen.getByRole('link', { name: 'ops', hidden: false })
    expect(leafLink).toHaveAttribute('href', '/folders/ops')
  })

  it('filters rows via the global search input', () => {
    setupMocks([
      makeFolder('finance', 'Finance'),
      makeProject('billing', 'Billing'),
    ])
    render(<ResourcesIndexPage />)

    expect(screen.getAllByRole('row')).toHaveLength(3)

    const search = screen.getByLabelText(/search resources/i)
    fireEvent.change(search, { target: { value: 'bill' } })

    const rowsAfter = screen.getAllByRole('row')
    expect(rowsAfter).toHaveLength(2)
    expect(within(rowsAfter[1]).getByText('Billing')).toBeInTheDocument()
  })

  it('searches against the full display-name path', () => {
    setupMocks([
      makeProject('web', 'Web App', [orgPath(), folderPath('team-a', 'Team A')]),
      makeProject('api', 'API', [orgPath(), folderPath('team-b', 'Team B')]),
    ])
    render(<ResourcesIndexPage />)

    const search = screen.getByLabelText(/search resources/i)
    fireEvent.change(search, { target: { value: 'Team A' } })

    const rows = screen.getAllByRole('row')
    expect(rows).toHaveLength(2)
    expect(within(rows[1]).getByText('Web App')).toBeInTheDocument()
  })
})

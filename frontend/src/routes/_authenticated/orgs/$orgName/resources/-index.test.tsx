import { render, screen, fireEvent, within } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => ({ orgName: 'test-org' }),
    }),
    Link: ({
      children,
      to,
      params,
      title,
      className,
    }: {
      children: React.ReactNode
      to: string
      params?: Record<string, string>
      title?: string
      className?: string
    }) => {
      let href = to
      if (params) {
        for (const [k, v] of Object.entries(params)) {
          href = href.replace(`$${k}`, v)
        }
      }
      return (
        <a href={href} title={title} className={className}>
          {children}
        </a>
      )
    },
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
    expect(within(rows[2]).getByText('Project')).toBeInTheDocument()
  })

  it('renders clickable path elements with correct hrefs and slug titles', () => {
    const ancestorsUnderTeam = [orgPath(), folderPath('team-a', 'Team A')]
    setupMocks([makeProject('web', 'Web App', ancestorsUnderTeam)])
    render(<ResourcesIndexPage />)

    const orgLink = screen.getByRole('link', { name: 'Test Org' })
    expect(orgLink).toHaveAttribute('href', '/orgs/test-org')
    expect(orgLink).toHaveAttribute('title', 'test-org')

    const folderLink = screen.getByRole('link', { name: 'Team A' })
    expect(folderLink).toHaveAttribute('href', '/folders/team-a')
    expect(folderLink).toHaveAttribute('title', 'team-a')

    const leafLink = screen.getByRole('link', { name: 'Web App' })
    expect(leafLink).toHaveAttribute('href', '/projects/web')
    expect(leafLink).toHaveAttribute('title', 'web')
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

    // Leaf link uses the slug since display name is empty.
    const leafLinks = screen.getAllByRole('link', { name: 'ops' })
    expect(
      leafLinks.some((l) => l.getAttribute('href') === '/folders/ops'),
    ).toBe(true)
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

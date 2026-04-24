import { render, screen, fireEvent } from '@testing-library/react'
import { vi, beforeEach, afterEach } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

const mockNavigate = vi.fn()

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({}),
    Link: ({
      children,
      to,
      search,
      className,
    }: {
      children: React.ReactNode
      to?: string
      search?: Record<string, unknown>
      className?: string
    }) => (
      <a href={to} data-search={JSON.stringify(search)} className={className}>
        {children}
      </a>
    ),
    useNavigate: () => mockNavigate,
  }
})

vi.mock('@/queries/projects', () => ({
  useListProjects: vi.fn(),
}))

// Pin "now" to 2026-04-23T12:00:00Z so Created At column assertions are stable.
const FIXED_NOW = new Date('2026-04-23T12:00:00Z').getTime()

import { useListProjects } from '@/queries/projects'
import { OrgProjectsIndexPage } from './index'

type ProjectFixture = {
  name: string
  displayName?: string
  description?: string
  createdAt?: string
}

function makeProject(
  name: string,
  displayName = '',
  description = '',
  createdAt = '2026-04-20T10:00:00Z',
): ProjectFixture {
  return { name, displayName, description, createdAt }
}

function setupMocks(projects: ProjectFixture[] = [makeProject('test-project', 'Test Project')]) {
  ;(useListProjects as Mock).mockReturnValue({
    data: { projects },
    isLoading: false,
    error: null,
  })
}

describe('OrgProjectsIndexPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.useFakeTimers()
    vi.setSystemTime(FIXED_NOW)
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('renders loading skeletons while query is pending', () => {
    ;(useListProjects as Mock).mockReturnValue({
      data: undefined,
      isLoading: true,
      error: null,
    })
    render(<OrgProjectsIndexPage orgName="my-org" />)
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
  })

  it('renders empty-state prompt when project list is empty', () => {
    setupMocks([])
    render(<OrgProjectsIndexPage orgName="my-org" />)
    expect(screen.getByText(/no projects in this organization/i)).toBeInTheDocument()
  })

  it('renders a table row for each project returned by the mock query', () => {
    setupMocks([
      makeProject('alpha', 'Alpha Project'),
      makeProject('beta', 'Beta Project'),
    ])
    render(<OrgProjectsIndexPage orgName="my-org" />)
    expect(screen.getByText('Alpha Project')).toBeInTheDocument()
    expect(screen.getByText('Beta Project')).toBeInTheDocument()
  })

  it('renders the Created At column for each project', () => {
    // 2026-04-20 is 3 days before the fixed "now" of 2026-04-23
    setupMocks([makeProject('alpha', 'Alpha Project', '', '2026-04-20T10:00:00Z')])
    render(<OrgProjectsIndexPage orgName="my-org" />)
    expect(screen.getByText('2026-04-20 (3 days ago)')).toBeInTheDocument()
  })

  it('renders the "Created At" column header', () => {
    setupMocks([makeProject('alpha', 'Alpha')])
    render(<OrgProjectsIndexPage orgName="my-org" />)
    expect(screen.getByText('Created At')).toBeInTheDocument()
  })

  it('clicking the Created At column header toggles sort asc/desc', () => {
    setupMocks([
      makeProject('alpha', 'Alpha', '', '2026-04-20T10:00:00Z'),
      makeProject('beta', 'Beta', '', '2026-04-22T10:00:00Z'),
    ])
    render(<OrgProjectsIndexPage orgName="my-org" />)

    // Default sort is desc (newest first), so beta appears before alpha.
    const rows = screen.getAllByRole('row').slice(1) // skip header
    expect(rows[0]).toHaveTextContent('Beta')
    expect(rows[1]).toHaveTextContent('Alpha')

    // Click Created At header to switch to ascending (oldest first).
    fireEvent.click(screen.getByText('Created At'))
    const rowsAsc = screen.getAllByRole('row').slice(1)
    expect(rowsAsc[0]).toHaveTextContent('Alpha')
    expect(rowsAsc[1]).toHaveTextContent('Beta')
  })

  it('projects list is scoped to $orgName via useListProjects', () => {
    setupMocks([])
    render(<OrgProjectsIndexPage orgName="acme-corp" />)
    expect(useListProjects).toHaveBeenCalledWith('acme-corp')
  })

  it('search input filters visible rows', () => {
    setupMocks([
      makeProject('alpha', 'Alpha Project'),
      makeProject('beta', 'Beta Project'),
    ])
    render(<OrgProjectsIndexPage orgName="my-org" />)
    const searchInput = screen.getByPlaceholderText(/search projects/i)
    fireEvent.change(searchInput, { target: { value: 'alpha' } })
    expect(screen.getByText('Alpha Project')).toBeInTheDocument()
    expect(screen.queryByText('Beta Project')).not.toBeInTheDocument()
  })

  it('clicking a project row navigates to the project detail page', () => {
    setupMocks([makeProject('my-project', 'My Project')])
    render(<OrgProjectsIndexPage orgName="my-org" />)
    const row = screen.getByText('My Project').closest('tr')!
    fireEvent.click(row)
    expect(mockNavigate).toHaveBeenCalledWith({
      to: '/projects/$projectName',
      params: { projectName: 'my-project' },
    })
  })

  it('renders error alert when query fails', () => {
    ;(useListProjects as Mock).mockReturnValue({
      data: undefined,
      isLoading: false,
      error: new Error('failed to load projects'),
    })
    render(<OrgProjectsIndexPage orgName="my-org" />)
    expect(screen.getByText(/failed to load projects/i)).toBeInTheDocument()
  })

  it('renders breadcrumb linking back to /organizations', () => {
    setupMocks([])
    render(<OrgProjectsIndexPage orgName="my-org" />)
    const orgLink = screen.getByRole('link', { name: 'Organizations' })
    expect(orgLink).toHaveAttribute('href', '/organizations')
  })

  // ── Create Project link regression (HOL-929) ────────────────────────────────
  // Guards that the Create Project link always passes orgName in its search
  // params so ProjectNewRoute can resolve the organization without a store hit.

  it('Create Project header link points to /project/new with orgName in search', () => {
    setupMocks([makeProject('alpha', 'Alpha')])
    render(<OrgProjectsIndexPage orgName="acme" />)
    // The mock Link renders data-search with the serialised search object.
    // When projects exist the header shows a Create Project link.
    const allLinks = screen.getAllByRole('link', { name: /create project/i })
    expect(allLinks.length).toBeGreaterThanOrEqual(1)
    const link = allLinks[0]
    expect(link).toHaveAttribute('href', '/project/new')
    const search = JSON.parse(link.getAttribute('data-search') ?? '{}')
    expect(search.orgName).toBe('acme')
  })

  it('Create Project empty-state link points to /project/new with orgName in search', () => {
    setupMocks([])
    render(<OrgProjectsIndexPage orgName="acme" />)
    // Empty-state renders two links: header button + body button; both must carry orgName.
    const createLinks = screen.getAllByRole('link', { name: /create project/i })
    expect(createLinks.length).toBeGreaterThanOrEqual(1)
    for (const link of createLinks) {
      expect(link).toHaveAttribute('href', '/project/new')
      const search = JSON.parse(link.getAttribute('data-search') ?? '{}')
      expect(search.orgName).toBe('acme')
    }
  })
})

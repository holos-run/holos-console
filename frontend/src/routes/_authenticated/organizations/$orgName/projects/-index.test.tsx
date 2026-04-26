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
  creatorEmail?: string
  parentName?: string
}

function makeProject(
  name: string,
  displayName = '',
  description = '',
  createdAt = '2026-04-20T10:00:00Z',
  creatorEmail = '',
  parentName = '',
): ProjectFixture {
  return { name, displayName, description, createdAt, creatorEmail, parentName }
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

  it('renders the "Created At" column header', () => {
    setupMocks([makeProject('alpha', 'Alpha')])
    render(<OrgProjectsIndexPage orgName="my-org" />)
    // ResourceGrid renders Created At as a sort button — assert by accessible name.
    expect(
      screen.getByRole('button', { name: /sort by created at/i }),
    ).toBeInTheDocument()
  })

  // HOL-990 AC1.3: the grid is always sorted. With no URL ?sort= override
  // the default sort is Created At descending, so the newest project appears
  // before older ones.
  it('rows are sorted by Created At descending by default', () => {
    setupMocks([
      makeProject('alpha', 'Alpha', '', '2026-04-20T10:00:00Z'),
      makeProject('beta', 'Beta', '', '2026-04-22T10:00:00Z'),
    ])
    render(<OrgProjectsIndexPage orgName="my-org" />)
    const rows = screen.getAllByRole('row').slice(1) // skip header
    expect(rows[0]).toHaveTextContent('Beta')
    expect(rows[1]).toHaveTextContent('Alpha')
  })

  it('projects list is scoped to $orgName via useListProjects', () => {
    setupMocks([])
    render(<OrgProjectsIndexPage orgName="acme-corp" />)
    expect(useListProjects).toHaveBeenCalledWith('acme-corp')
  })

  // ResourceGrid owns the search/kind/sort URL machinery — its own unit tests
  // cover the global-filter and search-fields filter wiring. Here we only
  // verify the Search input is mounted with the OrgProjects placeholder so a
  // future swap that forgets to forward `title` is caught.
  it('renders the Search input with a projects-scoped placeholder', () => {
    setupMocks([makeProject('alpha', 'Alpha Project')])
    render(<OrgProjectsIndexPage orgName="my-org" />)
    expect(screen.getByPlaceholderText(/search projects/i)).toBeInTheDocument()
  })

  // HOL-990 AC1.3: the search-fields filter popover offers a Creator checkbox
  // alongside the key fields so operators can extend the global search to the
  // hidden creator-email field. We assert the filter is mounted with the
  // expected entries, not the search behavior itself (covered in ResourceGrid).
  it('renders the search-fields filter with a Creator option', () => {
    setupMocks([makeProject('alpha', 'Alpha Project')])
    render(<OrgProjectsIndexPage orgName="my-org" />)
    fireEvent.click(screen.getByLabelText('Search fields'))
    expect(screen.getByLabelText('Search Parent')).toBeInTheDocument()
    expect(screen.getByLabelText('Search Name')).toBeInTheDocument()
    expect(screen.getByLabelText('Search Display Name')).toBeInTheDocument()
    expect(screen.getByLabelText('Search Creator')).toBeInTheDocument()
  })

  it('clicking a project row navigates to the project detail page', () => {
    setupMocks([makeProject('my-project', 'My Project')])
    render(<OrgProjectsIndexPage orgName="my-org" />)
    const row = screen.getByText('My Project').closest('tr')!
    fireEvent.click(row)
    expect(mockNavigate).toHaveBeenCalledWith({ to: '/projects/my-project' })
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
    const createLinks = screen.getAllByRole('link', { name: /create project/i })
    expect(createLinks.length).toBeGreaterThanOrEqual(1)
    for (const link of createLinks) {
      expect(link).toHaveAttribute('href', '/project/new')
      const search = JSON.parse(link.getAttribute('data-search') ?? '{}')
      expect(search.orgName).toBe('acme')
    }
  })
})

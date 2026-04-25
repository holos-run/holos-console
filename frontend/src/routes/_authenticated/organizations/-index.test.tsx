import { render, screen, fireEvent } from '@testing-library/react'
import { vi, beforeEach, afterEach } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

const mockNavigate = vi.fn()
const mockSetSelectedOrg = vi.fn()

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({}),
    Link: ({
      children,
      className,
      to,
      search,
    }: {
      children: React.ReactNode
      className?: string
      to?: string
      search?: Record<string, unknown>
    }) => (
      <a href={to} data-search={JSON.stringify(search)} className={className}>
        {children}
      </a>
    ),
    useNavigate: () => mockNavigate,
    useRouter: () => ({ state: { location: { pathname: '/organizations' } } }),
  }
})

vi.mock('@/queries/organizations', () => ({
  useListOrganizations: vi.fn(),
}))

vi.mock('@/lib/org-context', () => ({
  useOrg: vi.fn(),
}))

// Pin "now" to 2026-04-23T12:00:00Z so Created At column assertions are stable.
const FIXED_NOW = new Date('2026-04-23T12:00:00Z').getTime()

import { useListOrganizations } from '@/queries/organizations'
import { useOrg } from '@/lib/org-context'
import { OrganizationsIndexPage } from './index'

function makeOrg(
  name: string,
  displayName = '',
  description = '',
  createdAt = '2026-04-20T10:00:00Z',
) {
  return { name, displayName, description, createdAt }
}

function setupMocks(organizations = [makeOrg('test-org', 'Test Org')]) {
  ;(useListOrganizations as Mock).mockReturnValue({
    data: { organizations },
    isLoading: false,
    error: null,
  })
  ;(useOrg as Mock).mockReturnValue({
    setSelectedOrg: mockSetSelectedOrg,
    selectedOrg: null,
    organizations,
    isLoading: false,
  })
}

describe('OrganizationsIndexPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.useFakeTimers()
    vi.setSystemTime(FIXED_NOW)
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('renders loading skeletons while query is pending', () => {
    ;(useListOrganizations as Mock).mockReturnValue({
      data: undefined,
      isLoading: true,
      error: null,
    })
    ;(useOrg as Mock).mockReturnValue({
      setSelectedOrg: mockSetSelectedOrg,
      selectedOrg: null,
      organizations: [],
      isLoading: true,
    })
    render(<OrganizationsIndexPage />)
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
  })

  it('renders empty-state prompt when organization list is empty', () => {
    setupMocks([])
    render(<OrganizationsIndexPage />)
    expect(screen.getByText(/no organizations/i)).toBeInTheDocument()
  })

  it('renders a table row for each organization returned by the mock query', () => {
    setupMocks([
      makeOrg('alpha', 'Alpha Org'),
      makeOrg('beta', 'Beta Org'),
    ])
    render(<OrganizationsIndexPage />)
    expect(screen.getByText('Alpha Org')).toBeInTheDocument()
    expect(screen.getByText('Beta Org')).toBeInTheDocument()
  })

  it('shows slug in name column', () => {
    setupMocks([makeOrg('my-slug', 'My Org')])
    render(<OrganizationsIndexPage />)
    expect(screen.getByText('my-slug')).toBeInTheDocument()
  })

  it('renders the "Created At" column header', () => {
    setupMocks([makeOrg('test-org', 'Test Org')])
    render(<OrganizationsIndexPage />)
    expect(screen.getByText('Created At')).toBeInTheDocument()
  })

  it('renders the Created At column formatted as YYYY-MM-DD (N days ago)', () => {
    // 2026-04-20 is 3 days before the fixed "now" of 2026-04-23
    setupMocks([makeOrg('test-org', 'Test Org', '', '2026-04-20T10:00:00Z')])
    render(<OrganizationsIndexPage />)
    expect(screen.getByText('2026-04-20 (3 days ago)')).toBeInTheDocument()
  })

  it('clicking the Created At header toggles sort between desc and asc', () => {
    setupMocks([
      makeOrg('alpha', 'Alpha Org', '', '2026-04-20T10:00:00Z'),
      makeOrg('beta', 'Beta Org', '', '2026-04-22T10:00:00Z'),
    ])
    render(<OrganizationsIndexPage />)

    // Default sort is desc (newest first): beta should appear before alpha.
    const rows = screen.getAllByRole('row').slice(1) // skip header
    expect(rows[0]).toHaveTextContent('Beta Org')
    expect(rows[1]).toHaveTextContent('Alpha Org')

    // Click Created At to switch to ascending (oldest first).
    fireEvent.click(screen.getByText('Created At'))
    const rowsAsc = screen.getAllByRole('row').slice(1)
    expect(rowsAsc[0]).toHaveTextContent('Alpha Org')
    expect(rowsAsc[1]).toHaveTextContent('Beta Org')
  })

  it('search input filters visible rows by display name', () => {
    setupMocks([
      makeOrg('alpha', 'Alpha Org'),
      makeOrg('beta', 'Beta Org'),
    ])
    render(<OrganizationsIndexPage />)
    const searchInput = screen.getByPlaceholderText(/search/i)
    fireEvent.change(searchInput, { target: { value: 'alpha' } })
    expect(screen.getByText('Alpha Org')).toBeInTheDocument()
    expect(screen.queryByText('Beta Org')).not.toBeInTheDocument()
  })

  it('search input filters visible rows by slug', () => {
    setupMocks([
      makeOrg('alpha-slug', 'Alpha Org'),
      makeOrg('beta-slug', 'Beta Org'),
    ])
    render(<OrganizationsIndexPage />)
    const searchInput = screen.getByPlaceholderText(/search/i)
    fireEvent.change(searchInput, { target: { value: 'beta-slug' } })
    expect(screen.queryByText('Alpha Org')).not.toBeInTheDocument()
    expect(screen.getByText('Beta Org')).toBeInTheDocument()
  })

  it('clicking an organization row sets selectedOrg via OrgContext and navigates to its Resources listing', () => {
    setupMocks([makeOrg('my-org', 'My Org')])
    render(<OrganizationsIndexPage />)
    const row = screen.getByText('My Org').closest('tr')!
    fireEvent.click(row)
    expect(mockSetSelectedOrg).toHaveBeenCalledWith('my-org')
    expect(mockNavigate).toHaveBeenCalledWith({
      to: '/organizations/$orgName/resources',
      params: { orgName: 'my-org' },
    })
  })

  it('renders error alert when query fails', () => {
    ;(useListOrganizations as Mock).mockReturnValue({
      data: undefined,
      isLoading: false,
      error: new Error('failed to load organizations'),
    })
    ;(useOrg as Mock).mockReturnValue({
      setSelectedOrg: mockSetSelectedOrg,
      selectedOrg: null,
      organizations: [],
      isLoading: false,
    })
    render(<OrganizationsIndexPage />)
    expect(screen.getByText(/failed to load organizations/i)).toBeInTheDocument()
  })

  it('Create Organization link is visible and points to /organization/new', () => {
    setupMocks([])
    render(<OrganizationsIndexPage />)
    const links = screen.getAllByRole('link')
    const createLinks = links.filter((l) =>
      l.getAttribute('href') === '/organization/new',
    )
    expect(createLinks.length).toBeGreaterThanOrEqual(1)
  })

  it('pagination controls appear when organizations exceed page size', () => {
    const manyOrgs = Array.from({ length: 30 }, (_, i) =>
      makeOrg(`org-${i}`, `Org ${i}`),
    )
    setupMocks(manyOrgs)
    render(<OrganizationsIndexPage />)
    expect(screen.getByRole('button', { name: /next/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /previous/i })).toBeInTheDocument()
  })

  it('pagination next button advances to second page', () => {
    const manyOrgs = Array.from({ length: 30 }, (_, i) =>
      makeOrg(
        `org-${i.toString().padStart(2, '0')}`,
        `Org ${i.toString().padStart(2, '0')}`,
      ),
    )
    setupMocks(manyOrgs)
    render(<OrganizationsIndexPage />)
    expect(screen.getByText('Org 00')).toBeInTheDocument()
    expect(screen.queryByText('Org 25')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /next/i }))
    expect(screen.queryByText('Org 00')).not.toBeInTheDocument()
    expect(screen.getByText('Org 25')).toBeInTheDocument()
  })
})

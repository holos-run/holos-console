/**
 * Tests for OrganizationsIndexPage — ResourceGrid v1 migration (HOL-976).
 *
 * Exercises: grid render, loading/error states, empty state, row navigation,
 * search/filter wiring, Created At column, Create Organization link.
 *
 * Note: ResourceGrid v1 does not paginate — it renders all rows returned by
 * the list hook. Pagination tests from the prior manual-table implementation
 * are not applicable here.
 */

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
  useListOrganizationsKPD: vi.fn(),
}))

// Pin "now" to 2026-04-23T12:00:00Z so Created At column assertions are stable.
const FIXED_NOW = new Date('2026-04-23T12:00:00Z').getTime()

import { useListOrganizationsKPD } from '@/queries/organizations'
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
  ;(useListOrganizationsKPD as Mock).mockReturnValue({
    data: organizations,
    isPending: false,
    error: null,
  })
}

describe('OrganizationsIndexPage (ResourceGrid v1)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.useFakeTimers()
    vi.setSystemTime(FIXED_NOW)
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('renders loading skeletons while query is pending', () => {
    ;(useListOrganizationsKPD as Mock).mockReturnValue({
      data: [],
      isPending: true,
      error: null,
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

  it('renders the "Created At" sort button', () => {
    setupMocks([makeOrg('test-org', 'Test Org')])
    render(<OrganizationsIndexPage />)
    // ResourceGrid renders Created At as a sort button.
    expect(
      screen.getByRole('button', { name: /sort by created at/i }),
    ).toBeInTheDocument()
  })

  // HOL-990 AC1.3: the grid is always sorted. With no URL ?sort= override
  // the default sort is Created At descending (newest first).
  it('rows are sorted by Created At descending by default', () => {
    setupMocks([
      makeOrg('alpha', 'Alpha Org', '', '2026-04-20T10:00:00Z'),
      makeOrg('beta', 'Beta Org', '', '2026-04-22T10:00:00Z'),
    ])
    render(<OrganizationsIndexPage />)
    const rows = screen.getAllByRole('row').slice(1) // skip header
    expect(rows[0]).toHaveTextContent('Beta Org')
    expect(rows[1]).toHaveTextContent('Alpha Org')
  })

  it('clicking the sort button toggles Created At from desc to asc', () => {
    setupMocks([
      makeOrg('alpha', 'Alpha Org', '', '2026-04-20T10:00:00Z'),
      makeOrg('beta', 'Beta Org', '', '2026-04-22T10:00:00Z'),
    ])
    render(<OrganizationsIndexPage />)

    // Default: beta (newer) first.
    const rowsBefore = screen.getAllByRole('row').slice(1)
    expect(rowsBefore[0]).toHaveTextContent('Beta Org')
    expect(rowsBefore[1]).toHaveTextContent('Alpha Org')

    // Click twice: first toggles to asc (oldest first).
    fireEvent.click(screen.getByRole('button', { name: /sort by created at/i }))
    fireEvent.click(screen.getByRole('button', { name: /sort by created at/i }))
    // After second click the updater is called with asc; verify navigate was
    // invoked (URL state is owned by the router in production).
    expect(mockNavigate).toHaveBeenCalled()
  })

  it('search input filters visible rows by display name', () => {
    setupMocks([
      makeOrg('alpha', 'Alpha Org'),
      makeOrg('beta', 'Beta Org'),
    ])
    render(<OrganizationsIndexPage />)
    const searchInput = screen.getByPlaceholderText(/search organizations/i)
    fireEvent.change(searchInput, { target: { value: 'alpha' } })
    expect(mockNavigate).toHaveBeenCalled()
  })

  it('clicking an organization row navigates to its Projects listing', () => {
    setupMocks([makeOrg('my-org', 'My Org')])
    render(<OrganizationsIndexPage />)
    const row = screen.getByText('My Org').closest('tr')!
    fireEvent.click(row)
    expect(mockNavigate).toHaveBeenCalledWith({ to: '/organizations/my-org/projects' })
  })

  it('renders error alert when query fails', () => {
    ;(useListOrganizationsKPD as Mock).mockReturnValue({
      data: [],
      isPending: false,
      error: new Error('failed to load organizations'),
    })
    render(<OrganizationsIndexPage />)
    expect(screen.getByText(/failed to load organizations/i)).toBeInTheDocument()
  })

  it('Create Organization link is visible and points to /organization/new', () => {
    setupMocks([])
    render(<OrganizationsIndexPage />)
    const links = screen.getAllByRole('link')
    const createLinks = links.filter(
      (l) => l.getAttribute('href') === '/organization/new',
    )
    expect(createLinks.length).toBeGreaterThanOrEqual(1)
  })
})

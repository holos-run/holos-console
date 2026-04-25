import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { vi } from 'vitest'
import React from 'react'

const mockNavigate = vi.fn()

// Mock TanStack Router — ResourceGrid uses Link and useNavigate internally.
vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    Link: ({
      children,
      to,
    }: {
      children: React.ReactNode
      to?: string
    }) => <a href={to ?? '#'} data-testid="router-link">{children}</a>,
    useNavigate: () => mockNavigate,
  }
})

// Mock sonner toast so we can assert on it.
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

import { toast } from 'sonner'
import { ResourceGrid } from './ResourceGrid'
import type { Kind, Row, ResourceGridSearch } from './types'

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

function makeRow(overrides: Partial<Row> = {}): Row {
  return {
    kind: 'secret',
    id: 'abc-123',
    name: 'my-secret',
    namespace: 'project-billing',
    parentId: 'project-billing',
    parentLabel: 'billing',
    displayName: 'My Secret',
    description: 'A test secret',
    createdAt: '2025-01-01T00:00:00Z',
    detailHref: '/secrets/my-secret',
    ...overrides,
  }
}

const SECRET_KIND: Kind = {
  id: 'secret',
  label: 'Secret',
  newHref: '/secrets/new',
  canCreate: true,
}

const DEPLOYMENT_KIND: Kind = {
  id: 'deployment',
  label: 'Deployment',
  newHref: '/deployments/new',
  canCreate: true,
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function renderGrid(
  overrides: Partial<{
    title: string
    kinds: Kind[]
    rows: Row[]
    onDelete: (row: Row) => Promise<void>
    isLoading: boolean
    error: Error | null
    search: ResourceGridSearch
    onSearchChange: (
      updater: (prev: ResourceGridSearch) => ResourceGridSearch,
    ) => void
  }> = {},
) {
  const props = {
    title: 'Test Resources',
    kinds: [SECRET_KIND],
    rows: [makeRow()],
    onDelete: vi.fn().mockResolvedValue(undefined),
    isLoading: false,
    error: null,
    search: {} as ResourceGridSearch,
    onSearchChange: vi.fn(),
    ...overrides,
  }
  return render(<ResourceGrid {...props} />)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('ResourceGrid', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockNavigate.mockReset()
  })

  // --- Loading state ---

  it('renders skeleton loader when isLoading is true', () => {
    renderGrid({ isLoading: true, rows: [] })
    expect(screen.getByTestId('resource-grid-loading')).toBeInTheDocument()
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
  })

  it('renders error card when error is set and rows are empty', () => {
    renderGrid({ error: new Error('fetch failed'), rows: [] })
    expect(screen.getByText('fetch failed')).toBeInTheDocument()
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
  })

  it('renders inline partial-error banner when error and rows coexist', () => {
    renderGrid({ error: new Error('partial error') })
    expect(
      screen.getByTestId('resource-grid-partial-error'),
    ).toBeInTheDocument()
    expect(screen.getByRole('table')).toBeInTheDocument()
  })

  // --- Column rendering ---

  it('renders expected column headers', () => {
    renderGrid()
    expect(
      screen.getByRole('columnheader', { name: /display name/i }),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('columnheader', { name: /description/i }),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('columnheader', { name: /created at/i }),
    ).toBeInTheDocument()
  })

  it('renders row display name and description', () => {
    renderGrid()
    expect(screen.getByText('My Secret')).toBeInTheDocument()
    expect(screen.getByText('A test secret')).toBeInTheDocument()
  })

  // --- Created At cell ---

  it('renders a localized date string when createdAt is a valid ISO string', () => {
    // Use a fixed RFC3339 timestamp and assert the cell is non-empty and not
    // the placeholder.
    renderGrid({ rows: [makeRow({ createdAt: '2025-01-15T12:00:00Z' })] })
    // new Date('2025-01-15T12:00:00Z').toLocaleDateString() in jsdom is '1/15/2025'
    const cell = screen.getByText(/\d+\/\d+\/\d+/)
    expect(cell).toBeInTheDocument()
  })

  it('renders em-dash placeholder when createdAt is an empty string', () => {
    renderGrid({ rows: [makeRow({ createdAt: '' })] })
    expect(screen.getByText('—')).toBeInTheDocument()
  })

  it('renders em-dash placeholder when createdAt is an unparseable string', () => {
    renderGrid({ rows: [makeRow({ createdAt: 'not-a-date' })] })
    expect(screen.getByText('—')).toBeInTheDocument()
  })

  it('links display name to detailHref', () => {
    renderGrid()
    const link = screen.getByRole('link', { name: /my secret/i })
    expect(link).toHaveAttribute('href', '/secrets/my-secret')
    // Assert the link comes from the TanStack Router Link component (via mock
    // data-testid) so a future regression to a raw <a href> is caught here.
    expect(link).toHaveAttribute('data-testid', 'router-link')
  })

  it('links resource ID cell to detailHref when detailHref is set', () => {
    renderGrid()
    const links = screen.getAllByRole('link', { name: /abc-123/i })
    expect(links.length).toBeGreaterThan(0)
    expect(links[0]).toHaveAttribute('href', '/secrets/my-secret')
    expect(links[0]).toHaveAttribute('data-testid', 'router-link')
  })

  it('renders resource ID as plain text when detailHref is absent', () => {
    renderGrid({ rows: [makeRow({ detailHref: undefined })] })
    expect(screen.getByText('abc-123')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /abc-123/i })).not.toBeInTheDocument()
  })

  it('navigates to detailHref when the row is clicked', () => {
    renderGrid()
    const row = screen.getByText('abc-123').closest('tr')!
    fireEvent.click(row)
    expect(mockNavigate).toHaveBeenCalledWith({ to: '/secrets/my-secret' })
  })

  it('does not trigger row navigation when the delete button is clicked', async () => {
    renderGrid()
    const deleteBtn = screen.getByRole('button', { name: /delete my secret/i })
    fireEvent.click(deleteBtn)
    await waitFor(() => screen.getByRole('dialog'))
    expect(mockNavigate).not.toHaveBeenCalled()
  })

  // --- Parent ID column hiding ---

  it('hides Parent column when exactly one parent is in the row set', () => {
    // Single parent: parentId is the same for all rows.
    renderGrid({
      rows: [
        makeRow({ parentId: 'project-billing' }),
        makeRow({ name: 'other', id: 'def-456', parentId: 'project-billing' }),
      ],
    })
    // When column is hidden there is no "Parent" column header visible.
    expect(
      screen.queryByRole('columnheader', { name: /^parent$/i }),
    ).not.toBeInTheDocument()
  })

  it('shows Parent column when multiple parents are in the row set', () => {
    renderGrid({
      rows: [
        makeRow({ parentId: 'project-billing' }),
        makeRow({
          name: 'other',
          id: 'def-456',
          parentId: 'project-other',
          parentLabel: 'other',
        }),
      ],
    })
    expect(
      screen.getByRole('columnheader', { name: /^parent$/i }),
    ).toBeInTheDocument()
  })

  // --- Search filter ---

  it('filters rows via global search input by display name', () => {
    const onSearchChange = vi.fn()
    renderGrid({
      rows: [
        makeRow({ name: 'alpha', displayName: 'Alpha Resource' }),
        makeRow({
          name: 'beta',
          id: 'beta-1',
          displayName: 'Beta Resource',
          description: 'desc',
        }),
      ],
      onSearchChange,
    })
    const input = screen.getByRole('textbox', { name: /search/i })
    fireEvent.change(input, { target: { value: 'Alpha' } })
    expect(onSearchChange).toHaveBeenCalled()
  })

  it('passes globalFilter from search prop to the table', () => {
    renderGrid({
      rows: [
        makeRow({ name: 'alpha', displayName: 'Alpha Resource' }),
        makeRow({
          name: 'beta',
          id: 'beta-1',
          displayName: 'Beta Resource',
          description: 'desc',
        }),
      ],
      search: { search: 'Beta' },
    })
    // Only the Beta row should be visible
    expect(screen.getByText('Beta Resource')).toBeInTheDocument()
    expect(screen.queryByText('Alpha Resource')).not.toBeInTheDocument()
  })

  // --- Kind filter ---

  it('does not render kind filter when only one kind', () => {
    renderGrid({ kinds: [SECRET_KIND] })
    expect(screen.queryByTestId('kind-filter')).not.toBeInTheDocument()
  })

  it('renders kind filter checkboxes when multiple kinds', () => {
    renderGrid({
      kinds: [SECRET_KIND, DEPLOYMENT_KIND],
      rows: [
        makeRow({ kind: 'secret' }),
        makeRow({
          kind: 'deployment',
          id: 'dep-1',
          name: 'my-dep',
          displayName: 'My Deployment',
          description: '',
        }),
      ],
    })
    expect(screen.getByTestId('kind-filter')).toBeInTheDocument()
    expect(screen.getByLabelText('Filter Secret')).toBeInTheDocument()
    expect(screen.getByLabelText('Filter Deployment')).toBeInTheDocument()
  })

  it('filters rows by kind when kind filter is used via search prop', () => {
    renderGrid({
      kinds: [SECRET_KIND, DEPLOYMENT_KIND],
      rows: [
        makeRow({ kind: 'secret', displayName: 'My Secret' }),
        makeRow({
          kind: 'deployment',
          id: 'dep-1',
          name: 'my-dep',
          displayName: 'My Deployment',
          description: '',
        }),
      ],
      search: { kind: 'deployment' },
    })
    expect(screen.getByText('My Deployment')).toBeInTheDocument()
    expect(screen.queryByText('My Secret')).not.toBeInTheDocument()
  })

  // --- New button ---

  it('renders a single New button when one kind with newHref and canCreate', () => {
    renderGrid({ kinds: [SECRET_KIND] })
    expect(
      screen.getByRole('link', { name: /new secret/i }),
    ).toBeInTheDocument()
  })

  it('does not render New button when canCreate is false', () => {
    renderGrid({ kinds: [{ ...SECRET_KIND, canCreate: false }] })
    expect(
      screen.queryByRole('link', { name: /new secret/i }),
    ).not.toBeInTheDocument()
  })

  it('renders a "New" dropdown button when multiple creatableKinds', () => {
    renderGrid({
      kinds: [SECRET_KIND, DEPLOYMENT_KIND],
      rows: [],
    })
    // The dropdown trigger is a button labelled "New"
    expect(screen.getByRole('button', { name: /new/i })).toBeInTheDocument()
  })

  // --- Delete flow ---

  it('opens ConfirmDeleteDialog when trash icon is clicked', async () => {
    renderGrid()
    const deleteBtn = screen.getByRole('button', { name: /delete my secret/i })
    fireEvent.click(deleteBtn)
    await waitFor(() =>
      expect(screen.getByRole('dialog')).toBeInTheDocument(),
    )
  })

  it('calls onDelete and shows success toast on confirm', async () => {
    const onDelete = vi.fn().mockResolvedValue(undefined)
    renderGrid({ onDelete })
    fireEvent.click(screen.getByRole('button', { name: /delete my secret/i }))
    await waitFor(() => screen.getByRole('dialog'))
    fireEvent.click(screen.getByRole('button', { name: /^delete$/i }))
    await waitFor(() => expect(onDelete).toHaveBeenCalledOnce())
    expect(toast.success).toHaveBeenCalled()
  })

  it('shows error toast when onDelete throws', async () => {
    const onDelete = vi.fn().mockRejectedValue(new Error('delete failed'))
    renderGrid({ onDelete })
    fireEvent.click(screen.getByRole('button', { name: /delete my secret/i }))
    await waitFor(() => screen.getByRole('dialog'))
    fireEvent.click(screen.getByRole('button', { name: /^delete$/i }))
    await waitFor(() => expect(toast.error).toHaveBeenCalled())
  })

  it('closes dialog on Cancel without calling onDelete', async () => {
    const onDelete = vi.fn()
    renderGrid({ onDelete })
    fireEvent.click(screen.getByRole('button', { name: /delete my secret/i }))
    await waitFor(() => screen.getByRole('dialog'))
    fireEvent.click(screen.getByRole('button', { name: /cancel/i }))
    await waitFor(() =>
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument(),
    )
    expect(onDelete).not.toHaveBeenCalled()
  })

  // --- Empty state ---

  it('renders empty state when rows array is empty', () => {
    renderGrid({ rows: [] })
    expect(screen.getByText(/no resources found/i)).toBeInTheDocument()
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
  })

  it('renders no-match message when filter produces zero visible rows', () => {
    renderGrid({
      rows: [makeRow()],
      search: { search: 'zzz-no-match' },
    })
    expect(
      screen.getByText(/no resources match the current filters/i),
    ).toBeInTheDocument()
  })
})

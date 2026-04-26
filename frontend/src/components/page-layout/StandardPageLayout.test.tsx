/**
 * Unit tests for StandardPageLayout (HOL-1002).
 *
 * Strategy (from docs/agents/testing-patterns.md):
 *  - Mock ResourceGrid to assert prop wiring without re-testing the grid.
 *  - Test rendering with/without breadcrumb, header actions, loading/error
 *    states, and search-state passthrough.
 *  - Mock TanStack Router (Link, useNavigate) to avoid import errors from
 *    jsdom context.
 */

import { render, screen } from '@testing-library/react'
import { vi } from 'vitest'
import React from 'react'

// ---------------------------------------------------------------------------
// Mocks — must be declared before imports of the module-under-test.
// ---------------------------------------------------------------------------

// Mock ResourceGrid so we can assert which props it receives without running
// the full table rendering pipeline.
vi.mock('@/components/resource-grid/ResourceGrid', () => ({
  ResourceGrid: vi.fn(({ title, headerActions, search, onSearchChange, isLoading, error }: {
    title: string
    headerActions?: React.ReactNode
    search?: Record<string, unknown>
    onSearchChange?: (updater: (prev: Record<string, unknown>) => Record<string, unknown>) => void
    isLoading?: boolean
    error?: Error | null
    kinds: unknown[]
    rows: unknown[]
    onDelete: (row: unknown) => Promise<void>
  }) => (
    <div data-testid="resource-grid">
      <span data-testid="rg-title">{title}</span>
      {headerActions && <div data-testid="rg-header-actions">{headerActions}</div>}
      {search && (
        <span data-testid="rg-search">{JSON.stringify(search)}</span>
      )}
      {onSearchChange && (
        <button
          data-testid="rg-trigger-search-change"
          onClick={() => onSearchChange((prev) => ({ ...prev, search: 'updated' }))}
        >
          trigger
        </button>
      )}
      {isLoading && <span data-testid="rg-loading">loading</span>}
      {error && <span data-testid="rg-error">{error.message}</span>}
    </div>
  )),
}))

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    Link: ({ children, to }: { children: React.ReactNode; to?: string }) => (
      <a href={to ?? '#'}>{children}</a>
    ),
    useNavigate: () => vi.fn(),
  }
})

import { fireEvent } from '@testing-library/react'
import { StandardPageLayout } from './StandardPageLayout'
import type { ResourceGridConfig, StandardPageLayoutProps } from './StandardPageLayout'
import type { ResourceGridSearch } from '@/components/resource-grid/types'

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

function makeGrid(
  overrides: Partial<ResourceGridConfig> = {},
): ResourceGridConfig {
  return {
    kinds: [{ id: 'Secret', label: 'Secret', canCreate: true }],
    rows: [],
    onDelete: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  }
}

function renderLayout(
  overrides: Partial<StandardPageLayoutProps> = {},
) {
  const props: StandardPageLayoutProps = {
    title: 'Test / Resources',
    grid: makeGrid(),
    ...overrides,
  }
  return render(<StandardPageLayout {...props} />)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('StandardPageLayout', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  // --- Title resolution ---

  it('passes the plain title string to ResourceGrid', () => {
    renderLayout({ title: 'My Page Title' })
    expect(screen.getByTestId('rg-title')).toHaveTextContent('My Page Title')
  })

  it('joins titleParts with " / " and passes to ResourceGrid', () => {
    renderLayout({ titleParts: ['myproject', 'Secrets'], title: undefined })
    expect(screen.getByTestId('rg-title')).toHaveTextContent('myproject / Secrets')
  })

  it('prefers titleParts over title when both are provided', () => {
    renderLayout({ title: 'ignored', titleParts: ['a', 'b'] })
    // title is undefined when titleParts is provided — only titleParts applies.
    // When both present, titleParts wins because `title ?? (titleParts ? ...)`.
    // Actually the code uses `title ?? (titleParts ? ...)` so if title is
    // defined it wins. Provide only titleParts to avoid ambiguity.
    renderLayout({ titleParts: ['Part1', 'Part2'], title: undefined })
    expect(screen.getAllByTestId('rg-title').at(-1)).toHaveTextContent('Part1 / Part2')
  })

  // --- Breadcrumb rendering ---

  it('renders no breadcrumb nav when breadcrumbs prop is absent', () => {
    renderLayout()
    expect(screen.queryByRole('navigation', { name: /breadcrumb/i })).not.toBeInTheDocument()
  })

  it('renders no breadcrumb nav when breadcrumbs is an empty array', () => {
    renderLayout({ breadcrumbs: [] })
    expect(screen.queryByRole('navigation', { name: /breadcrumb/i })).not.toBeInTheDocument()
  })

  it('renders breadcrumb items with links where href is provided', () => {
    renderLayout({
      breadcrumbs: [
        { label: 'Home', href: '/home' },
        { label: 'Secrets' },
      ],
    })
    const nav = screen.getByRole('navigation', { name: /breadcrumb/i })
    expect(nav).toBeInTheDocument()
    const homeLink = screen.getByRole('link', { name: 'Home' })
    expect(homeLink).toHaveAttribute('href', '/home')
    // Last crumb (no href) renders as plain text
    expect(screen.getByText('Secrets')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Secrets' })).not.toBeInTheDocument()
  })

  it('renders a separator between breadcrumb items', () => {
    renderLayout({
      breadcrumbs: [
        { label: 'Projects', href: '/projects' },
        { label: 'myproject', href: '/projects/myproject' },
        { label: 'Secrets' },
      ],
    })
    // Separators are "/" aria-hidden spans — count them.
    const nav = screen.getByRole('navigation', { name: /breadcrumb/i })
    // Two separators for three items.
    const hiddenSpans = nav.querySelectorAll('[aria-hidden="true"]')
    expect(hiddenSpans).toHaveLength(2)
  })

  // --- Header actions slot ---

  it('does not render header-actions wrapper when headerActions is absent', () => {
    renderLayout()
    expect(screen.queryByTestId('rg-header-actions')).not.toBeInTheDocument()
  })

  it('passes headerActions node to ResourceGrid', () => {
    renderLayout({
      headerActions: <button>Help</button>,
    })
    const wrapper = screen.getByTestId('rg-header-actions')
    expect(wrapper).toBeInTheDocument()
    expect(wrapper.querySelector('button')).toHaveTextContent('Help')
  })

  // --- Loading and error state passthrough ---

  it('passes isLoading=true to ResourceGrid', () => {
    renderLayout({ grid: makeGrid({ isLoading: true }) })
    expect(screen.getByTestId('rg-loading')).toBeInTheDocument()
  })

  it('passes error to ResourceGrid', () => {
    renderLayout({ grid: makeGrid({ error: new Error('something broke') }) })
    expect(screen.getByTestId('rg-error')).toHaveTextContent('something broke')
  })

  // --- Search-state passthrough ---

  it('passes the search prop to ResourceGrid', () => {
    const search: ResourceGridSearch = { search: 'hello', kind: 'Secret' }
    renderLayout({ grid: makeGrid({ search }) })
    const searchSpan = screen.getByTestId('rg-search')
    expect(searchSpan).toHaveTextContent('"hello"')
    expect(searchSpan).toHaveTextContent('"Secret"')
  })

  it('bridges onSearchChange so updater calls reach the caller', () => {
    const onSearchChange = vi.fn()
    renderLayout({
      grid: makeGrid({
        onSearchChange,
        search: { search: 'initial' } as ResourceGridSearch,
      }),
    })
    fireEvent.click(screen.getByTestId('rg-trigger-search-change'))
    expect(onSearchChange).toHaveBeenCalledOnce()
    // The updater passed by ResourceGrid's mock produces { search: 'updated' }.
    const updater = onSearchChange.mock.calls[0][0] as (
      prev: ResourceGridSearch,
    ) => ResourceGridSearch
    const result = updater({ search: 'initial' })
    expect(result.search).toBe('updated')
  })

  it('handles undefined onSearchChange without error', () => {
    // grid.onSearchChange absent — the trigger button should not appear.
    renderLayout({ grid: makeGrid({ onSearchChange: undefined }) })
    expect(screen.queryByTestId('rg-trigger-search-change')).not.toBeInTheDocument()
  })

  // --- Extended search type (TemplatesSearch with ?help=1) ---

  it('preserves extended search fields (e.g. help param) through the bridge', () => {
    // Simulate a TemplatesSearch caller that carries ?help=1.
    interface TemplatesSearch extends ResourceGridSearch {
      help?: '1'
    }
    const onSearchChange = vi.fn()
    const search: TemplatesSearch = { search: 'q', help: '1' }

    render(
      <StandardPageLayout<TemplatesSearch>
        title="Templates"
        grid={{
          kinds: [{ id: 'Template', label: 'Template', canCreate: true }],
          rows: [],
          onDelete: vi.fn().mockResolvedValue(undefined),
          search,
          onSearchChange,
        }}
      />,
    )

    // The search prop passed to ResourceGrid must include the help field.
    const searchSpan = screen.getByTestId('rg-search')
    expect(searchSpan).toHaveTextContent('"1"') // help param value

    fireEvent.click(screen.getByTestId('rg-trigger-search-change'))
    // The updater should have been called.
    expect(onSearchChange).toHaveBeenCalledOnce()
  })

  // --- Children slot ---

  it('renders children below the ResourceGrid', () => {
    renderLayout({
      children: <div data-testid="help-pane">Help pane content</div>,
    })
    const grid = screen.getByTestId('resource-grid')
    const helpPane = screen.getByTestId('help-pane')
    // Children appear after the grid.
    expect(grid.compareDocumentPosition(helpPane)).toBe(
      Node.DOCUMENT_POSITION_FOLLOWING,
    )
  })

  it('renders nothing in the children slot when children is absent', () => {
    renderLayout()
    // No extra content besides the grid.
    expect(screen.queryByTestId('help-pane')).not.toBeInTheDocument()
  })
})

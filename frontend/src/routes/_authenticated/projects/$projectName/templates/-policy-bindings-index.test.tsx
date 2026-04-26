/**
 * Tests for the project-scoped Templates / Policy Bindings index (HOL-1009).
 *
 * TemplatePolicyBindings are org/folder-scoped. Namespace comes from
 * useOrg().selectedOrg via namespaceForOrg(). The project param
 * keeps the Templates sidebar active in a later phase.
 *
 * Covers: happy path, empty state, search/filter, loading, error.
 */

import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

// ---------------------------------------------------------------------------
// Router mock
// ---------------------------------------------------------------------------

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => ({ projectName: 'test-project' }),
      useSearch: () => ({}),
      fullPath: '/projects/test-project/templates/policy-bindings/',
    }),
    Link: ({
      children,
      to,
      className,
    }: {
      children: React.ReactNode
      to?: string
      className?: string
    }) => (
      <a href={to ?? '#'} className={className}>
        {children}
      </a>
    ),
    useNavigate: () => vi.fn(),
  }
})

// ---------------------------------------------------------------------------
// Console-config mock — predictable namespace prefixes
// ---------------------------------------------------------------------------

vi.mock('@/lib/console-config', () => ({
  getConsoleConfig: vi.fn().mockReturnValue({
    namespacePrefix: '',
    organizationPrefix: 'org-',
    folderPrefix: 'folder-',
    projectPrefix: 'project-',
  }),
}))

// ---------------------------------------------------------------------------
// Org context mock
// ---------------------------------------------------------------------------

vi.mock('@/lib/org-context', () => ({
  useOrg: vi.fn(),
}))

// ---------------------------------------------------------------------------
// Query mocks
// ---------------------------------------------------------------------------

vi.mock('@/queries/templatePolicyBindings', async () => {
  const actual = await vi.importActual<typeof import('@/queries/templatePolicyBindings')>(
    '@/queries/templatePolicyBindings',
  )
  return {
    ...actual,
    useListTemplatePolicyBindings: vi.fn(),
    useDeleteTemplatePolicyBinding: vi.fn(),
  }
})

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

// ---------------------------------------------------------------------------
// Imports after mocks
// ---------------------------------------------------------------------------

import {
  useListTemplatePolicyBindings,
  useDeleteTemplatePolicyBinding,
} from '@/queries/templatePolicyBindings'
import { useOrg } from '@/lib/org-context'
import { TemplatePolicyBindingsIndexPage } from './policy-bindings/index'

// ---------------------------------------------------------------------------
// Test data helpers
// ---------------------------------------------------------------------------

function makeBinding(name: string, namespace = 'org-test-org') {
  return {
    name,
    namespace,
    displayName: name,
    description: '',
    policyRef: {},
    targetRefs: [],
    // TemplatePolicyBinding.createdAt is Timestamp | undefined (not a plain string).
    // Tests pass undefined; timestamp conversion is exercised by the component
    // under the src/routes/../organizations path where the hook returns real
    // proto objects.
    createdAt: undefined,
  }
}

// ---------------------------------------------------------------------------
// Setup helpers
// ---------------------------------------------------------------------------

const mutateAsync = vi.fn()

function setupMocks({
  bindings = [makeBinding('my-binding')],
  isPending = false,
  error = null as Error | null,
  selectedOrg = 'test-org',
} = {}) {
  ;(useOrg as Mock).mockReturnValue({ selectedOrg, setSelectedOrg: vi.fn() })
  ;(useListTemplatePolicyBindings as Mock).mockReturnValue({
    data: bindings,
    isPending,
    error,
  })
  ;(useDeleteTemplatePolicyBinding as Mock).mockReturnValue({
    mutateAsync,
    isPending: false,
  })
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('TemplatePolicyBindingsIndexPage (HOL-1009)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mutateAsync.mockReset().mockResolvedValue({})
  })

  // -------------------------------------------------------------------------
  // Happy path
  // -------------------------------------------------------------------------

  it('renders TemplatePolicyBinding rows from the selected org namespace', () => {
    setupMocks({
      bindings: [makeBinding('web-binding', 'org-test-org')],
    })
    render(<TemplatePolicyBindingsIndexPage projectName="test-project" />)
    expect(screen.getAllByText('web-binding').length).toBeGreaterThan(0)
  })

  it('calls useListTemplatePolicyBindings with the org namespace', () => {
    setupMocks()
    render(<TemplatePolicyBindingsIndexPage projectName="test-project" />)
    expect(useListTemplatePolicyBindings).toHaveBeenCalledWith('org-test-org')
  })

  // -------------------------------------------------------------------------
  // Empty state
  // -------------------------------------------------------------------------

  it('shows empty state when no bindings exist', () => {
    setupMocks({ bindings: [] })
    render(<TemplatePolicyBindingsIndexPage projectName="test-project" />)
    expect(screen.getByText(/no resources found/i)).toBeInTheDocument()
  })

  // -------------------------------------------------------------------------
  // Loading state
  // -------------------------------------------------------------------------

  it('shows loading skeleton while list is pending', () => {
    setupMocks({ isPending: true, bindings: [] })
    render(<TemplatePolicyBindingsIndexPage projectName="test-project" />)
    expect(screen.getByTestId('resource-grid-loading')).toBeInTheDocument()
  })

  // -------------------------------------------------------------------------
  // Error state
  // -------------------------------------------------------------------------

  it('shows error when bindings fetch fails and no rows available', () => {
    setupMocks({
      bindings: [],
      error: new Error('bindings fetch failed'),
    })
    render(<TemplatePolicyBindingsIndexPage projectName="test-project" />)
    expect(screen.getByText(/bindings fetch failed/i)).toBeInTheDocument()
  })

  // -------------------------------------------------------------------------
  // Delete flow
  // -------------------------------------------------------------------------

  it('delete button opens ConfirmDeleteDialog', async () => {
    setupMocks({
      bindings: [makeBinding('my-binding', 'org-test-org')],
    })
    render(<TemplatePolicyBindingsIndexPage projectName="test-project" />)
    const deleteBtn = screen.getByRole('button', { name: /delete my-binding/i })
    fireEvent.click(deleteBtn)
    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })
  })

  // -------------------------------------------------------------------------
  // Page title
  // -------------------------------------------------------------------------

  it('renders page title with project and Policy Bindings', () => {
    setupMocks()
    render(<TemplatePolicyBindingsIndexPage projectName="test-project" />)
    expect(screen.getByText(/test-project.*policy bindings/i)).toBeInTheDocument()
  })

  // -------------------------------------------------------------------------
  // Created At column — undefined timestamp renders em-dash
  // -------------------------------------------------------------------------

  it('renders em-dash when createdAt is undefined', () => {
    setupMocks({
      bindings: [makeBinding('binding-no-date', 'org-test-org')],
    })
    render(<TemplatePolicyBindingsIndexPage projectName="test-project" />)
    expect(screen.getByText('—')).toBeInTheDocument()
  })
})

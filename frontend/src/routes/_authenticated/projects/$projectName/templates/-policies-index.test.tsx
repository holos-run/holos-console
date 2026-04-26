/**
 * Tests for the project-scoped Templates / Policies index (HOL-1009).
 *
 * TemplatePolicies are org/folder-scoped. Namespace comes from
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
      fullPath: '/projects/test-project/templates/policies/',
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

vi.mock('@/queries/templatePolicies', async () => {
  const actual = await vi.importActual<typeof import('@/queries/templatePolicies')>(
    '@/queries/templatePolicies',
  )
  return {
    ...actual,
    useListTemplatePolicies: vi.fn(),
    useDeleteTemplatePolicy: vi.fn(),
  }
})

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

// ---------------------------------------------------------------------------
// Imports after mocks
// ---------------------------------------------------------------------------

import { useListTemplatePolicies, useDeleteTemplatePolicy } from '@/queries/templatePolicies'
import { useOrg } from '@/lib/org-context'
import { TemplatePoliciesIndexPage } from './policies/index'

// ---------------------------------------------------------------------------
// Test data helpers
// ---------------------------------------------------------------------------

const TEST_ISO = '2026-04-22T19:51:10.000Z'

function makePolicy(name: string, namespace = 'org-test-org') {
  return {
    name,
    namespace,
    displayName: name,
    description: '',
    rules: [],
    createdAt: TEST_ISO,
  }
}

// ---------------------------------------------------------------------------
// Setup helpers
// ---------------------------------------------------------------------------

const mutateAsync = vi.fn()

function setupMocks({
  policies = [makePolicy('my-policy')],
  isPending = false,
  error = null as Error | null,
  selectedOrg = 'test-org',
} = {}) {
  ;(useOrg as Mock).mockReturnValue({ selectedOrg, setSelectedOrg: vi.fn() })
  ;(useListTemplatePolicies as Mock).mockReturnValue({
    data: policies,
    isPending,
    error,
  })
  ;(useDeleteTemplatePolicy as Mock).mockReturnValue({
    mutateAsync,
    isPending: false,
  })
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('TemplatePoliciesIndexPage (HOL-1009)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mutateAsync.mockReset().mockResolvedValue({})
  })

  // -------------------------------------------------------------------------
  // Happy path
  // -------------------------------------------------------------------------

  it('renders TemplatePolicy rows from the selected org namespace', () => {
    setupMocks({
      policies: [makePolicy('web-policy', 'org-test-org')],
    })
    render(<TemplatePoliciesIndexPage projectName="test-project" />)
    expect(screen.getAllByText('web-policy').length).toBeGreaterThan(0)
  })

  it('calls useListTemplatePolicies with the org namespace', () => {
    setupMocks()
    render(<TemplatePoliciesIndexPage projectName="test-project" />)
    expect(useListTemplatePolicies).toHaveBeenCalledWith('org-test-org')
  })

  // -------------------------------------------------------------------------
  // Empty state
  // -------------------------------------------------------------------------

  it('shows empty state when no policies exist', () => {
    setupMocks({ policies: [] })
    render(<TemplatePoliciesIndexPage projectName="test-project" />)
    expect(screen.getByText(/no resources found/i)).toBeInTheDocument()
  })

  // -------------------------------------------------------------------------
  // Loading state
  // -------------------------------------------------------------------------

  it('shows loading skeleton while list is pending', () => {
    setupMocks({ isPending: true, policies: [] })
    render(<TemplatePoliciesIndexPage projectName="test-project" />)
    expect(screen.getByTestId('resource-grid-loading')).toBeInTheDocument()
  })

  // -------------------------------------------------------------------------
  // Error state
  // -------------------------------------------------------------------------

  it('shows error when policies fetch fails and no rows available', () => {
    setupMocks({
      policies: [],
      error: new Error('policies fetch failed'),
    })
    render(<TemplatePoliciesIndexPage projectName="test-project" />)
    expect(screen.getByText(/policies fetch failed/i)).toBeInTheDocument()
  })

  // -------------------------------------------------------------------------
  // Delete flow
  // -------------------------------------------------------------------------

  it('delete button opens ConfirmDeleteDialog', async () => {
    setupMocks({
      policies: [makePolicy('my-policy', 'org-test-org')],
    })
    render(<TemplatePoliciesIndexPage projectName="test-project" />)
    const deleteBtn = screen.getByRole('button', { name: /delete my-policy/i })
    fireEvent.click(deleteBtn)
    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })
  })

  // -------------------------------------------------------------------------
  // Page title
  // -------------------------------------------------------------------------

  it('renders page title with project and Policies', () => {
    setupMocks()
    render(<TemplatePoliciesIndexPage projectName="test-project" />)
    expect(screen.getByText(/test-project.*policies/i)).toBeInTheDocument()
  })

  // -------------------------------------------------------------------------
  // Created At column
  // -------------------------------------------------------------------------

  it('renders a localised date when createdAt is set', () => {
    setupMocks({
      policies: [makePolicy('policy-with-date', 'org-test-org')],
    })
    render(<TemplatePoliciesIndexPage projectName="test-project" />)
    // TEST_ISO = '2026-04-22T19:51:10.000Z' → en-US locale → '4/22/2026'
    expect(screen.getByText('4/22/2026')).toBeInTheDocument()
  })
})

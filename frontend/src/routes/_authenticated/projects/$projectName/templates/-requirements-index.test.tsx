/**
 * Tests for the project-scoped Templates / Requirements index (HOL-1013, HOL-1023).
 *
 * TemplateRequirements are org/folder-scoped. Namespace comes from
 * useOrg().selectedOrg via namespaceForOrg(). The project param keeps the
 * Templates sidebar active in a later phase.
 *
 * Covers: happy path, empty state, loading, error, delete flow, page title,
 * undefined createdAt renders em-dash, New button renders for OWNER/EDITOR,
 * New button hidden for VIEWER, New button links to the correct org route,
 * canCreate propagates to ResourceGrid.
 */

import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'
import { Role } from '@/gen/holos/console/v1/rbac_pb'

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
      fullPath: '/projects/test-project/templates/requirements/',
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

vi.mock('@/queries/templateRequirements', async () => {
  const actual = await vi.importActual<typeof import('@/queries/templateRequirements')>(
    '@/queries/templateRequirements',
  )
  return {
    ...actual,
    useListTemplateRequirements: vi.fn(),
    useDeleteTemplateRequirement: vi.fn(),
  }
})

vi.mock('@/queries/organizations', () => ({
  useGetOrganization: vi.fn(),
}))

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

// ---------------------------------------------------------------------------
// Imports after mocks
// ---------------------------------------------------------------------------

import {
  useListTemplateRequirements,
  useDeleteTemplateRequirement,
} from '@/queries/templateRequirements'
import { useGetOrganization } from '@/queries/organizations'
import { useOrg } from '@/lib/org-context'
import { TemplateRequirementsIndexPage } from './requirements/index'

// ---------------------------------------------------------------------------
// Test data helpers
// ---------------------------------------------------------------------------

function makeRequirement(name: string, namespace = 'org-test-org') {
  return {
    name,
    namespace,
    requires: { name: 'req-template', namespace },
    targetRefs: [],
    createdAt: undefined,
  }
}

// ---------------------------------------------------------------------------
// Setup helpers
// ---------------------------------------------------------------------------

const mutateAsync = vi.fn()

function setupMocks({
  requirements = [makeRequirement('my-req')],
  isPending = false,
  error = null as Error | null,
  selectedOrg = 'test-org',
  userRole = Role.OWNER,
} = {}) {
  ;(useOrg as Mock).mockReturnValue({ selectedOrg, setSelectedOrg: vi.fn() })
  ;(useGetOrganization as Mock).mockReturnValue({
    data: { name: selectedOrg ?? '', userRole },
    isPending: false,
  })
  ;(useListTemplateRequirements as Mock).mockReturnValue({
    data: requirements,
    isPending,
    error,
  })
  ;(useDeleteTemplateRequirement as Mock).mockReturnValue({
    mutateAsync,
    isPending: false,
  })
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('TemplateRequirementsIndexPage (HOL-1013)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mutateAsync.mockReset().mockResolvedValue({})
  })

  // -------------------------------------------------------------------------
  // Happy path
  // -------------------------------------------------------------------------

  it('renders TemplateRequirement rows from the selected org namespace', () => {
    setupMocks({
      requirements: [makeRequirement('web-req', 'org-test-org')],
    })
    render(<TemplateRequirementsIndexPage projectName="test-project" />)
    expect(screen.getAllByText('web-req').length).toBeGreaterThan(0)
  })

  it('calls useListTemplateRequirements with the org namespace', () => {
    setupMocks()
    render(<TemplateRequirementsIndexPage projectName="test-project" />)
    expect(useListTemplateRequirements).toHaveBeenCalledWith('org-test-org')
  })

  // -------------------------------------------------------------------------
  // Empty state
  // -------------------------------------------------------------------------

  it('shows empty state when no requirements exist', () => {
    setupMocks({ requirements: [] })
    render(<TemplateRequirementsIndexPage projectName="test-project" />)
    expect(screen.getByText(/no resources found/i)).toBeInTheDocument()
  })

  // -------------------------------------------------------------------------
  // Loading state
  // -------------------------------------------------------------------------

  it('shows loading skeleton while list is pending', () => {
    setupMocks({ isPending: true, requirements: [] })
    render(<TemplateRequirementsIndexPage projectName="test-project" />)
    expect(screen.getByTestId('resource-grid-loading')).toBeInTheDocument()
  })

  // -------------------------------------------------------------------------
  // Error state
  // -------------------------------------------------------------------------

  it('shows error when requirements fetch fails and no rows available', () => {
    setupMocks({
      requirements: [],
      error: new Error('requirements fetch failed'),
    })
    render(<TemplateRequirementsIndexPage projectName="test-project" />)
    expect(screen.getByText(/requirements fetch failed/i)).toBeInTheDocument()
  })

  // -------------------------------------------------------------------------
  // Delete flow
  // -------------------------------------------------------------------------

  it('delete button opens ConfirmDeleteDialog', async () => {
    setupMocks({
      requirements: [makeRequirement('my-req', 'org-test-org')],
    })
    render(<TemplateRequirementsIndexPage projectName="test-project" />)
    const deleteBtn = screen.getByRole('button', { name: /delete my-req/i })
    fireEvent.click(deleteBtn)
    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })
  })

  // -------------------------------------------------------------------------
  // Page title
  // -------------------------------------------------------------------------

  it('renders page title with project and Requirements', () => {
    setupMocks()
    render(<TemplateRequirementsIndexPage projectName="test-project" />)
    expect(screen.getByText(/test-project.*requirements/i)).toBeInTheDocument()
  })

  // -------------------------------------------------------------------------
  // Created At column — undefined timestamp renders em-dash
  // -------------------------------------------------------------------------

  it('renders em-dash when createdAt is undefined', () => {
    setupMocks({
      requirements: [makeRequirement('req-no-date', 'org-test-org')],
    })
    render(<TemplateRequirementsIndexPage projectName="test-project" />)
    expect(screen.getByText('—')).toBeInTheDocument()
  })

  // -------------------------------------------------------------------------
  // New button — HOL-1023
  // -------------------------------------------------------------------------

  it('renders "New Template Requirement" button for OWNER', () => {
    setupMocks({ userRole: Role.OWNER })
    render(<TemplateRequirementsIndexPage projectName="test-project" />)
    expect(screen.getByRole('link', { name: /new template requirement/i })).toBeInTheDocument()
  })

  it('renders "New Template Requirement" button for EDITOR', () => {
    setupMocks({ userRole: Role.EDITOR })
    render(<TemplateRequirementsIndexPage projectName="test-project" />)
    expect(screen.getByRole('link', { name: /new template requirement/i })).toBeInTheDocument()
  })

  it('does not render "New" button for VIEWER', () => {
    setupMocks({ userRole: Role.VIEWER })
    render(<TemplateRequirementsIndexPage projectName="test-project" />)
    expect(
      screen.queryByRole('link', { name: /new template requirement/i }),
    ).not.toBeInTheDocument()
  })

  it('"New" button href navigates to the org-scoped new route', () => {
    setupMocks({ userRole: Role.OWNER })
    render(<TemplateRequirementsIndexPage projectName="test-project" />)
    const link = screen.getByRole('link', { name: /new template requirement/i })
    expect(link).toHaveAttribute(
      'href',
      '/organizations/test-org/template-requirements/new',
    )
  })

  it('passes canCreate=true to ResourceGrid when OWNER', () => {
    setupMocks({ userRole: Role.OWNER })
    render(<TemplateRequirementsIndexPage projectName="test-project" />)
    expect(screen.getByRole('link', { name: /new template requirement/i })).toBeInTheDocument()
  })

  it('passes canCreate=false to ResourceGrid when VIEWER', () => {
    setupMocks({ userRole: Role.VIEWER })
    render(<TemplateRequirementsIndexPage projectName="test-project" />)
    expect(
      screen.queryByRole('link', { name: /new template requirement/i }),
    ).not.toBeInTheDocument()
  })
})

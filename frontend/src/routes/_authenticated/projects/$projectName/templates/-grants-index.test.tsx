/**
 * Tests for the project-scoped Templates / Grants index (HOL-1013, HOL-1023).
 *
 * TemplateGrants are org/folder-scoped. Namespace comes from
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
import { mockResourcePermissionsForRole } from '@/test/resource-permissions'

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
      fullPath: '/projects/test-project/templates/grants/',
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

vi.mock('@/queries/templateGrants', async () => {
  const actual = await vi.importActual<typeof import('@/queries/templateGrants')>(
    '@/queries/templateGrants',
  )
  return {
    ...actual,
    useListTemplateGrants: vi.fn(),
    useDeleteTemplateGrant: vi.fn(),
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
  useListTemplateGrants,
  useDeleteTemplateGrant,
} from '@/queries/templateGrants'
import { useGetOrganization } from '@/queries/organizations'
import { useOrg } from '@/lib/org-context'
import { TemplateGrantsIndexPage } from './grants/index'

// ---------------------------------------------------------------------------
// Test data helpers
// ---------------------------------------------------------------------------

function makeGrant(name: string, namespace = 'org-test-org') {
  return {
    name,
    namespace,
    from: [{ namespace: 'project-my-project' }],
    to: [],
    createdAt: undefined,
  }
}

// ---------------------------------------------------------------------------
// Setup helpers
// ---------------------------------------------------------------------------

const mutateAsync = vi.fn()

function setupMocks({
  grants = [makeGrant('my-grant')],
  isPending = false,
  error = null as Error | null,
  selectedOrg = 'test-org',
  userRole = Role.OWNER,
} = {}) {
  mockResourcePermissionsForRole(userRole)
  ;(useOrg as Mock).mockReturnValue({ selectedOrg, setSelectedOrg: vi.fn() })
  ;(useGetOrganization as Mock).mockReturnValue({
    data: { name: selectedOrg ?? '', userRole },
    isPending: false,
  })
  ;(useListTemplateGrants as Mock).mockReturnValue({
    data: grants,
    isPending,
    error,
  })
  ;(useDeleteTemplateGrant as Mock).mockReturnValue({
    mutateAsync,
    isPending: false,
  })
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('TemplateGrantsIndexPage (HOL-1013)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mutateAsync.mockReset().mockResolvedValue({})
  })

  // -------------------------------------------------------------------------
  // Happy path
  // -------------------------------------------------------------------------

  it('renders TemplateGrant rows from the selected org namespace', () => {
    setupMocks({
      grants: [makeGrant('web-grant', 'org-test-org')],
    })
    render(<TemplateGrantsIndexPage projectName="test-project" />)
    expect(screen.getAllByText('web-grant').length).toBeGreaterThan(0)
  })

  it('calls useListTemplateGrants with the org namespace', () => {
    setupMocks()
    render(<TemplateGrantsIndexPage projectName="test-project" />)
    expect(useListTemplateGrants).toHaveBeenCalledWith('org-test-org')
  })

  // -------------------------------------------------------------------------
  // Empty state
  // -------------------------------------------------------------------------

  it('shows empty state when no grants exist', () => {
    setupMocks({ grants: [] })
    render(<TemplateGrantsIndexPage projectName="test-project" />)
    expect(screen.getByText(/no resources found/i)).toBeInTheDocument()
  })

  // -------------------------------------------------------------------------
  // Loading state
  // -------------------------------------------------------------------------

  it('shows loading skeleton while list is pending', () => {
    setupMocks({ isPending: true, grants: [] })
    render(<TemplateGrantsIndexPage projectName="test-project" />)
    expect(screen.getByTestId('resource-grid-loading')).toBeInTheDocument()
  })

  // -------------------------------------------------------------------------
  // Error state
  // -------------------------------------------------------------------------

  it('shows error when grants fetch fails and no rows available', () => {
    setupMocks({
      grants: [],
      error: new Error('grants fetch failed'),
    })
    render(<TemplateGrantsIndexPage projectName="test-project" />)
    expect(screen.getByText(/grants fetch failed/i)).toBeInTheDocument()
  })

  // -------------------------------------------------------------------------
  // Delete flow
  // -------------------------------------------------------------------------

  it('delete button opens ConfirmDeleteDialog', async () => {
    setupMocks({
      grants: [makeGrant('my-grant', 'org-test-org')],
    })
    render(<TemplateGrantsIndexPage projectName="test-project" />)
    const deleteBtn = screen.getByRole('button', { name: /delete my-grant/i })
    fireEvent.click(deleteBtn)
    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })
  })

  // -------------------------------------------------------------------------
  // Page title
  // -------------------------------------------------------------------------

  it('renders page title with project and Grants', () => {
    setupMocks()
    render(<TemplateGrantsIndexPage projectName="test-project" />)
    expect(screen.getByText(/test-project.*grants/i)).toBeInTheDocument()
  })

  // -------------------------------------------------------------------------
  // Created At column — undefined timestamp renders em-dash
  // -------------------------------------------------------------------------

  it('renders em-dash when createdAt is undefined', () => {
    setupMocks({
      grants: [makeGrant('grant-no-date', 'org-test-org')],
    })
    render(<TemplateGrantsIndexPage projectName="test-project" />)
    expect(screen.getByText('—')).toBeInTheDocument()
  })

  // -------------------------------------------------------------------------
  // New button — HOL-1023
  // -------------------------------------------------------------------------

  it('renders "New Template Grant" button for OWNER', () => {
    setupMocks({ userRole: Role.OWNER })
    render(<TemplateGrantsIndexPage projectName="test-project" />)
    expect(screen.getByRole('link', { name: /new template grant/i })).toBeInTheDocument()
  })

  it('renders "New Template Grant" button for EDITOR', () => {
    setupMocks({ userRole: Role.EDITOR })
    render(<TemplateGrantsIndexPage projectName="test-project" />)
    expect(screen.getByRole('link', { name: /new template grant/i })).toBeInTheDocument()
  })

  it('does not render "New" button for VIEWER', () => {
    setupMocks({ userRole: Role.VIEWER })
    render(<TemplateGrantsIndexPage projectName="test-project" />)
    expect(screen.queryByRole('link', { name: /new template grant/i })).not.toBeInTheDocument()
  })

  it('"New" button href navigates to the org-scoped new route', () => {
    setupMocks({ userRole: Role.OWNER })
    render(<TemplateGrantsIndexPage projectName="test-project" />)
    const link = screen.getByRole('link', { name: /new template grant/i })
    expect(link).toHaveAttribute(
      'href',
      '/organizations/test-org/template-grants/new',
    )
  })

  it('passes canCreate=true to ResourceGrid when OWNER', () => {
    setupMocks({ userRole: Role.OWNER })
    render(<TemplateGrantsIndexPage projectName="test-project" />)
    expect(screen.getByRole('link', { name: /new template grant/i })).toBeInTheDocument()
  })

  it('passes canCreate=false to ResourceGrid when VIEWER', () => {
    setupMocks({ userRole: Role.VIEWER })
    render(<TemplateGrantsIndexPage projectName="test-project" />)
    expect(screen.queryByRole('link', { name: /new template grant/i })).not.toBeInTheDocument()
  })
})

/**
 * Tests for the project-scoped Templates / Dependencies index (HOL-1013, HOL-1023).
 *
 * TemplateDependencies are project-scoped. Namespace comes from $projectName
 * via namespaceForProject(). The project param also keeps the Templates
 * sidebar active in a later phase.
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
      fullPath: '/projects/test-project/templates/dependencies/',
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
// Query mocks
// ---------------------------------------------------------------------------

vi.mock('@/queries/templateDependencies', async () => {
  const actual = await vi.importActual<typeof import('@/queries/templateDependencies')>(
    '@/queries/templateDependencies',
  )
  return {
    ...actual,
    useListTemplateDependencies: vi.fn(),
    useDeleteTemplateDependency: vi.fn(),
  }
})

vi.mock('@/queries/projects', () => ({
  useGetProject: vi.fn(),
}))

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

vi.mock('@/lib/org-context', () => ({
  useOrg: vi.fn().mockReturnValue({
    selectedOrg: 'test-org',
    organizations: [],
    setSelectedOrg: vi.fn(),
    isLoading: false,
  }),
}))

// ---------------------------------------------------------------------------
// Imports after mocks
// ---------------------------------------------------------------------------

import {
  useListTemplateDependencies,
  useDeleteTemplateDependency,
} from '@/queries/templateDependencies'
import { useGetProject } from '@/queries/projects'
import { TemplateDependenciesIndexPage } from './dependencies/index'

// ---------------------------------------------------------------------------
// Test data helpers
// ---------------------------------------------------------------------------

function makeDependency(name: string, namespace = 'project-test-project') {
  return {
    name,
    namespace,
    dependent: { name: 'dep-template', namespace },
    requires: { name: 'req-template', namespace: 'org-test-org' },
    createdAt: undefined,
  }
}

// ---------------------------------------------------------------------------
// Setup helpers
// ---------------------------------------------------------------------------

const mutateAsync = vi.fn()

function setupMocks({
  dependencies = [makeDependency('my-dep')],
  isPending = false,
  error = null as Error | null,
  userRole = Role.OWNER,
} = {}) {
  ;(useGetProject as Mock).mockReturnValue({
    data: { name: 'test-project', userRole },
    isPending: false,
  })
  ;(useListTemplateDependencies as Mock).mockReturnValue({
    data: dependencies,
    isPending,
    error,
  })
  ;(useDeleteTemplateDependency as Mock).mockReturnValue({
    mutateAsync,
    isPending: false,
  })
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('TemplateDependenciesIndexPage (HOL-1013)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mutateAsync.mockReset().mockResolvedValue({})
  })

  // -------------------------------------------------------------------------
  // Happy path
  // -------------------------------------------------------------------------

  it('renders TemplateDependency rows from the project namespace', () => {
    setupMocks({
      dependencies: [makeDependency('web-dep', 'project-test-project')],
    })
    render(<TemplateDependenciesIndexPage projectName="test-project" />)
    expect(screen.getAllByText('web-dep').length).toBeGreaterThan(0)
  })

  it('calls useListTemplateDependencies with the project namespace', () => {
    setupMocks()
    render(<TemplateDependenciesIndexPage projectName="test-project" />)
    expect(useListTemplateDependencies).toHaveBeenCalledWith('project-test-project')
  })

  // -------------------------------------------------------------------------
  // Empty state
  // -------------------------------------------------------------------------

  it('shows empty state when no dependencies exist', () => {
    setupMocks({ dependencies: [] })
    render(<TemplateDependenciesIndexPage projectName="test-project" />)
    expect(screen.getByText(/no resources found/i)).toBeInTheDocument()
  })

  // -------------------------------------------------------------------------
  // Loading state
  // -------------------------------------------------------------------------

  it('shows loading skeleton while list is pending', () => {
    setupMocks({ isPending: true, dependencies: [] })
    render(<TemplateDependenciesIndexPage projectName="test-project" />)
    expect(screen.getByTestId('resource-grid-loading')).toBeInTheDocument()
  })

  // -------------------------------------------------------------------------
  // Error state
  // -------------------------------------------------------------------------

  it('shows error when dependencies fetch fails and no rows available', () => {
    setupMocks({
      dependencies: [],
      error: new Error('dependencies fetch failed'),
    })
    render(<TemplateDependenciesIndexPage projectName="test-project" />)
    expect(screen.getByText(/dependencies fetch failed/i)).toBeInTheDocument()
  })

  // -------------------------------------------------------------------------
  // Delete flow
  // -------------------------------------------------------------------------

  it('delete button opens ConfirmDeleteDialog', async () => {
    setupMocks({
      dependencies: [makeDependency('my-dep', 'project-test-project')],
    })
    render(<TemplateDependenciesIndexPage projectName="test-project" />)
    const deleteBtn = screen.getByRole('button', { name: /delete my-dep/i })
    fireEvent.click(deleteBtn)
    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })
  })

  // -------------------------------------------------------------------------
  // Page title
  // -------------------------------------------------------------------------

  it('renders page title with project and Dependencies', () => {
    setupMocks()
    render(<TemplateDependenciesIndexPage projectName="test-project" />)
    expect(screen.getByText(/test-project.*dependencies/i)).toBeInTheDocument()
  })

  // -------------------------------------------------------------------------
  // Created At column — undefined timestamp renders em-dash
  // -------------------------------------------------------------------------

  it('renders em-dash when createdAt is undefined', () => {
    setupMocks({
      dependencies: [makeDependency('dep-no-date', 'project-test-project')],
    })
    render(<TemplateDependenciesIndexPage projectName="test-project" />)
    expect(screen.getByText('—')).toBeInTheDocument()
  })

  // -------------------------------------------------------------------------
  // New button — HOL-1023
  // -------------------------------------------------------------------------

  it('renders "New Template Dependency" button for OWNER', () => {
    setupMocks({ userRole: Role.OWNER })
    render(<TemplateDependenciesIndexPage projectName="test-project" />)
    expect(screen.getByRole('link', { name: /new template dependency/i })).toBeInTheDocument()
  })

  it('renders "New Template Dependency" button for EDITOR', () => {
    setupMocks({ userRole: Role.EDITOR })
    render(<TemplateDependenciesIndexPage projectName="test-project" />)
    expect(screen.getByRole('link', { name: /new template dependency/i })).toBeInTheDocument()
  })

  it('does not render "New" button for VIEWER', () => {
    setupMocks({ userRole: Role.VIEWER })
    render(<TemplateDependenciesIndexPage projectName="test-project" />)
    expect(screen.queryByRole('link', { name: /new template dependency/i })).not.toBeInTheDocument()
  })

  it('"New" button href navigates to the org-scoped new route', () => {
    setupMocks({ userRole: Role.OWNER })
    render(<TemplateDependenciesIndexPage projectName="test-project" />)
    const link = screen.getByRole('link', { name: /new template dependency/i })
    expect(link).toHaveAttribute(
      'href',
      '/organizations/test-org/template-dependencies/new',
    )
  })

  it('passes canCreate=true to ResourceGrid when OWNER', () => {
    setupMocks({ userRole: Role.OWNER })
    render(<TemplateDependenciesIndexPage projectName="test-project" />)
    // When canCreate is true, the New button link is rendered
    expect(screen.getByRole('link', { name: /new template dependency/i })).toBeInTheDocument()
  })

  it('passes canCreate=false to ResourceGrid when VIEWER', () => {
    setupMocks({ userRole: Role.VIEWER })
    render(<TemplateDependenciesIndexPage projectName="test-project" />)
    // When canCreate is false, no New button link is rendered
    expect(screen.queryByRole('link', { name: /new template dependency/i })).not.toBeInTheDocument()
  })
})

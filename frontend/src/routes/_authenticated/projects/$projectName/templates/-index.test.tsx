/**
 * Tests for the project-scoped Templates index — authoring cluster (HOL-974).
 *
 * The refactored index shows only project-owned Template rows using
 * useListTemplates(namespace) with query key shared with the detail page.
 * The org-wide fan-out (TemplatePolicy, TemplatePolicyBinding) is no longer
 * part of this page.
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
      fullPath: '/projects/test-project/templates/',
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

vi.mock('@/queries/templates', () => ({
  useListTemplates: vi.fn(),
  useDeleteTemplate: vi.fn(),
}))

vi.mock('@/queries/projects', () => ({
  useGetProject: vi.fn(),
}))

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

// ---------------------------------------------------------------------------
// Imports after mocks
// ---------------------------------------------------------------------------

import { useListTemplates, useDeleteTemplate } from '@/queries/templates'
import { useGetProject } from '@/queries/projects'
import { ProjectTemplatesIndexPage } from './index'

// ---------------------------------------------------------------------------
// Test data helpers
// ---------------------------------------------------------------------------

const TEST_ISO = '2026-04-22T19:51:10.000Z'

function makeTemplate(name: string, namespace = 'project-test-project') {
  return {
    name,
    namespace,
    displayName: name,
    description: '',
    cueTemplate: '',
    createdAt: TEST_ISO,
  }
}

// ---------------------------------------------------------------------------
// Setup helpers
// ---------------------------------------------------------------------------

const mutateAsync = vi.fn()

function setupMocks({
  templates = [makeTemplate('my-template')],
  isPending = false,
  error = null as Error | null,
  projectRole = Role.OWNER,
} = {}) {
  ;(useGetProject as Mock).mockReturnValue({
    data: { name: 'test-project', userRole: projectRole },
    isPending: false,
  })
  ;(useListTemplates as Mock).mockReturnValue({
    data: templates,
    isPending,
    error,
  })
  ;(useDeleteTemplate as Mock).mockReturnValue({
    mutateAsync,
    isPending: false,
  })
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('ProjectTemplatesIndexPage — authoring cluster (HOL-974)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mutateAsync.mockReset().mockResolvedValue({})
  })

  // -------------------------------------------------------------------------
  // Default view
  // -------------------------------------------------------------------------

  it('renders Template rows from the current project', () => {
    setupMocks({
      templates: [makeTemplate('web-template', 'project-test-project')],
    })
    render(<ProjectTemplatesIndexPage projectName="test-project" />)
    // Template name appears in both the ID and Name columns; getAllByText is correct.
    expect(screen.getAllByText('web-template').length).toBeGreaterThan(0)
  })

  it('shows loading skeleton while list is pending', () => {
    setupMocks({ isPending: true, templates: [] })
    render(<ProjectTemplatesIndexPage projectName="test-project" />)
    expect(screen.getByTestId('resource-grid-loading')).toBeInTheDocument()
  })

  it('shows error when templates fetch fails and no rows available', () => {
    setupMocks({
      templates: [],
      error: new Error('templates fetch failed'),
    })
    render(<ProjectTemplatesIndexPage projectName="test-project" />)
    expect(screen.getByText(/templates fetch failed/i)).toBeInTheDocument()
  })

  // -------------------------------------------------------------------------
  // Single-kind grid — no kind filter
  // -------------------------------------------------------------------------

  it('does NOT render a kind filter when only one kind exists', () => {
    setupMocks({ templates: [makeTemplate('my-template')] })
    render(<ProjectTemplatesIndexPage projectName="test-project" />)
    // ResourceGrid only renders the kind-filter when there are 2+ kinds.
    expect(screen.queryByTestId('kind-filter')).not.toBeInTheDocument()
  })

  // -------------------------------------------------------------------------
  // New button
  // -------------------------------------------------------------------------

  it('shows "New Template" button for owner', () => {
    setupMocks({ projectRole: Role.OWNER })
    render(<ProjectTemplatesIndexPage projectName="test-project" />)
    expect(screen.getByRole('button', { name: /new template$/i })).toBeInTheDocument()
  })

  it('New button links to the clone page', () => {
    setupMocks({ projectRole: Role.OWNER })
    render(<ProjectTemplatesIndexPage projectName="test-project" />)
    const newLink = screen.getByRole('link', { name: /new template/i })
    expect(newLink).toHaveAttribute('href', '/projects/test-project/templates/new')
  })

  // -------------------------------------------------------------------------
  // Delete flow
  // -------------------------------------------------------------------------

  it('delete button opens ConfirmDeleteDialog', async () => {
    setupMocks({
      templates: [makeTemplate('my-template', 'project-test-project')],
    })
    render(<ProjectTemplatesIndexPage projectName="test-project" />)
    const deleteBtn = screen.getByRole('button', { name: /delete my-template/i })
    fireEvent.click(deleteBtn)
    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })
  })

  // -------------------------------------------------------------------------
  // Parent column — single parent (project) — column hidden
  // -------------------------------------------------------------------------

  it('Parent column hidden when all rows share the same project parent', () => {
    setupMocks({
      templates: [
        makeTemplate('tpl-a', 'project-test-project'),
        makeTemplate('tpl-b', 'project-test-project'),
      ],
    })
    render(<ProjectTemplatesIndexPage projectName="test-project" />)
    expect(screen.queryByRole('columnheader', { name: /parent/i })).not.toBeInTheDocument()
  })

  // -------------------------------------------------------------------------
  // useListTemplates called with project namespace
  // -------------------------------------------------------------------------

  it('calls useListTemplates with the project namespace', () => {
    setupMocks()
    render(<ProjectTemplatesIndexPage projectName="test-project" />)
    expect(useListTemplates).toHaveBeenCalledWith('project-test-project')
  })

  // -------------------------------------------------------------------------
  // Created At column
  // -------------------------------------------------------------------------

  it('Template row renders a localised date when createdAt is set', () => {
    setupMocks({
      templates: [makeTemplate('tpl-with-date', 'project-test-project')],
    })
    render(<ProjectTemplatesIndexPage projectName="test-project" />)
    // TEST_ISO = '2026-04-22T19:51:10.000Z' → en-US locale → '4/22/2026'
    expect(screen.getByText('4/22/2026')).toBeInTheDocument()
  })

  it('Template row renders em-dash when createdAt is empty string', () => {
    setupMocks({
      templates: [{ ...makeTemplate('tpl-no-date'), createdAt: '' }],
    })
    render(<ProjectTemplatesIndexPage projectName="test-project" />)
    expect(screen.getByText('—')).toBeInTheDocument()
  })

  // -------------------------------------------------------------------------
  // Help pane toggle
  // -------------------------------------------------------------------------

  it('renders the help button', () => {
    setupMocks()
    render(<ProjectTemplatesIndexPage projectName="test-project" />)
    expect(screen.getByTestId('templates-help-button')).toBeInTheDocument()
  })
})

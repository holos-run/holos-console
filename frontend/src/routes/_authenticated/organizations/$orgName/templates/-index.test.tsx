/**
 * Tests for the org templates index — ResourceGrid v1 migration (HOL-975).
 *
 * The page fans out across all org-reachable namespaces via useAllTemplatesForOrg
 * and renders rows via ResourceGrid v1 with scope-aware detailHref values.
 */

import { render, screen, within } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

const mockNavigate = vi.fn()
const mockSearch: Record<string, unknown> = {}

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => ({ orgName: 'test-org' }),
      useSearch: () => mockSearch,
      fullPath: '/organizations/$orgName/templates/',
    }),
    Link: ({
      children,
      to,
      params,
      search,
      title,
      className,
    }: {
      children: React.ReactNode
      to: string
      params?: Record<string, string>
      search?: Record<string, string>
      title?: string
      className?: string
    }) => {
      let href = to
      if (params) {
        for (const [k, v] of Object.entries(params)) {
          href = href.replace(`$${k}`, v)
        }
      }
      if (search) {
        const qs = new URLSearchParams(search).toString()
        if (qs) href = `${href}?${qs}`
      }
      return (
        <a href={href} title={title} className={className}>
          {children}
        </a>
      )
    },
    useNavigate: () => mockNavigate,
  }
})

vi.mock('@/queries/templates', async () => {
  const actual = await vi.importActual<typeof import('@/queries/templates')>(
    '@/queries/templates',
  )
  return {
    ...actual,
    useAllTemplatesForOrg: vi.fn(),
  }
})

vi.mock('@/queries/organizations', () => ({
  useGetOrganization: vi.fn(),
}))

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

import { useAllTemplatesForOrg } from '@/queries/templates'
import { useGetOrganization } from '@/queries/organizations'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import {
  namespaceForOrg,
  namespaceForFolder,
  namespaceForProject,
} from '@/lib/scope-labels'
import { OrgTemplatesIndexPage } from './index'

type TemplateFixture = {
  name: string
  namespace: string
  displayName: string
  createdAt?: string
}

const ORG_NS = namespaceForOrg('test-org')
const FOLDER_NS = namespaceForFolder('team-a')
const PROJECT_NS = namespaceForProject('billing')

function setup(
  templates: TemplateFixture[] = [],
  userRole: Role = Role.OWNER,
  overrides: Partial<{ isPending: boolean; error: Error | null }> = {},
) {
  ;(useAllTemplatesForOrg as Mock).mockReturnValue({
    data: overrides.isPending ? undefined : templates,
    isPending: overrides.isPending ?? false,
    error: overrides.error ?? null,
  })
  ;(useGetOrganization as Mock).mockReturnValue({
    data: { name: 'test-org', userRole },
    isPending: false,
    error: null,
  })
}

describe('OrgTemplatesIndexPage (ResourceGrid v1, HOL-975)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    Object.keys(mockSearch).forEach((k) => delete mockSearch[k])
  })

  // ---------------------------------------------------------------------------
  // Loading and error states
  // ---------------------------------------------------------------------------

  it('renders loading skeletons while the fan-out is pending', () => {
    setup([], Role.OWNER, { isPending: true })
    render(<OrgTemplatesIndexPage orgName="test-org" />)
    expect(screen.getByTestId('resource-grid-loading')).toBeInTheDocument()
  })

  it('renders the full-page error when the fan-out fails with no data', () => {
    setup([], Role.OWNER, { error: new Error('boom') })
    render(<OrgTemplatesIndexPage orgName="test-org" />)
    expect(screen.getByText('boom')).toBeInTheDocument()
    // Full-page error suppresses the grid.
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
  })

  it('renders an inline partial-error banner when some rows loaded', () => {
    ;(useAllTemplatesForOrg as Mock).mockReturnValue({
      data: [{ name: 'gateway', namespace: ORG_NS, displayName: 'Gateway', createdAt: '' }],
      isPending: false,
      error: new Error('folders unavailable'),
    })
    ;(useGetOrganization as Mock).mockReturnValue({
      data: { name: 'test-org', userRole: Role.OWNER },
      isPending: false,
      error: null,
    })
    render(<OrgTemplatesIndexPage orgName="test-org" />)
    expect(screen.getByText('Gateway')).toBeInTheDocument()
    expect(screen.getByRole('table')).toBeInTheDocument()
    expect(screen.getByTestId('resource-grid-partial-error')).toBeInTheDocument()
    expect(screen.getByText('folders unavailable')).toBeInTheDocument()
  })

  it('renders the empty state when no templates exist', () => {
    setup([])
    render(<OrgTemplatesIndexPage orgName="test-org" />)
    expect(screen.getByText(/no resources found/i)).toBeInTheDocument()
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
  })

  // ---------------------------------------------------------------------------
  // Row rendering
  // ---------------------------------------------------------------------------

  it('renders a row for each template across scopes', () => {
    setup([
      { name: 'gateway', namespace: ORG_NS, displayName: 'Gateway', createdAt: '' },
      { name: 'backend', namespace: FOLDER_NS, displayName: 'Backend', createdAt: '' },
      { name: 'web', namespace: PROJECT_NS, displayName: 'Web Service', createdAt: '' },
    ])
    render(<OrgTemplatesIndexPage orgName="test-org" />)

    expect(screen.getByText('Gateway')).toBeInTheDocument()
    expect(screen.getByText('Backend')).toBeInTheDocument()
    expect(screen.getByText('Web Service')).toBeInTheDocument()
  })

  it('routes org-scoped rows to the org editor', () => {
    setup([{ name: 'gateway', namespace: ORG_NS, displayName: 'Gateway', createdAt: '' }])
    render(<OrgTemplatesIndexPage orgName="test-org" />)
    // The Resource ID cell and display name cell both link to the detail page.
    const links = screen.getAllByRole('link', { name: /gateway/i })
    expect(
      links.some(
        (l) =>
          l.getAttribute('href') ===
          `/organizations/test-org/templates/${ORG_NS}/gateway`,
      ),
    ).toBe(true)
  })

  it('routes folder-scoped rows to the folder template editor', () => {
    setup([
      { name: 'backend', namespace: FOLDER_NS, displayName: 'Backend', createdAt: '' },
    ])
    render(<OrgTemplatesIndexPage orgName="test-org" />)
    const links = screen.getAllByRole('link', { name: /backend/i })
    expect(
      links.some(
        (l) => l.getAttribute('href') === '/folders/team-a/templates/backend',
      ),
    ).toBe(true)
  })

  it('routes project-scoped rows to the project template editor', () => {
    setup([
      { name: 'web', namespace: PROJECT_NS, displayName: 'Web Service', createdAt: '' },
    ])
    render(<OrgTemplatesIndexPage orgName="test-org" />)
    const links = screen.getAllByRole('link', { name: /web service/i })
    expect(
      links.some(
        (l) => l.getAttribute('href') === '/projects/billing/templates/web',
      ),
    ).toBe(true)
  })

  it('falls back to the slug when displayName is empty', () => {
    setup([{ name: 'ops', namespace: ORG_NS, displayName: '', createdAt: '' }])
    render(<OrgTemplatesIndexPage orgName="test-org" />)
    const links = screen.getAllByRole('link', { name: /^ops$/i })
    expect(
      links.some(
        (l) =>
          l.getAttribute('href') ===
          `/organizations/test-org/templates/${ORG_NS}/ops`,
      ),
    ).toBe(true)
  })

  // ---------------------------------------------------------------------------
  // Scope badge column
  // ---------------------------------------------------------------------------

  it('renders a Scope column header', () => {
    setup([{ name: 'gateway', namespace: ORG_NS, displayName: 'Gateway', createdAt: '' }])
    render(<OrgTemplatesIndexPage orgName="test-org" />)
    expect(
      screen.getByRole('columnheader', { name: /^scope$/i }),
    ).toBeInTheDocument()
  })

  it('renders scope badges for org, folder, and project rows', () => {
    setup([
      { name: 'gateway', namespace: ORG_NS, displayName: 'Gateway', createdAt: '' },
      { name: 'backend', namespace: FOLDER_NS, displayName: 'Backend', createdAt: '' },
      { name: 'web', namespace: PROJECT_NS, displayName: 'Web Service', createdAt: '' },
    ])
    render(<OrgTemplatesIndexPage orgName="test-org" />)
    expect(screen.getByText('Organization: test-org')).toBeInTheDocument()
    expect(screen.getByText('Folder: team-a')).toBeInTheDocument()
    expect(screen.getByText('Project: billing')).toBeInTheDocument()
  })

  // ---------------------------------------------------------------------------
  // Create CTA
  // ---------------------------------------------------------------------------

  it('renders a Create Template / New button for org OWNERs', () => {
    setup([], Role.OWNER)
    render(<OrgTemplatesIndexPage orgName="test-org" />)
    // ResourceGrid renders a "New Template" button linking to the newHref.
    const newLink = screen.getByRole('link', { name: /template/i })
    expect(newLink).toHaveAttribute('href', '/organizations/test-org/templates/new')
  })

  it('hides the Create button for non-OWNER users', () => {
    setup([], Role.VIEWER)
    render(<OrgTemplatesIndexPage orgName="test-org" />)
    // When canCreate is false, ResourceGrid suppresses the New button.
    expect(
      screen.queryByRole('link', { name: /template/i }),
    ).not.toBeInTheDocument()
  })

  // ---------------------------------------------------------------------------
  // Scope filter dropdown
  // ---------------------------------------------------------------------------

  it('renders a scope filter select in the toolbar', () => {
    setup([{ name: 'gateway', namespace: ORG_NS, displayName: 'Gateway', createdAt: '' }])
    render(<OrgTemplatesIndexPage orgName="test-org" />)
    expect(
      screen.getByRole('combobox', { name: /filter by scope/i }),
    ).toBeInTheDocument()
  })

  // ---------------------------------------------------------------------------
  // Namespace column
  // ---------------------------------------------------------------------------

  it('renders a Namespace column header', () => {
    setup([{ name: 'gateway', namespace: ORG_NS, displayName: 'Gateway', createdAt: '' }])
    render(<OrgTemplatesIndexPage orgName="test-org" />)
    expect(
      screen.getByRole('columnheader', { name: /namespace/i }),
    ).toBeInTheDocument()
  })

  it('renders the namespace value for each row', () => {
    setup([
      { name: 'gateway', namespace: ORG_NS, displayName: 'Gateway', createdAt: '' },
    ])
    render(<OrgTemplatesIndexPage orgName="test-org" />)
    // Namespace column renders the raw namespace string.
    const ns = screen.getAllByText(ORG_NS)
    expect(ns.length).toBeGreaterThan(0)
  })

  // ---------------------------------------------------------------------------
  // Scope filtering via URL state
  // ---------------------------------------------------------------------------

  it('filters to only org-scoped rows when scope=org is set in search', () => {
    mockSearch['scope'] = 'org'
    setup([
      { name: 'gateway', namespace: ORG_NS, displayName: 'Gateway', createdAt: '' },
      { name: 'web', namespace: PROJECT_NS, displayName: 'Web Service', createdAt: '' },
    ])
    render(<OrgTemplatesIndexPage orgName="test-org" />)
    expect(screen.getByText('Gateway')).toBeInTheDocument()
    expect(screen.queryByText('Web Service')).not.toBeInTheDocument()
  })

  it('filters to only project-scoped rows when scope=project is set in search', () => {
    mockSearch['scope'] = 'project'
    setup([
      { name: 'gateway', namespace: ORG_NS, displayName: 'Gateway', createdAt: '' },
      { name: 'web', namespace: PROJECT_NS, displayName: 'Web Service', createdAt: '' },
    ])
    render(<OrgTemplatesIndexPage orgName="test-org" />)
    expect(screen.queryByText('Gateway')).not.toBeInTheDocument()
    expect(screen.getByText('Web Service')).toBeInTheDocument()
  })

  it('shows all rows when no scope filter is set', () => {
    setup([
      { name: 'gateway', namespace: ORG_NS, displayName: 'Gateway', createdAt: '' },
      { name: 'web', namespace: PROJECT_NS, displayName: 'Web Service', createdAt: '' },
    ])
    render(<OrgTemplatesIndexPage orgName="test-org" />)
    expect(screen.getByText('Gateway')).toBeInTheDocument()
    expect(screen.getByText('Web Service')).toBeInTheDocument()
  })

  // ---------------------------------------------------------------------------
  // Grid title
  // ---------------------------------------------------------------------------

  it('renders the grid title with orgName', () => {
    setup([])
    render(<OrgTemplatesIndexPage orgName="test-org" />)
    expect(screen.getByText('test-org / Templates')).toBeInTheDocument()
  })
})

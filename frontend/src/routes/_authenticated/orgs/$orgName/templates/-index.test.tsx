import { render, screen, fireEvent, within } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => ({ orgName: 'test-org' }),
    }),
    Link: ({
      children,
      to,
      params,
      title,
      className,
    }: {
      children: React.ReactNode
      to: string
      params?: Record<string, string>
      title?: string
      className?: string
    }) => {
      let href = to
      if (params) {
        for (const [k, v] of Object.entries(params)) {
          href = href.replace(`$${k}`, v)
        }
      }
      return (
        <a href={href} title={title} className={className}>
          {children}
        </a>
      )
    },
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

describe('OrgTemplatesIndexPage', () => {
  beforeEach(() => vi.clearAllMocks())

  it('renders loading skeletons while the fan-out is pending', () => {
    setup([], Role.OWNER, { isPending: true })
    render(<OrgTemplatesIndexPage orgName="test-org" />)
    expect(screen.getByTestId('templates-loading')).toBeInTheDocument()
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
      data: [{ name: 'gateway', namespace: ORG_NS, displayName: 'Gateway' }],
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
    expect(screen.getByTestId('templates-partial-error')).toBeInTheDocument()
    expect(screen.getByText('folders unavailable')).toBeInTheDocument()
  })

  it('renders the empty state when no templates exist', () => {
    setup([])
    render(<OrgTemplatesIndexPage orgName="test-org" />)
    expect(screen.getByText(/no templates yet/i)).toBeInTheDocument()
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
  })

  it('renders a row for each template across scopes', () => {
    setup([
      { name: 'gateway', namespace: ORG_NS, displayName: 'Gateway' },
      { name: 'backend', namespace: FOLDER_NS, displayName: 'Backend' },
      { name: 'web', namespace: PROJECT_NS, displayName: 'Web Service' },
    ])
    render(<OrgTemplatesIndexPage orgName="test-org" />)

    const rows = screen.getAllByRole('row')
    // 1 header + 3 body rows
    expect(rows).toHaveLength(4)

    expect(within(rows[1]).getByText('Gateway')).toBeInTheDocument()
    expect(within(rows[1]).getByText(ORG_NS)).toBeInTheDocument()
    expect(within(rows[2]).getByText('Backend')).toBeInTheDocument()
    expect(within(rows[2]).getByText(FOLDER_NS)).toBeInTheDocument()
    expect(within(rows[3]).getByText('Web Service')).toBeInTheDocument()
    expect(within(rows[3]).getByText(PROJECT_NS)).toBeInTheDocument()
  })

  it('routes org-scoped rows to the org editor', () => {
    setup([{ name: 'gateway', namespace: ORG_NS, displayName: 'Gateway' }])
    render(<OrgTemplatesIndexPage orgName="test-org" />)
    const link = screen.getByRole('link', { name: 'Gateway' })
    expect(link).toHaveAttribute(
      'href',
      `/orgs/test-org/templates/${ORG_NS}/gateway`,
    )
  })

  it('routes folder-scoped rows to the folder template editor', () => {
    setup([
      { name: 'backend', namespace: FOLDER_NS, displayName: 'Backend' },
    ])
    render(<OrgTemplatesIndexPage orgName="test-org" />)
    const link = screen.getByRole('link', { name: 'Backend' })
    expect(link).toHaveAttribute(
      'href',
      '/folders/team-a/templates/backend',
    )
  })

  it('routes project-scoped rows to the project template editor', () => {
    setup([
      { name: 'web', namespace: PROJECT_NS, displayName: 'Web Service' },
    ])
    render(<OrgTemplatesIndexPage orgName="test-org" />)
    const link = screen.getByRole('link', { name: 'Web Service' })
    expect(link).toHaveAttribute(
      'href',
      '/projects/billing/templates/web',
    )
  })

  it('renders a plain span for unknown namespaces', () => {
    // Stale caches or proto drift could surface an unrecognizable namespace.
    // Rather than forge a 404 link, the cell must render as plain text.
    setup([{ name: 'strange', namespace: 'mystery-ns', displayName: 'Strange' }])
    render(<OrgTemplatesIndexPage orgName="test-org" />)
    expect(screen.queryByRole('link', { name: 'Strange' })).toBeNull()
    expect(screen.getByText('Strange')).toBeInTheDocument()
  })

  it('falls back to the slug when displayName is empty', () => {
    setup([{ name: 'ops', namespace: ORG_NS, displayName: '' }])
    render(<OrgTemplatesIndexPage orgName="test-org" />)
    const links = screen.getAllByRole('link', { name: 'ops' })
    expect(
      links.some(
        (l) =>
          l.getAttribute('href') === `/orgs/test-org/templates/${ORG_NS}/ops`,
      ),
    ).toBe(true)
  })

  it('filters rows via the global search input by display name', () => {
    setup([
      { name: 'gateway', namespace: ORG_NS, displayName: 'Gateway' },
      { name: 'backend', namespace: FOLDER_NS, displayName: 'Backend' },
    ])
    render(<OrgTemplatesIndexPage orgName="test-org" />)

    expect(screen.getAllByRole('row')).toHaveLength(3)

    const search = screen.getByLabelText(/search templates/i)
    fireEvent.change(search, { target: { value: 'Gate' } })

    const rowsAfter = screen.getAllByRole('row')
    expect(rowsAfter).toHaveLength(2)
    expect(within(rowsAfter[1]).getByText('Gateway')).toBeInTheDocument()
  })

  it('filters rows by namespace', () => {
    setup([
      { name: 'gateway', namespace: ORG_NS, displayName: 'Gateway' },
      { name: 'web', namespace: PROJECT_NS, displayName: 'Web Service' },
    ])
    render(<OrgTemplatesIndexPage orgName="test-org" />)

    const search = screen.getByLabelText(/search templates/i)
    fireEvent.change(search, { target: { value: PROJECT_NS } })

    const rows = screen.getAllByRole('row')
    expect(rows).toHaveLength(2)
    expect(within(rows[1]).getByText('Web Service')).toBeInTheDocument()
  })

  it('filters rows by slug name', () => {
    setup([
      { name: 'gateway', namespace: ORG_NS, displayName: 'Gateway' },
      { name: 'backend', namespace: FOLDER_NS, displayName: 'Backend' },
    ])
    render(<OrgTemplatesIndexPage orgName="test-org" />)

    const search = screen.getByLabelText(/search templates/i)
    fireEvent.change(search, { target: { value: 'back' } })

    const rows = screen.getAllByRole('row')
    expect(rows).toHaveLength(2)
    expect(within(rows[1]).getByText('Backend')).toBeInTheDocument()
  })

  it('renders the Create Template button for org OWNERs', () => {
    setup([], Role.OWNER)
    render(<OrgTemplatesIndexPage orgName="test-org" />)
    const link = screen.getByRole('link', { name: /create template/i })
    expect(link).toHaveAttribute('href', '/orgs/test-org/templates/new')
  })

  it('hides the Create Template button for non-OWNER users', () => {
    setup([], Role.VIEWER)
    render(<OrgTemplatesIndexPage orgName="test-org" />)
    expect(
      screen.queryByRole('link', { name: /create template/i }),
    ).not.toBeInTheDocument()
  })

  it('empty state prompts OWNERs to create a template', () => {
    setup([], Role.OWNER)
    render(<OrgTemplatesIndexPage orgName="test-org" />)
    expect(
      screen.getByText(/no templates yet\. create one to get started/i),
    ).toBeInTheDocument()
  })

  it('empty state directs non-OWNERs to ask an owner', () => {
    setup([], Role.VIEWER)
    render(<OrgTemplatesIndexPage orgName="test-org" />)
    expect(
      screen.getByText(/ask an organization owner to create one/i),
    ).toBeInTheDocument()
  })

  // HOL-793: Scope column teaches users which rows live where at a glance.
  it('renders a Scope column with a badge per row', () => {
    setup([
      { name: 'gateway', namespace: ORG_NS, displayName: 'Gateway' },
      { name: 'backend', namespace: FOLDER_NS, displayName: 'Backend' },
      { name: 'web', namespace: PROJECT_NS, displayName: 'Web Service' },
    ])
    render(<OrgTemplatesIndexPage orgName="test-org" />)
    expect(
      screen.getByRole('columnheader', { name: /^scope$/i }),
    ).toBeInTheDocument()
    expect(screen.getByText('Organization: test-org')).toBeInTheDocument()
    expect(screen.getByText('Folder: team-a')).toBeInTheDocument()
    expect(screen.getByText('Project: billing')).toBeInTheDocument()
  })

  it('renders a scope filter select in the toolbar', () => {
    setup([{ name: 'gateway', namespace: ORG_NS, displayName: 'Gateway' }])
    render(<OrgTemplatesIndexPage orgName="test-org" />)
    expect(
      screen.getByRole('combobox', { name: /filter by scope/i }),
    ).toBeInTheDocument()
  })
})

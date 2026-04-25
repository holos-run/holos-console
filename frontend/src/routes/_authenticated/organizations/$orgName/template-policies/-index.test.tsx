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

vi.mock('@/queries/templatePolicies', async () => {
  const actual = await vi.importActual<typeof import('@/queries/templatePolicies')>(
    '@/queries/templatePolicies',
  )
  return {
    ...actual,
    useListTemplatePolicies: vi.fn(),
  }
})

vi.mock('@/queries/organizations', () => ({
  useGetOrganization: vi.fn(),
}))

import {
  useListTemplatePolicies,
} from '@/queries/templatePolicies'
import { useGetOrganization } from '@/queries/organizations'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { namespaceForOrg, namespaceForFolder } from '@/lib/scope-labels'
import { OrgTemplatePoliciesIndexPage } from './index'

type PolicyFixture = {
  name: string
  namespace: string
  displayName: string
  description?: string
  creatorEmail?: string
  rules: unknown[]
}

function makePolicy(
  name: string,
  namespace: string,
  displayName = name,
): PolicyFixture {
  return {
    name,
    namespace,
    displayName,
    description: '',
    creatorEmail: '',
    rules: [],
  }
}

function setup(
  policies: PolicyFixture[] = [],
  userRole: Role = Role.OWNER,
  overrides: Partial<{ isPending: boolean; error: Error | null }> = {},
) {
  ;(useListTemplatePolicies as Mock).mockReturnValue({
    data: overrides.isPending ? undefined : policies,
    isPending: overrides.isPending ?? false,
    error: overrides.error ?? null,
  })
  ;(useGetOrganization as Mock).mockReturnValue({
    data: { name: 'test-org', userRole },
    isPending: false,
    error: null,
  })
}

const ORG_NS = namespaceForOrg('test-org')
const FOLDER_NS = namespaceForFolder('team-alpha')

describe('OrgTemplatePoliciesIndexPage', () => {
  beforeEach(() => vi.clearAllMocks())

  it('renders skeleton while loading', () => {
    setup([], Role.OWNER, { isPending: true })
    const { container } = render(<OrgTemplatePoliciesIndexPage orgName="test-org" />)
    expect(container.querySelector('[data-testid="policies-loading"]')).toBeInTheDocument()
  })

  it('renders error alert when the fan-out fails with no data', () => {
    setup([], Role.OWNER, { error: new Error('bad gateway') })
    render(<OrgTemplatePoliciesIndexPage orgName="test-org" />)
    expect(screen.getByText('bad gateway')).toBeInTheDocument()
    // full-page error — table should not be rendered
    expect(screen.queryByRole('table')).toBeNull()
  })

  it('renders rows with inline warning banner when partial data and error coexist', () => {
    ;(useListTemplatePolicies as Mock).mockReturnValue({
      data: [makePolicy('p-org', namespaceForOrg('test-org'), 'Org Policy')],
      isPending: false,
      error: new Error('folders unavailable'),
    })
    ;(useGetOrganization as Mock).mockReturnValue({
      data: { name: 'test-org', userRole: Role.OWNER },
      isPending: false,
      error: null,
    })
    render(<OrgTemplatePoliciesIndexPage orgName="test-org" />)
    // rows must be visible
    expect(screen.getByText('Org Policy')).toBeInTheDocument()
    expect(screen.getByRole('table')).toBeInTheDocument()
    // inline warning banner must be present
    expect(screen.getByTestId('policies-partial-error')).toBeInTheDocument()
    expect(screen.getByText('folders unavailable')).toBeInTheDocument()
  })

  it('renders empty state when no policies exist', () => {
    setup([])
    render(<OrgTemplatePoliciesIndexPage orgName="test-org" />)
    expect(screen.getByText(/no template policies yet/i)).toBeInTheDocument()
  })

  it('renders org-scoped policies only', () => {
    setup([makePolicy('p-org', ORG_NS, 'Org Policy')])
    render(<OrgTemplatePoliciesIndexPage orgName="test-org" />)
    expect(screen.getByText('Org Policy')).toBeInTheDocument()
    expect(screen.getAllByText(ORG_NS).length).toBeGreaterThan(0)
    expect(screen.getByText('p-org')).toBeInTheDocument()
  })

  it('renders folder-scoped policies only', () => {
    setup([makePolicy('p-folder', FOLDER_NS, 'Folder Policy')])
    render(<OrgTemplatePoliciesIndexPage orgName="test-org" />)
    expect(screen.getByText('Folder Policy')).toBeInTheDocument()
    expect(screen.getAllByText(FOLDER_NS).length).toBeGreaterThan(0)
  })

  it('renders org and folder policies combined in one grid', () => {
    setup([
      makePolicy('p-org', ORG_NS, 'Org Policy'),
      makePolicy('p-folder', FOLDER_NS, 'Folder Policy'),
    ])
    render(<OrgTemplatePoliciesIndexPage orgName="test-org" />)
    expect(screen.getByText('Org Policy')).toBeInTheDocument()
    expect(screen.getByText('Folder Policy')).toBeInTheDocument()
  })

  it('routes org-scoped rows to the org detail page', () => {
    setup([makePolicy('p-org', ORG_NS, 'Org Policy')])
    render(<OrgTemplatePoliciesIndexPage orgName="test-org" />)
    const link = screen.getByRole('link', { name: 'Org Policy' })
    expect(link).toHaveAttribute(
      'href',
      '/organizations/test-org/template-policies/p-org',
    )
  })

  it('routes folder-scoped rows to the folder detail page', () => {
    setup([makePolicy('p-folder', FOLDER_NS, 'Folder Policy')])
    render(<OrgTemplatePoliciesIndexPage orgName="test-org" />)
    const link = screen.getByRole('link', { name: 'Folder Policy' })
    expect(link).toHaveAttribute(
      'href',
      '/folders/team-alpha/template-policies/p-folder',
    )
  })

  it('falls back to the name when displayName is empty', () => {
    setup([makePolicy('p-nodn', ORG_NS, '')])
    render(<OrgTemplatePoliciesIndexPage orgName="test-org" />)
    const link = screen.getByRole('link', { name: 'p-nodn' })
    expect(link).toBeInTheDocument()
  })

  it('renders a plain span (not a link) for unknown or project-scoped namespaces', () => {
    // Safety net for stale caches or proto drift: HOL-590 guarantees policies
    // live only at org or folder scope, but if the server ever surfaces a
    // project-scoped row we must not forge a link to a 404 page.
    setup([
      makePolicy('p-proj', 'holos-prj-billing', 'Project Policy'),
      makePolicy('p-bad', 'some-other-ns', 'Bad Scope Policy'),
    ])
    render(<OrgTemplatePoliciesIndexPage orgName="test-org" />)
    expect(screen.queryByRole('link', { name: 'Project Policy' })).toBeNull()
    expect(screen.queryByRole('link', { name: 'Bad Scope Policy' })).toBeNull()
    expect(screen.getByText('Project Policy')).toBeInTheDocument()
    expect(screen.getByText('Bad Scope Policy')).toBeInTheDocument()
  })

  it('filters by display name', () => {
    setup([
      makePolicy('p-alpha', ORG_NS, 'Alpha Policy'),
      makePolicy('p-beta', ORG_NS, 'Beta Policy'),
    ])
    render(<OrgTemplatePoliciesIndexPage orgName="test-org" />)
    const search = screen.getByRole('textbox', { name: /search template policies/i })
    fireEvent.change(search, { target: { value: 'alpha' } })
    expect(screen.getByText('Alpha Policy')).toBeInTheDocument()
    expect(screen.queryByText('Beta Policy')).not.toBeInTheDocument()
  })

  it('filters by namespace', () => {
    setup([
      makePolicy('p-org', ORG_NS, 'Org Policy'),
      makePolicy('p-folder', FOLDER_NS, 'Folder Policy'),
    ])
    render(<OrgTemplatePoliciesIndexPage orgName="test-org" />)
    const search = screen.getByRole('textbox', { name: /search template policies/i })
    fireEvent.change(search, { target: { value: 'fld-team-alpha' } })
    expect(screen.getByText('Folder Policy')).toBeInTheDocument()
    expect(screen.queryByText('Org Policy')).not.toBeInTheDocument()
  })

  it('filters by policy name', () => {
    setup([
      makePolicy('reference-grant', ORG_NS, 'ReferenceGrant'),
      makePolicy('tls-required', ORG_NS, 'Require TLS'),
    ])
    render(<OrgTemplatePoliciesIndexPage orgName="test-org" />)
    const search = screen.getByRole('textbox', { name: /search template policies/i })
    fireEvent.change(search, { target: { value: 'reference' } })
    expect(screen.getByText('ReferenceGrant')).toBeInTheDocument()
    expect(screen.queryByText('Require TLS')).not.toBeInTheDocument()
  })

  it('shows Create Policy for OWNER', () => {
    setup([])
    render(<OrgTemplatePoliciesIndexPage orgName="test-org" />)
    expect(screen.getByRole('link', { name: /create policy/i })).toBeInTheDocument()
  })

  it('shows Create Policy for EDITOR', () => {
    setup([], Role.EDITOR)
    render(<OrgTemplatePoliciesIndexPage orgName="test-org" />)
    expect(screen.getByRole('link', { name: /create policy/i })).toBeInTheDocument()
  })

  it('hides Create Policy for VIEWER', () => {
    setup([], Role.VIEWER)
    render(<OrgTemplatePoliciesIndexPage orgName="test-org" />)
    expect(screen.queryByRole('link', { name: /create policy/i })).not.toBeInTheDocument()
  })

  it('renders each policy row with a distinct Display Name cell', () => {
    setup([
      makePolicy('p-org', ORG_NS, 'Org Policy'),
      makePolicy('p-folder', FOLDER_NS, 'Folder Policy'),
    ])
    render(<OrgTemplatePoliciesIndexPage orgName="test-org" />)
    const table = screen.getByRole('table')
    const rows = within(table).getAllByRole('row')
    // 1 header + 2 body rows
    expect(rows.length).toBe(3)
  })

  // HOL-793: the Scope column renders a human-readable label per row so users
  // can tell org-vs-folder rows apart at a glance. Previously scope was only
  // visible from the name-cell link target.
  it('renders a Scope column with a badge per row', () => {
    setup([
      makePolicy('p-org', ORG_NS, 'Org Policy'),
      makePolicy('p-folder', FOLDER_NS, 'Folder Policy'),
    ])
    render(<OrgTemplatePoliciesIndexPage orgName="test-org" />)
    expect(screen.getByRole('columnheader', { name: /^scope$/i })).toBeInTheDocument()
    expect(screen.getByText('Organization: test-org')).toBeInTheDocument()
    expect(screen.getByText('Folder: team-alpha')).toBeInTheDocument()
  })

  // HOL-917: the "All scopes / Organization / Folder" Select was removed. The
  // page is now org-scoped only — no scope filter in the toolbar.
  it('does not render a scope filter select in the toolbar', () => {
    setup([makePolicy('p-org', ORG_NS, 'Org Policy')])
    render(<OrgTemplatePoliciesIndexPage orgName="test-org" />)
    expect(
      screen.queryByRole('combobox', { name: /filter by scope/i }),
    ).not.toBeInTheDocument()
  })

  // HOL-917: prove that org-namespace policies appear in the listing (the page
  // now calls useListTemplatePolicies(orgNamespace) directly).
  it('lists policies returned from the org namespace RPC call', () => {
    setup([
      makePolicy('allow-tls', ORG_NS, 'Allow TLS'),
      makePolicy('deny-http', ORG_NS, 'Deny HTTP'),
    ])
    render(<OrgTemplatePoliciesIndexPage orgName="test-org" />)
    expect(screen.getByText('Allow TLS')).toBeInTheDocument()
    expect(screen.getByText('Deny HTTP')).toBeInTheDocument()
  })
})

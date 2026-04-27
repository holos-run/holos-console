/**
 * Tests for OrgTemplatePoliciesIndexPage (HOL-948) — ResourceGrid v1 migration.
 *
 * Mocks @/queries/templatePolicies and @/queries/organizations.
 * Exercises: grid render, extra Scope + Rules columns, row navigation,
 * loading/error states, empty state, create-button visibility.
 */

import { render, screen } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { namespaceForOrg, namespaceForFolder } from '@/lib/scope-labels'
import { mockResourcePermissionsForRole } from '@/test/resource-permissions'

// ---------------------------------------------------------------------------
// Router mock — Route.useParams / useSearch / useNavigate / fullPath
// ---------------------------------------------------------------------------

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => ({ orgName: 'test-org' }),
      useSearch: () => ({}),
      fullPath: '/organizations/test-org/template-policies/',
    }),
    Link: ({
      children,
      to,
      params,
      className,
    }: {
      children: React.ReactNode
      to?: string
      params?: Record<string, string>
      className?: string
    }) => {
      let href = to ?? '#'
      if (params) {
        for (const [k, v] of Object.entries(params)) {
          href = href.replace(`$${k}`, v)
        }
      }
      return (
        <a href={href} className={className}>
          {children}
        </a>
      )
    },
    useNavigate: () => vi.fn(),
  }
})

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

vi.mock('@/queries/organizations', () => ({
  useGetOrganization: vi.fn(),
}))

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

// ---------------------------------------------------------------------------
// Imports after mocks
// ---------------------------------------------------------------------------

import { useListTemplatePolicies, useDeleteTemplatePolicy } from '@/queries/templatePolicies'
import { useGetOrganization } from '@/queries/organizations'
import { OrgTemplatePoliciesIndexPage } from './index'

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

type PolicyFixture = {
  name: string
  namespace: string
  displayName: string
  description?: string
  creatorEmail?: string
  rules: unknown[]
  createdAt?: undefined
}

function makePolicy(
  name: string,
  namespace: string,
  displayName = name,
  rules: unknown[] = [],
): PolicyFixture {
  return {
    name,
    namespace,
    displayName,
    description: '',
    creatorEmail: '',
    rules,
    createdAt: undefined,
  }
}

function setup(
  policies: PolicyFixture[] = [],
  userRole: Role = Role.OWNER,
  overrides: Partial<{ isPending: boolean; error: Error | null }> = {},
) {
  mockResourcePermissionsForRole(userRole)
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
  ;(useDeleteTemplatePolicy as Mock).mockReturnValue({
    mutateAsync: vi.fn().mockResolvedValue(undefined),
    isPending: false,
  })
}

const ORG_NS = namespaceForOrg('test-org')
const FOLDER_NS = namespaceForFolder('team-alpha')

describe('OrgTemplatePoliciesIndexPage (ResourceGrid v1)', () => {
  beforeEach(() => vi.clearAllMocks())

  it('renders loading skeleton while fetching', () => {
    setup([], Role.OWNER, { isPending: true })
    render(<OrgTemplatePoliciesIndexPage orgName="test-org" />)
    expect(screen.getByTestId('resource-grid-loading')).toBeInTheDocument()
  })

  it('renders error state when the query fails with no data', () => {
    setup([], Role.OWNER, { error: new Error('bad gateway') })
    render(<OrgTemplatePoliciesIndexPage orgName="test-org" />)
    expect(screen.getByText('bad gateway')).toBeInTheDocument()
  })

  it('renders empty state when no policies exist', () => {
    setup([])
    render(<OrgTemplatePoliciesIndexPage orgName="test-org" />)
    expect(screen.getByText(/no resources found/i)).toBeInTheDocument()
  })

  it('renders org-scoped policy rows in the grid', () => {
    setup([makePolicy('p-org', ORG_NS, 'Org Policy')])
    render(<OrgTemplatePoliciesIndexPage orgName="test-org" />)
    expect(screen.getByText('Org Policy')).toBeInTheDocument()
  })

  it('renders folder-scoped policy rows in the grid', () => {
    setup([makePolicy('p-folder', FOLDER_NS, 'Folder Policy')])
    render(<OrgTemplatePoliciesIndexPage orgName="test-org" />)
    expect(screen.getByText('Folder Policy')).toBeInTheDocument()
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

  it('renders a Scope extra column with badges', () => {
    setup([
      makePolicy('p-org', ORG_NS, 'Org Policy'),
      makePolicy('p-folder', FOLDER_NS, 'Folder Policy'),
    ])
    render(<OrgTemplatePoliciesIndexPage orgName="test-org" />)
    expect(screen.getByRole('columnheader', { name: /^scope$/i })).toBeInTheDocument()
    expect(screen.getByText('Organization: test-org')).toBeInTheDocument()
    expect(screen.getByText('Folder: team-alpha')).toBeInTheDocument()
  })

  it('renders a Rules extra column', () => {
    setup([makePolicy('p-org', ORG_NS, 'Org Policy')])
    render(<OrgTemplatePoliciesIndexPage orgName="test-org" />)
    expect(screen.getByRole('columnheader', { name: /^rules$/i })).toBeInTheDocument()
  })

  it('org-scoped row links to the org detail page via detailHref', () => {
    setup([makePolicy('p-org', ORG_NS, 'Org Policy')])
    render(<OrgTemplatePoliciesIndexPage orgName="test-org" />)
    // ResourceGrid renders a Link for the display name when detailHref is set.
    const links = screen.getAllByRole('link', { name: 'Org Policy' })
    expect(links.length).toBeGreaterThan(0)
    expect(links[0].getAttribute('href')).toContain('template-policies/p-org')
  })

  it('falls back to the name when displayName is empty', () => {
    setup([makePolicy('p-nodn', ORG_NS, '')])
    render(<OrgTemplatePoliciesIndexPage orgName="test-org" />)
    // The display name cell falls back to the name field.
    const links = screen.getAllByRole('link', { name: 'p-nodn' })
    expect(links.length).toBeGreaterThan(0)
  })

  it('shows Create Policy button for OWNER', () => {
    setup([])
    render(<OrgTemplatePoliciesIndexPage orgName="test-org" />)
    expect(screen.getByRole('button', { name: /new template policy/i })).toBeInTheDocument()
  })

  it('shows Create Policy button for EDITOR', () => {
    setup([], Role.EDITOR)
    render(<OrgTemplatePoliciesIndexPage orgName="test-org" />)
    expect(screen.getByRole('button', { name: /new template policy/i })).toBeInTheDocument()
  })

  it('hides Create Policy button for VIEWER', () => {
    setup([], Role.VIEWER)
    render(<OrgTemplatePoliciesIndexPage orgName="test-org" />)
    expect(screen.queryByRole('button', { name: /new template policy/i })).not.toBeInTheDocument()
  })
})

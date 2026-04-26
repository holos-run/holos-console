/**
 * Tests for the unified org templates index — four-facet surface (HOL-1006).
 *
 * The page fans out across all org-reachable namespaces via:
 *   - useAllTemplatesForOrg (Template rows)
 *   - useAllTemplatePoliciesForOrg (TemplatePolicy rows)
 *   - useAllTemplatePolicyBindingsForOrg (TemplatePolicyBinding rows)
 *
 * ResourceGrid v1 renders rows with scope-aware detailHref values and a
 * kind-filter toolbar that supports selecting one of the three resource kinds.
 */

import { render, screen } from '@testing-library/react'
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

vi.mock('@/queries/templatePolicies', async () => {
  const actual = await vi.importActual<typeof import('@/queries/templatePolicies')>(
    '@/queries/templatePolicies',
  )
  return {
    ...actual,
    useAllTemplatePoliciesForOrg: vi.fn(),
  }
})

vi.mock('@/queries/templatePolicyBindings', async () => {
  const actual = await vi.importActual<typeof import('@/queries/templatePolicyBindings')>(
    '@/queries/templatePolicyBindings',
  )
  return {
    ...actual,
    useAllTemplatePolicyBindingsForOrg: vi.fn(),
  }
})

vi.mock('@/queries/organizations', () => ({
  useGetOrganization: vi.fn(),
}))

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

import { useAllTemplatesForOrg } from '@/queries/templates'
import { useAllTemplatePoliciesForOrg } from '@/queries/templatePolicies'
import { useAllTemplatePolicyBindingsForOrg } from '@/queries/templatePolicyBindings'
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

type PolicyFixture = {
  name: string
  namespace: string
  displayName?: string
}

type BindingFixture = {
  name: string
  namespace: string
  displayName?: string
}

const ORG_NS = namespaceForOrg('test-org')
const FOLDER_NS = namespaceForFolder('team-a')
const PROJECT_NS = namespaceForProject('billing')

function setup(
  templates: TemplateFixture[] = [],
  userRole: Role = Role.OWNER,
  overrides: Partial<{ isPending: boolean; error: Error | null }> = {},
  policies: PolicyFixture[] = [],
  bindings: BindingFixture[] = [],
) {
  ;(useAllTemplatesForOrg as Mock).mockReturnValue({
    data: overrides.isPending ? undefined : templates,
    isPending: overrides.isPending ?? false,
    error: overrides.error ?? null,
  })
  ;(useAllTemplatePoliciesForOrg as Mock).mockReturnValue({
    data: overrides.isPending ? undefined : policies,
    isPending: overrides.isPending ?? false,
    error: null,
  })
  ;(useAllTemplatePolicyBindingsForOrg as Mock).mockReturnValue({
    data: overrides.isPending ? undefined : bindings,
    isPending: overrides.isPending ?? false,
    error: null,
  })
  ;(useGetOrganization as Mock).mockReturnValue({
    data: { name: 'test-org', userRole },
    isPending: false,
    error: null,
  })
}

describe('OrgTemplatesIndexPage (unified four-facet surface, HOL-1006)', () => {
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

  it('renders the empty state when no resources exist', () => {
    setup([])
    render(<OrgTemplatesIndexPage orgName="test-org" />)
    expect(screen.getByText(/no resources found/i)).toBeInTheDocument()
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
  })

  // ---------------------------------------------------------------------------
  // Template row rendering
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

  it('routes org-scoped template rows to the org editor', () => {
    setup([{ name: 'gateway', namespace: ORG_NS, displayName: 'Gateway', createdAt: '' }])
    render(<OrgTemplatesIndexPage orgName="test-org" />)
    const links = screen.getAllByRole('link', { name: /gateway/i })
    expect(
      links.some(
        (l) =>
          l.getAttribute('href') ===
          `/organizations/test-org/templates/${ORG_NS}/gateway`,
      ),
    ).toBe(true)
  })

  it('renders folder-scoped template rows as plain text (no link) after HOL-978 route cut', () => {
    setup([
      { name: 'backend', namespace: FOLDER_NS, displayName: 'Backend', createdAt: '' },
    ])
    render(<OrgTemplatesIndexPage orgName="test-org" />)
    // HOL-978 removed folder routes — no link should exist for folder-scoped rows.
    expect(screen.queryByRole('link', { name: /backend/i })).toBeNull()
    // The cell text should still appear.
    expect(screen.getByText('Backend')).toBeInTheDocument()
  })

  it('routes project-scoped template rows to the project template editor', () => {
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
  // TemplatePolicy row rendering
  // ---------------------------------------------------------------------------

  it('renders TemplatePolicy rows from the policies fan-out', () => {
    setup(
      [],
      Role.OWNER,
      {},
      [{ name: 'require-istio', namespace: ORG_NS, displayName: 'Require Istio' }],
    )
    render(<OrgTemplatesIndexPage orgName="test-org" />)
    expect(screen.getByText('Require Istio')).toBeInTheDocument()
  })

  it('routes org-scoped policy rows to the org policy editor', () => {
    setup(
      [],
      Role.OWNER,
      {},
      [{ name: 'require-istio', namespace: ORG_NS, displayName: 'Require Istio' }],
    )
    render(<OrgTemplatesIndexPage orgName="test-org" />)
    const links = screen.getAllByRole('link', { name: /require-istio/i })
    expect(
      links.some(
        (l) =>
          l.getAttribute('href') ===
          '/organizations/test-org/template-policies/require-istio',
      ),
    ).toBe(true)
  })

  it('renders folder-scoped policy rows as plain text (no link) after HOL-978 route cut', () => {
    setup(
      [],
      Role.OWNER,
      {},
      [{ name: 'folder-policy', namespace: FOLDER_NS }],
    )
    render(<OrgTemplatesIndexPage orgName="test-org" />)
    // HOL-978 removed folder routes — no link should exist for folder-scoped policy rows.
    expect(screen.queryByRole('link', { name: /folder-policy/i })).toBeNull()
    // The cell text should still appear.
    expect(screen.getByText('folder-policy')).toBeInTheDocument()
  })

  // ---------------------------------------------------------------------------
  // TemplatePolicyBinding row rendering
  // ---------------------------------------------------------------------------

  it('renders TemplatePolicyBinding rows from the bindings fan-out', () => {
    setup([], Role.OWNER, {}, [], [{ name: 'bind-istio', namespace: ORG_NS, displayName: 'Bind Istio' }])
    render(<OrgTemplatesIndexPage orgName="test-org" />)
    expect(screen.getByText('Bind Istio')).toBeInTheDocument()
  })

  it('routes org-scoped binding rows to the org binding editor', () => {
    setup([], Role.OWNER, {}, [], [{ name: 'bind-istio', namespace: ORG_NS }])
    render(<OrgTemplatesIndexPage orgName="test-org" />)
    const links = screen.getAllByRole('link', { name: /bind-istio/i })
    expect(
      links.some(
        (l) =>
          l.getAttribute('href') ===
          '/organizations/test-org/template-bindings/bind-istio',
      ),
    ).toBe(true)
  })

  it('renders folder-scoped binding rows as plain text (no link) after HOL-978 route cut', () => {
    setup([], Role.OWNER, {}, [], [{ name: 'folder-bind', namespace: FOLDER_NS }])
    render(<OrgTemplatesIndexPage orgName="test-org" />)
    // HOL-978 removed folder routes — no link should exist for folder-scoped binding rows.
    expect(screen.queryByRole('link', { name: /folder-bind/i })).toBeNull()
    // The cell text should still appear.
    expect(screen.getByText('folder-bind')).toBeInTheDocument()
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
  // Create CTA (three-kind dropdown when OWNER)
  // ---------------------------------------------------------------------------

  it('renders a New button for org OWNERs', () => {
    setup([], Role.OWNER)
    render(<OrgTemplatesIndexPage orgName="test-org" />)
    // ResourceGrid renders a "New" dropdown when multiple kinds are creatable.
    const newBtn = screen.getByRole('button', { name: /new/i })
    expect(newBtn).toBeInTheDocument()
  })

  it('hides the Create button for non-OWNER users', () => {
    setup([], Role.VIEWER)
    render(<OrgTemplatesIndexPage orgName="test-org" />)
    // When canCreate is false for all kinds, ResourceGrid suppresses the New button.
    expect(
      screen.queryByRole('button', { name: /new/i }),
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
  // Scope filtering via URL state (org / folder only — project is excluded
  // because TemplatePolicies and Bindings do not exist at project scope)
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

  it('filters to only folder-scoped rows when scope=folder is set in search', () => {
    mockSearch['scope'] = 'folder'
    setup([
      { name: 'gateway', namespace: ORG_NS, displayName: 'Gateway', createdAt: '' },
      { name: 'backend', namespace: FOLDER_NS, displayName: 'Backend', createdAt: '' },
    ])
    render(<OrgTemplatesIndexPage orgName="test-org" />)
    expect(screen.queryByText('Gateway')).not.toBeInTheDocument()
    expect(screen.getByText('Backend')).toBeInTheDocument()
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

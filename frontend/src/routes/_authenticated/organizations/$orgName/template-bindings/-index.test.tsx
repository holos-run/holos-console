/**
 * Tests for OrgTemplateBindingsIndexPage (HOL-948) — ResourceGrid v1 migration.
 *
 * Mocks @/queries/templatePolicyBindings and @/queries/organizations.
 * Exercises: grid render, extra Scope + Policy + Targets columns, row navigation,
 * loading/error states, empty state, create-button visibility.
 */

import { render, screen } from '@testing-library/react'
import { describe, it, expect, beforeEach, vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'
import { Role } from '@/gen/holos/console/v1/rbac_pb'

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
      fullPath: '/organizations/test-org/template-bindings/',
    }),
    Link: ({
      children,
      to,
      params,
      ...props
    }: React.AnchorHTMLAttributes<HTMLAnchorElement> & {
      children: React.ReactNode
      to?: string
      params?: Record<string, string>
    }) => {
      let href = to ?? ''
      if (params) {
        Object.entries(params).forEach(([k, v]) => {
          href = href.replace(`$${k}`, v)
        })
      }
      return (
        <a href={href} {...props}>
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

vi.mock('@/queries/templatePolicyBindings', async () => {
  const actual = await vi.importActual<
    typeof import('@/queries/templatePolicyBindings')
  >('@/queries/templatePolicyBindings')
  return {
    ...actual,
    useListTemplatePolicyBindings: vi.fn(),
    useDeleteTemplatePolicyBinding: vi.fn(),
  }
})

vi.mock('@/queries/organizations', () => ({
  useGetOrganization: vi.fn(),
}))

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

// ---------------------------------------------------------------------------
// Imports after mocks
// ---------------------------------------------------------------------------

import { useListTemplatePolicyBindings, useDeleteTemplatePolicyBinding } from '@/queries/templatePolicyBindings'
import { useGetOrganization } from '@/queries/organizations'
import { OrgTemplateBindingsIndexPage } from './index'

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function makeBinding(
  name: string,
  options: {
    namespace?: string
    description?: string
    targets?: number
    policyName?: string
  } = {},
) {
  return {
    name,
    displayName: name,
    namespace: options.namespace ?? 'holos-org-test-org',
    description: options.description ?? '',
    creatorEmail: '',
    createdAt: undefined,
    policyRef: options.policyName
      ? { namespace: 'holos-org-test-org', name: options.policyName }
      : undefined,
    targetRefs: Array.from({ length: options.targets ?? 0 }, (_, i) => ({
      kind: 1,
      name: `t-${i}`,
      projectName: 'proj-a',
    })),
  }
}

function setup(
  userRole: Role = Role.OWNER,
  bindings: ReturnType<typeof makeBinding>[] = [],
  isPending = false,
  error: Error | null = null,
) {
  ;(useListTemplatePolicyBindings as Mock).mockReturnValue({
    data: isPending ? undefined : bindings,
    isPending,
    error,
  })
  ;(useGetOrganization as Mock).mockReturnValue({
    data: { name: 'test-org', userRole },
    isPending: false,
    error: null,
  })
  ;(useDeleteTemplatePolicyBinding as Mock).mockReturnValue({
    mutateAsync: vi.fn().mockResolvedValue(undefined),
    isPending: false,
  })
}

describe('OrgTemplateBindingsIndexPage (ResourceGrid v1)', () => {
  beforeEach(() => vi.clearAllMocks())

  it('renders loading skeleton while fetching', () => {
    setup(Role.OWNER, [], true)
    render(<OrgTemplateBindingsIndexPage orgName="test-org" />)
    expect(screen.getByTestId('resource-grid-loading')).toBeInTheDocument()
  })

  it('renders empty state when no bindings exist', () => {
    setup(Role.OWNER, [])
    render(<OrgTemplateBindingsIndexPage orgName="test-org" />)
    expect(screen.getByText(/no resources found/i)).toBeInTheDocument()
  })

  it('renders binding rows in the grid', () => {
    setup(Role.OWNER, [makeBinding('org-bind', { targets: 3, policyName: 'require-http' })])
    render(<OrgTemplateBindingsIndexPage orgName="test-org" />)
    expect(screen.getByText('org-bind')).toBeInTheDocument()
  })

  it('renders a Scope extra column with badge', () => {
    setup(Role.OWNER, [makeBinding('org-bind')])
    render(<OrgTemplateBindingsIndexPage orgName="test-org" />)
    expect(screen.getByRole('columnheader', { name: /^scope$/i })).toBeInTheDocument()
    expect(screen.getByText(/^Organization: test-org$/)).toBeInTheDocument()
  })

  it('renders a Policy extra column with policy name', () => {
    setup(Role.OWNER, [makeBinding('org-bind', { policyName: 'require-http' })])
    render(<OrgTemplateBindingsIndexPage orgName="test-org" />)
    expect(screen.getByRole('columnheader', { name: /^policy$/i })).toBeInTheDocument()
    expect(screen.getByText('require-http')).toBeInTheDocument()
  })

  it('renders a Targets extra column with count badge', () => {
    setup(Role.OWNER, [makeBinding('org-bind', { targets: 3 })])
    render(<OrgTemplateBindingsIndexPage orgName="test-org" />)
    expect(screen.getByRole('columnheader', { name: /^targets$/i })).toBeInTheDocument()
    expect(screen.getByText(/3 targets/)).toBeInTheDocument()
  })

  it('renders org-scoped rows with detailHref links', () => {
    setup(Role.OWNER, [makeBinding('org-bind')])
    render(<OrgTemplateBindingsIndexPage orgName="test-org" />)
    // ResourceGrid renders a Link for the display name when detailHref is set.
    const links = screen.getAllByRole('link', { name: 'org-bind' })
    expect(links.length).toBeGreaterThan(0)
    expect(links[0].getAttribute('href')).toContain('template-bindings/org-bind')
  })

  it('shows Create Binding button for OWNER', () => {
    setup(Role.OWNER, [])
    render(<OrgTemplateBindingsIndexPage orgName="test-org" />)
    expect(screen.getByRole('button', { name: /new template binding/i })).toBeInTheDocument()
  })

  it('shows Create Binding button for EDITOR', () => {
    setup(Role.EDITOR, [])
    render(<OrgTemplateBindingsIndexPage orgName="test-org" />)
    expect(screen.getByRole('button', { name: /new template binding/i })).toBeInTheDocument()
  })

  it('hides Create Binding button for VIEWER', () => {
    setup(Role.VIEWER, [])
    render(<OrgTemplateBindingsIndexPage orgName="test-org" />)
    expect(screen.queryByRole('button', { name: /new template binding/i })).not.toBeInTheDocument()
  })

  it('lists multiple bindings returned from the org namespace RPC call', () => {
    setup(Role.OWNER, [
      makeBinding('bind-a', { policyName: 'policy-a' }),
      makeBinding('bind-b', { policyName: 'policy-b' }),
    ])
    render(<OrgTemplateBindingsIndexPage orgName="test-org" />)
    expect(screen.getByText('bind-a')).toBeInTheDocument()
    expect(screen.getByText('bind-b')).toBeInTheDocument()
  })
})

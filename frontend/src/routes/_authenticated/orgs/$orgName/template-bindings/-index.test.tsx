import { render, screen, fireEvent, within } from '@testing-library/react'
import { describe, it, expect, beforeEach, vi } from 'vitest'
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
        <a href={href} data-params={JSON.stringify(params)} {...props}>
          {children}
        </a>
      )
    },
  }
})

vi.mock('@/queries/templatePolicyBindings', async () => {
  const actual = await vi.importActual<
    typeof import('@/queries/templatePolicyBindings')
  >('@/queries/templatePolicyBindings')
  return {
    ...actual,
    useListTemplatePolicyBindings: vi.fn(),
  }
})

vi.mock('@/queries/organizations', () => ({
  useGetOrganization: vi.fn(),
}))

// Namespace prefixes default to 'holos-' / 'org-' / 'fld-' / 'prj-' when
// __CONSOLE_CONFIG__ is not injected (see console-config.ts), which matches
// the fixtures below — no mock needed.

import { useListTemplatePolicyBindings } from '@/queries/templatePolicyBindings'
import { useGetOrganization } from '@/queries/organizations'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { OrgTemplateBindingsIndexPage } from './index'

function makeBinding(
  name: string,
  options: {
    namespace?: string
    description?: string
    creatorEmail?: string
    targets?: number
    policyName?: string
  } = {},
) {
  return {
    name,
    displayName: name,
    namespace: options.namespace ?? 'holos-org-test-org',
    description: options.description ?? '',
    creatorEmail: options.creatorEmail ?? '',
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
  error: Error | null = null,
) {
  ;(useListTemplatePolicyBindings as Mock).mockReturnValue({
    data: bindings,
    isPending: false,
    error,
  })
  ;(useGetOrganization as Mock).mockReturnValue({
    data: { name: 'test-org', userRole },
    isPending: false,
    error: null,
  })
}

describe('OrgTemplateBindingsIndexPage', () => {
  beforeEach(() => vi.clearAllMocks())

  it('renders empty state when no bindings exist', () => {
    setup(Role.OWNER, [])
    render(<OrgTemplateBindingsIndexPage orgName="test-org" />)
    expect(
      screen.getByText(/no template bindings yet/i),
    ).toBeInTheDocument()
  })

  it('renders the grid with org-scoped name links, scope badges, and target counts', () => {
    setup(Role.OWNER, [
      makeBinding('org-bind', {
        targets: 3,
        policyName: 'require-http',
      }),
    ])
    render(<OrgTemplateBindingsIndexPage orgName="test-org" />)

    // Name cell renders as a link to the new template-bindings route.
    const orgLink = screen.getByRole('link', { name: 'org-bind' })
    expect(orgLink.getAttribute('href')).toBe(
      '/orgs/test-org/template-bindings/org-bind',
    )

    // Scope and targets columns.
    expect(screen.getByText(/^Organization: test-org$/)).toBeInTheDocument()
    expect(screen.getByText(/3 targets/)).toBeInTheDocument()
    expect(screen.getByText('require-http')).toBeInTheDocument()
  })

  it('renders plain text (no link) for non-org-scoped rows', () => {
    setup(Role.OWNER, [
      makeBinding('fld-bind', {
        namespace: 'holos-fld-team-alpha',
        targets: 1,
        policyName: 'exclude-http',
      }),
    ])
    render(<OrgTemplateBindingsIndexPage orgName="test-org" />)

    // Folder-scoped binding should NOT produce a link (this page is org-only).
    expect(screen.queryByRole('link', { name: 'fld-bind' })).not.toBeInTheDocument()
    expect(screen.getByText('fld-bind')).toBeInTheDocument()
  })

  it('filters rows by the global search input', () => {
    setup(Role.OWNER, [
      makeBinding('alpha', { policyName: 'p1' }),
      makeBinding('beta', { policyName: 'p2' }),
    ])
    render(<OrgTemplateBindingsIndexPage orgName="test-org" />)

    const input = screen.getByLabelText('Search template bindings')
    fireEvent.change(input, { target: { value: 'alph' } })

    expect(screen.getByRole('link', { name: 'alpha' })).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'beta' })).not.toBeInTheDocument()
  })

  it('does not render a scope filter select in the toolbar', () => {
    setup(Role.OWNER, [makeBinding('org-bind', { policyName: 'p1' })])
    render(<OrgTemplateBindingsIndexPage orgName="test-org" />)
    expect(
      screen.queryByRole('combobox', { name: /filter by scope/i }),
    ).not.toBeInTheDocument()
  })

  it('lists bindings returned from the org namespace RPC call', () => {
    setup(Role.OWNER, [
      makeBinding('bind-a', { policyName: 'policy-a' }),
      makeBinding('bind-b', { policyName: 'policy-b' }),
    ])
    render(<OrgTemplateBindingsIndexPage orgName="test-org" />)
    expect(screen.getByRole('link', { name: 'bind-a' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'bind-b' })).toBeInTheDocument()
  })

  it('shows Create Binding for OWNER and EDITOR', () => {
    setup(Role.OWNER, [])
    const { unmount } = render(
      <OrgTemplateBindingsIndexPage orgName="test-org" />,
    )
    expect(
      screen.getByRole('link', { name: /create binding/i }),
    ).toBeInTheDocument()
    unmount()

    setup(Role.EDITOR, [])
    render(<OrgTemplateBindingsIndexPage orgName="test-org" />)
    expect(
      screen.getByRole('link', { name: /create binding/i }),
    ).toBeInTheDocument()
  })

  it('hides Create Binding for VIEWER', () => {
    setup(Role.VIEWER, [])
    render(<OrgTemplateBindingsIndexPage orgName="test-org" />)
    expect(
      screen.queryByRole('link', { name: /create binding/i }),
    ).not.toBeInTheDocument()
  })

  it('surfaces an error when the list query fails with no partial data', () => {
    ;(useListTemplatePolicyBindings as Mock).mockReturnValue({
      data: [],
      isPending: false,
      error: new Error('backend unreachable'),
    })
    ;(useGetOrganization as Mock).mockReturnValue({
      data: { name: 'test-org', userRole: Role.OWNER },
      isPending: false,
      error: null,
    })
    render(<OrgTemplateBindingsIndexPage orgName="test-org" />)
    expect(screen.getByText('backend unreachable')).toBeInTheDocument()
  })

  it('shows a partial-error banner when the query errors but some rows loaded', () => {
    setup(
      Role.OWNER,
      [makeBinding('org-bind', { policyName: 'p1' })],
      new Error('fetch failed'),
    )
    render(<OrgTemplateBindingsIndexPage orgName="test-org" />)
    const banner = screen.getByTestId('bindings-partial-error')
    expect(within(banner).getByText('fetch failed')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'org-bind' })).toBeInTheDocument()
  })
})

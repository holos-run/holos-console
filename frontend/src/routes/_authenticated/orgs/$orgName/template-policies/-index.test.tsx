import { render, screen } from '@testing-library/react'
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
      ...props
    }: React.AnchorHTMLAttributes<HTMLAnchorElement> & {
      children: React.ReactNode
      to?: string
      params?: Record<string, string>
    }) => (
      <a href={to} data-params={JSON.stringify(params)} {...props}>
        {children}
      </a>
    ),
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

vi.mock('@/queries/templates', async () => {
  const actual = await vi.importActual<typeof import('@/queries/templates')>('@/queries/templates')
  return {
    ...actual,
    makeOrgScope: vi.fn().mockReturnValue({ scope: 1, scopeName: 'test-org' }),
  }
})

vi.mock('@/queries/organizations', () => ({
  useGetOrganization: vi.fn(),
}))

import { useListTemplatePolicies, TemplatePolicyKind } from '@/queries/templatePolicies'
import { useGetOrganization } from '@/queries/organizations'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { OrgTemplatePoliciesIndexPage } from './index'

function makePolicy(name: string, require = 1, exclude = 0) {
  const rule = (kind: TemplatePolicyKind) => ({
    kind,
    template: { scope: 1, scopeName: 'test-org', name: 'httproute', versionConstraint: '' },
    target: { projectPattern: '*', deploymentPattern: '*' },
  })
  return {
    name,
    displayName: name,
    description: '',
    creatorEmail: '',
    rules: [
      ...Array.from({ length: require }, () => rule(TemplatePolicyKind.REQUIRE)),
      ...Array.from({ length: exclude }, () => rule(TemplatePolicyKind.EXCLUDE)),
    ],
  }
}

function setup(
  userRole: Role = Role.OWNER,
  policies: ReturnType<typeof makePolicy>[] = [],
) {
  ;(useListTemplatePolicies as Mock).mockReturnValue({
    data: policies,
    isPending: false,
    error: null,
  })
  ;(useGetOrganization as Mock).mockReturnValue({
    data: { name: 'test-org', userRole },
    isPending: false,
    error: null,
  })
}

describe('OrgTemplatePoliciesIndexPage', () => {
  beforeEach(() => vi.clearAllMocks())

  it('renders populated list with summary counts', () => {
    setup(Role.OWNER, [makePolicy('p-1', 2, 1)])
    render(<OrgTemplatePoliciesIndexPage orgName="test-org" />)
    expect(screen.getByText('p-1')).toBeInTheDocument()
    expect(screen.getByText(/REQUIRE x 2/)).toBeInTheDocument()
    expect(screen.getByText(/EXCLUDE x 1/)).toBeInTheDocument()
  })

  it('renders empty state', () => {
    setup(Role.OWNER, [])
    render(<OrgTemplatePoliciesIndexPage orgName="test-org" />)
    expect(screen.getByText(/no template policies yet/i)).toBeInTheDocument()
  })

  it('shows Create Policy only for OWNER', () => {
    setup(Role.VIEWER, [])
    const { rerender } = render(<OrgTemplatePoliciesIndexPage orgName="test-org" />)
    expect(screen.queryByRole('link', { name: /create policy/i })).not.toBeInTheDocument()

    setup(Role.OWNER, [])
    rerender(<OrgTemplatePoliciesIndexPage orgName="test-org" />)
    expect(screen.getByRole('link', { name: /create policy/i })).toBeInTheDocument()
  })
})

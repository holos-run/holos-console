import { render, screen } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => ({ orgName: 'test-org', policyName: 'policy-a' }),
    }),
    useNavigate: () => vi.fn(),
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
    useGetTemplatePolicy: vi.fn(),
    useUpdateTemplatePolicy: vi.fn(),
    useDeleteTemplatePolicy: vi.fn(),
  }
})

vi.mock('@/queries/templates', async () => {
  const actual = await vi.importActual<typeof import('@/queries/templates')>('@/queries/templates')
  return {
    ...actual,
    makeOrgScope: vi.fn().mockReturnValue({ scope: 1, scopeName: 'test-org' }),
    useListLinkableTemplates: vi.fn().mockReturnValue({
      data: [],
      isPending: false,
      error: null,
    }),
  }
})

vi.mock('@/queries/organizations', () => ({
  useGetOrganization: vi.fn(),
}))

import {
  useGetTemplatePolicy,
  useUpdateTemplatePolicy,
  useDeleteTemplatePolicy,
  TemplatePolicyKind,
} from '@/queries/templatePolicies'
import { useGetOrganization } from '@/queries/organizations'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { OrgTemplatePolicyDetailPage } from './$policyName'

function makeMockPolicy() {
  return {
    name: 'policy-a',
    displayName: 'Policy A',
    description: 'Requires HTTPRoute',
    creatorEmail: 'jane@example.com',
    rules: [
      {
        kind: TemplatePolicyKind.REQUIRE,
        template: {
          scope: 1,
          scopeName: 'test-org',
          name: 'httproute',
          versionConstraint: '',
        },
        target: { projectPattern: '*', deploymentPattern: '*' },
      },
    ],
  }
}

function setupMocks(
  userRole: Role = Role.OWNER,
  policy: ReturnType<typeof makeMockPolicy> | undefined = makeMockPolicy(),
) {
  ;(useGetTemplatePolicy as Mock).mockReturnValue({
    data: policy,
    isPending: false,
    error: null,
  })
  ;(useUpdateTemplatePolicy as Mock).mockReturnValue({
    mutateAsync: vi.fn().mockResolvedValue({}),
    isPending: false,
  })
  ;(useDeleteTemplatePolicy as Mock).mockReturnValue({
    mutateAsync: vi.fn().mockResolvedValue({}),
    isPending: false,
  })
  ;(useGetOrganization as Mock).mockReturnValue({
    data: { name: 'test-org', userRole },
    isPending: false,
    error: null,
  })
}

describe('OrgTemplatePolicyDetailPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('shows the Delete Policy button for OWNER', () => {
    setupMocks(Role.OWNER)
    render(<OrgTemplatePolicyDetailPage orgName="test-org" policyName="policy-a" />)
    expect(screen.getByRole('button', { name: /delete policy/i })).toBeInTheDocument()
  })

  it('hides the Delete Policy button for VIEWER', () => {
    setupMocks(Role.VIEWER)
    render(<OrgTemplatePolicyDetailPage orgName="test-org" policyName="policy-a" />)
    expect(screen.queryByRole('button', { name: /delete policy/i })).not.toBeInTheDocument()
  })

  // Regression test for codex review round 3: PERMISSION_TEMPLATE_POLICIES_DELETE
  // is OWNER-only in the RBAC cascade, so the Delete button must stay hidden
  // for editors even though PERMISSION_TEMPLATE_POLICIES_WRITE is granted and
  // the rest of the form is enabled.
  it('enables the form for EDITOR but hides the Delete Policy button', () => {
    setupMocks(Role.EDITOR)
    render(<OrgTemplatePolicyDetailPage orgName="test-org" policyName="policy-a" />)
    expect(screen.queryByRole('button', { name: /delete policy/i })).not.toBeInTheDocument()
    expect(screen.getByLabelText(/display name/i)).not.toBeDisabled()
    expect(screen.getByRole('button', { name: /^save$/i })).not.toBeDisabled()
  })
})

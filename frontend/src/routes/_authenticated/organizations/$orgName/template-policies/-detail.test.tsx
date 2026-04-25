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

vi.mock('@/queries/templatePolicyBindings', async () => {
  const actual = await vi.importActual<
    typeof import('@/queries/templatePolicyBindings')
  >('@/queries/templatePolicyBindings')
  return {
    ...actual,
    useListTemplatePolicyBindings: vi.fn(),
  }
})

import {
  useGetTemplatePolicy,
  useUpdateTemplatePolicy,
  useDeleteTemplatePolicy,
  TemplatePolicyKind,
} from '@/queries/templatePolicies'
import { useListTemplatePolicyBindings } from '@/queries/templatePolicyBindings'
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
  bindings: Array<{
    name: string
    displayName?: string
    description?: string
    policyRef?: { name: string; namespace?: string }
    targetRefs?: Array<{ kind?: number; name: string; projectName?: string }>
  }> = [],
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
  ;(useListTemplatePolicyBindings as Mock).mockReturnValue({
    data: bindings,
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

  // HOL-598: the Policy detail page must surface the bindings that attach
  // this policy to specific render targets. The section is implemented via
  // useListTemplatePolicyBindings(scope) + client-side filter on
  // policyRef.name === policyName.
  describe('Bindings section (HOL-598)', () => {
    it('renders a Bindings heading and an empty-state message when no bindings reference the policy', () => {
      setupMocks(Role.OWNER, makeMockPolicy(), [])
      render(<OrgTemplatePolicyDetailPage orgName="test-org" policyName="policy-a" />)
      expect(
        screen.getByRole('heading', { name: /^bindings$/i }),
      ).toBeInTheDocument()
      expect(screen.getByTestId('policy-bindings-empty')).toBeInTheDocument()
    })

    it('lists only bindings whose policyRef.name matches this policy', () => {
      const bindings = [
        {
          name: 'binding-for-policy-a',
          displayName: 'Binding for policy-a',
          description: 'attaches policy-a to api deployments',
          policyRef: {
            name: 'policy-a',
            namespace: "holos-org-test-org",
          },
          targetRefs: [
            { kind: 2, name: 'api', projectName: 'frontend' },
            { kind: 2, name: 'worker', projectName: 'frontend' },
          ],
        },
        {
          name: 'binding-for-policy-b',
          displayName: 'Unrelated binding',
          policyRef: {
            name: 'policy-b',
            namespace: "holos-org-test-org",
          },
          targetRefs: [{ kind: 2, name: 'api', projectName: 'frontend' }],
        },
      ]
      setupMocks(Role.OWNER, makeMockPolicy(), bindings)
      render(<OrgTemplatePolicyDetailPage orgName="test-org" policyName="policy-a" />)

      const list = screen.getByTestId('policy-bindings-list')
      expect(list).toBeInTheDocument()
      expect(list).toHaveTextContent('binding-for-policy-a')
      expect(list).not.toHaveTextContent('binding-for-policy-b')
    })

    it('links each row to the binding detail route', () => {
      const bindings = [
        {
          name: 'binding-for-policy-a',
          policyRef: {
            name: 'policy-a',
            namespace: "holos-org-test-org",
          },
          targetRefs: [] as Array<{ kind?: number; name: string; projectName?: string }>,
        },
      ]
      setupMocks(Role.OWNER, makeMockPolicy(), bindings)
      render(<OrgTemplatePolicyDetailPage orgName="test-org" policyName="policy-a" />)

      const link = screen.getByRole('link', { name: /binding-for-policy-a/i })
      // The Link mock at the top of this file exposes the `to` template as
      // href, so we can assert the route shape without running the router.
      expect(link).toHaveAttribute(
        'href',
        '/organizations/$orgName/template-bindings/$bindingName',
      )
      const params = JSON.parse(link.getAttribute('data-params') ?? '{}')
      expect(params).toEqual({
        orgName: 'test-org',
        bindingName: 'binding-for-policy-a',
      })
    })
  })
})

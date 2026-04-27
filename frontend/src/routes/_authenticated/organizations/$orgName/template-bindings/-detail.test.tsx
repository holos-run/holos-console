import { render, screen } from '@testing-library/react'
import { describe, it, expect, beforeEach, vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => ({ orgName: 'test-org', bindingName: 'bind-a' }),
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

vi.mock('@/queries/templatePolicyBindings', async () => {
  const actual = await vi.importActual<
    typeof import('@/queries/templatePolicyBindings')
  >('@/queries/templatePolicyBindings')
  return {
    ...actual,
    useGetTemplatePolicyBinding: vi.fn(),
    useUpdateTemplatePolicyBinding: vi.fn(),
    useDeleteTemplatePolicyBinding: vi.fn(),
  }
})

vi.mock('@/queries/templates', async () => {
  const actual = await vi.importActual<typeof import('@/queries/templates')>(
    '@/queries/templates',
  )
  return {
    ...actual,
    useListTemplates: vi.fn().mockReturnValue({
      data: [],
      isPending: false,
      error: null,
    }),
  }
})

vi.mock('@/queries/templatePolicies', () => ({
  useListTemplatePolicies: vi.fn().mockReturnValue({
    data: [],
    isPending: false,
    error: null,
  }),
}))

vi.mock('@/queries/projects', () => ({
  useListProjects: vi.fn().mockReturnValue({
    data: { projects: [] },
    isPending: false,
    isLoading: false,
    error: null,
  }),
  useListProjectsByParent: vi.fn().mockReturnValue({
    data: [],
    isPending: false,
    isLoading: false,
    error: null,
  }),
}))

vi.mock('@/queries/deployments', () => ({
  useListDeployments: vi.fn().mockReturnValue({
    data: [],
    isPending: false,
    error: null,
  }),
}))

vi.mock('@/queries/organizations', () => ({
  useGetOrganization: vi.fn(),
}))

import {
  useGetTemplatePolicyBinding,
  useUpdateTemplatePolicyBinding,
  useDeleteTemplatePolicyBinding,
  TemplatePolicyBindingTargetKind,
} from '@/queries/templatePolicyBindings'
import { useGetOrganization } from '@/queries/organizations'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { mockResourcePermissionsForRole } from '@/test/resource-permissions'
import { OrgTemplateBindingDetailPage } from './$bindingName'

function makeBinding() {
  return {
    name: 'bind-a',
    displayName: 'Bind A',
    description: 'Attach require-http to ingress',
    creatorEmail: 'jane@example.com',
    policyRef: {
      namespace: 'holos-org-test-org',
      name: 'require-http',
    },
    targetRefs: [
      {
        kind: TemplatePolicyBindingTargetKind.PROJECT_TEMPLATE,
        name: 'ingress',
        projectName: 'proj-a',
      },
    ],
  }
}

function setupMocks(
  userRole: Role = Role.OWNER,
  binding: ReturnType<typeof makeBinding> | undefined = makeBinding(),
) {
  mockResourcePermissionsForRole(userRole)
  ;(useGetTemplatePolicyBinding as Mock).mockReturnValue({
    data: binding,
    isPending: false,
    error: null,
  })
  ;(useUpdateTemplatePolicyBinding as Mock).mockReturnValue({
    mutateAsync: vi.fn().mockResolvedValue({}),
    isPending: false,
  })
  ;(useDeleteTemplatePolicyBinding as Mock).mockReturnValue({
    mutateAsync: vi.fn().mockResolvedValue({}),
    isPending: false,
  })
  ;(useGetOrganization as Mock).mockReturnValue({
    data: { name: 'test-org', userRole },
    isPending: false,
    error: null,
  })
}

describe('OrgTemplateBindingDetailPage', () => {
  beforeEach(() => vi.clearAllMocks())

  it('shows the Delete Binding button for OWNER', () => {
    setupMocks(Role.OWNER)
    render(
      <OrgTemplateBindingDetailPage
        orgName="test-org"
        bindingName="bind-a"
      />,
    )
    expect(
      screen.getByRole('button', { name: /delete binding/i }),
    ).toBeInTheDocument()
  })

  it('hides the Delete Binding button for EDITOR (DELETE is OWNER-only)', () => {
    setupMocks(Role.EDITOR)
    render(
      <OrgTemplateBindingDetailPage
        orgName="test-org"
        bindingName="bind-a"
      />,
    )
    expect(
      screen.queryByRole('button', { name: /delete binding/i }),
    ).not.toBeInTheDocument()
    // but form controls should be enabled
    expect(screen.getByLabelText(/display name/i)).not.toBeDisabled()
    expect(screen.getByRole('button', { name: /^save$/i })).not.toBeDisabled()
  })

  it('hides the Delete Binding button for VIEWER', () => {
    setupMocks(Role.VIEWER)
    render(
      <OrgTemplateBindingDetailPage
        orgName="test-org"
        bindingName="bind-a"
      />,
    )
    expect(
      screen.queryByRole('button', { name: /delete binding/i }),
    ).not.toBeInTheDocument()
  })

  it('seeds the form with initial values from the proto binding', () => {
    setupMocks(Role.OWNER)
    render(
      <OrgTemplateBindingDetailPage
        orgName="test-org"
        bindingName="bind-a"
      />,
    )
    expect(screen.getByLabelText(/display name/i)).toHaveValue('Bind A')
    expect(screen.getByLabelText(/^description$/i)).toHaveValue(
      'Attach require-http to ingress',
    )
    // Name slug should be locked in edit mode.
    expect(screen.getByLabelText(/name slug/i)).toBeDisabled()
  })
})

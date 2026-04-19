import { render, screen } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => ({ folderName: 'test-folder', policyName: 'policy-a' }),
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
    makeFolderScope: vi.fn().mockReturnValue({ scope: 2, scopeName: 'test-folder' }),
    useListLinkableTemplates: vi.fn().mockReturnValue({
      data: [],
      isPending: false,
      error: null,
    }),
  }
})

vi.mock('@/queries/folders', () => ({
  useGetFolder: vi.fn(),
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
import { useGetFolder } from '@/queries/folders'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { FolderTemplatePolicyDetailPage } from './$policyName'

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
  ;(useGetFolder as Mock).mockReturnValue({
    data: { name: 'test-folder', organization: 'test-org', userRole },
    isPending: false,
    error: null,
  })
  ;(useListTemplatePolicyBindings as Mock).mockReturnValue({
    data: bindings,
    isPending: false,
    error: null,
  })
}

describe('FolderTemplatePolicyDetailPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders the policy display name and locks the name slug', () => {
    setupMocks()
    render(<FolderTemplatePolicyDetailPage folderName="test-folder" policyName="policy-a" />)
    expect(screen.getAllByText('Policy A').length).toBeGreaterThan(0)
    const slugInput = screen.getByLabelText(/name slug/i) as HTMLInputElement
    expect(slugInput).toBeDisabled()
    expect(slugInput.value).toBe('policy-a')
  })

  it('shows the Delete Policy button for OWNER and hides it for VIEWER', () => {
    setupMocks(Role.OWNER)
    const { rerender } = render(
      <FolderTemplatePolicyDetailPage folderName="test-folder" policyName="policy-a" />,
    )
    expect(screen.getByRole('button', { name: /delete policy/i })).toBeInTheDocument()

    setupMocks(Role.VIEWER)
    rerender(<FolderTemplatePolicyDetailPage folderName="test-folder" policyName="policy-a" />)
    expect(screen.queryByRole('button', { name: /delete policy/i })).not.toBeInTheDocument()
  })

  it('pre-populates one rule row per existing rule', () => {
    setupMocks()
    render(<FolderTemplatePolicyDetailPage folderName="test-folder" policyName="policy-a" />)
    expect(screen.getByTestId('rule-editor-row-0')).toBeInTheDocument()
  })

  // Regression test for codex review round 1: editors are granted
  // PERMISSION_TEMPLATE_POLICIES_WRITE by the cascade table. The detail page
  // previously gated the whole form on Role.OWNER, which incorrectly disabled
  // editing for editors.
  //
  // Round 3 refinement: PERMISSION_TEMPLATE_POLICIES_DELETE is OWNER-only in
  // the RBAC cascade, so the Delete button must stay hidden for editors even
  // though the rest of the form is enabled.
  it('enables the form for EDITOR but hides the Delete Policy button', () => {
    setupMocks(Role.EDITOR)
    render(<FolderTemplatePolicyDetailPage folderName="test-folder" policyName="policy-a" />)
    expect(screen.queryByRole('button', { name: /delete policy/i })).not.toBeInTheDocument()
    expect(screen.getByLabelText(/display name/i)).not.toBeDisabled()
    expect(screen.getByRole('button', { name: /^save$/i })).not.toBeDisabled()
  })

  // HOL-598: folder-scope policies must surface matching folder-scope bindings
  // via useListTemplatePolicyBindings(folderScope) + client-side name filter.
  describe('Bindings section (HOL-598)', () => {
    it('renders a Bindings heading and an empty-state message when no bindings reference the policy', () => {
      setupMocks(Role.OWNER, makeMockPolicy(), [])
      render(
        <FolderTemplatePolicyDetailPage folderName="test-folder" policyName="policy-a" />,
      )
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
          policyRef: {
            name: 'policy-a',
            namespace: "holos-fld-test-folder",
          },
          targetRefs: [{ kind: 1, name: 'project-template', projectName: 'frontend' }],
        },
        {
          name: 'binding-for-other',
          policyRef: {
            name: 'other-policy',
            namespace: "holos-fld-test-folder",
          },
          targetRefs: [],
        },
      ]
      setupMocks(Role.OWNER, makeMockPolicy(), bindings)
      render(
        <FolderTemplatePolicyDetailPage folderName="test-folder" policyName="policy-a" />,
      )

      const list = screen.getByTestId('policy-bindings-list')
      expect(list).toBeInTheDocument()
      expect(list).toHaveTextContent('binding-for-policy-a')
      expect(list).not.toHaveTextContent('binding-for-other')
    })

    it('links each row to the folder binding detail route', () => {
      const bindings = [
        {
          name: 'binding-for-policy-a',
          policyRef: {
            name: 'policy-a',
            namespace: "holos-fld-test-folder",
          },
          targetRefs: [] as Array<{ kind?: number; name: string; projectName?: string }>,
        },
      ]
      setupMocks(Role.OWNER, makeMockPolicy(), bindings)
      render(
        <FolderTemplatePolicyDetailPage folderName="test-folder" policyName="policy-a" />,
      )

      const link = screen.getByRole('link', { name: /binding-for-policy-a/i })
      expect(link).toHaveAttribute(
        'href',
        '/folders/$folderName/template-policy-bindings/$bindingName',
      )
      const params = JSON.parse(link.getAttribute('data-params') ?? '{}')
      expect(params).toEqual({
        folderName: 'test-folder',
        bindingName: 'binding-for-policy-a',
      })
    })
  })
})

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

import {
  useGetTemplatePolicy,
  useUpdateTemplatePolicy,
  useDeleteTemplatePolicy,
  TemplatePolicyKind,
} from '@/queries/templatePolicies'
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
  // previously gated on Role.OWNER, which incorrectly hid the Delete Policy
  // button and disabled the form for editors.
  it('shows the Delete Policy button and enables the form for EDITOR', () => {
    setupMocks(Role.EDITOR)
    render(<FolderTemplatePolicyDetailPage folderName="test-folder" policyName="policy-a" />)
    expect(screen.getByRole('button', { name: /delete policy/i })).toBeInTheDocument()
    expect(screen.getByLabelText(/display name/i)).not.toBeDisabled()
    expect(screen.getByRole('button', { name: /^save$/i })).not.toBeDisabled()
  })
})

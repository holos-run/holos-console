import { render, screen } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => ({ folderName: 'test-folder' }),
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
    makeFolderScope: vi.fn().mockReturnValue({ scope: 2, scopeName: 'test-folder' }),
  }
})

vi.mock('@/queries/folders', () => ({
  useGetFolder: vi.fn(),
}))

import { useListTemplatePolicies, TemplatePolicyKind } from '@/queries/templatePolicies'
import { useGetFolder } from '@/queries/folders'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { FolderTemplatePoliciesIndexPage } from './index'

function makeRule(kind: TemplatePolicyKind) {
  return {
    kind,
    template: { scope: 1, scopeName: 'test-org', name: 'http', versionConstraint: '' },
    target: { projectPattern: '*', deploymentPattern: '*' },
  }
}

function setupMocks(
  userRole: Role = Role.OWNER,
  policies: ReturnType<typeof makePolicy>[] | undefined = [],
) {
  ;(useListTemplatePolicies as Mock).mockReturnValue({
    data: policies,
    isPending: false,
    error: null,
  })
  ;(useGetFolder as Mock).mockReturnValue({
    data: { name: 'test-folder', organization: 'test-org', userRole },
    isPending: false,
    error: null,
  })
}

function makePolicy(
  name: string,
  options: {
    description?: string
    creatorEmail?: string
    require?: number
    exclude?: number
  } = {},
) {
  const rules = [
    ...Array.from({ length: options.require ?? 0 }, () => makeRule(TemplatePolicyKind.REQUIRE)),
    ...Array.from({ length: options.exclude ?? 0 }, () => makeRule(TemplatePolicyKind.EXCLUDE)),
  ]
  return {
    name,
    displayName: name,
    description: options.description ?? '',
    creatorEmail: options.creatorEmail ?? '',
    rules,
  }
}

describe('FolderTemplatePoliciesIndexPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders the empty state when no policies exist and explains dual project/deployment scope', () => {
    setupMocks(Role.OWNER, [])
    render(<FolderTemplatePoliciesIndexPage folderName="test-folder" />)
    expect(screen.getByText(/no template policies yet/i)).toBeInTheDocument()
    expect(
      screen.getAllByText(/apply to .*both project templates and deployments/i, { exact: false }).length,
    ).toBeGreaterThan(0)
  })

  it('renders a populated list with REQUIRE/EXCLUDE summary counts', () => {
    setupMocks(Role.OWNER, [
      makePolicy('policy-a', { require: 2, exclude: 1, creatorEmail: 'jane@example.com' }),
      makePolicy('policy-b', { require: 0, exclude: 3 }),
    ])
    render(<FolderTemplatePoliciesIndexPage folderName="test-folder" />)

    expect(screen.getByText('policy-a')).toBeInTheDocument()
    expect(screen.getByText('policy-b')).toBeInTheDocument()
    expect(screen.getByText(/REQUIRE x 2/)).toBeInTheDocument()
    expect(screen.getByText(/EXCLUDE x 1/)).toBeInTheDocument()
    expect(screen.getByText(/EXCLUDE x 3/)).toBeInTheDocument()
    expect(screen.getByText(/Created by jane@example.com/)).toBeInTheDocument()
  })

  it('shows Create Policy link for OWNER', () => {
    setupMocks(Role.OWNER, [])
    render(<FolderTemplatePoliciesIndexPage folderName="test-folder" />)
    const link = screen.getByRole('link', { name: /create policy/i })
    expect(link).toBeInTheDocument()
    expect(link).toHaveAttribute('href', '/folders/$folderName/template-policies/new')
  })

  it('hides Create Policy link for VIEWER (read-only)', () => {
    setupMocks(Role.VIEWER, [makePolicy('policy-a', { require: 1 })])
    render(<FolderTemplatePoliciesIndexPage folderName="test-folder" />)
    expect(screen.queryByRole('link', { name: /create policy/i })).not.toBeInTheDocument()
  })

  it('shows Create Policy link for EDITOR (PERMISSION_TEMPLATE_POLICIES_WRITE cascades to editors)', () => {
    // Regression test for codex review round 1: previously the UI gated on
    // Role.OWNER only, but the backend grants PERMISSION_TEMPLATE_POLICIES_WRITE
    // to both OWNER and EDITOR. Editors must not see a read-only UI for flows
    // they are authorized to perform.
    setupMocks(Role.EDITOR, [])
    render(<FolderTemplatePoliciesIndexPage folderName="test-folder" />)
    const link = screen.getByRole('link', { name: /create policy/i })
    expect(link).toBeInTheDocument()
    expect(link).toHaveAttribute('href', '/folders/$folderName/template-policies/new')
  })

  it('renders skeleton while loading', () => {
    ;(useListTemplatePolicies as Mock).mockReturnValue({
      data: undefined,
      isPending: true,
      error: null,
    })
    ;(useGetFolder as Mock).mockReturnValue({
      data: { name: 'test-folder', organization: 'test-org', userRole: Role.OWNER },
      isPending: false,
      error: null,
    })
    render(<FolderTemplatePoliciesIndexPage folderName="test-folder" />)
    expect(screen.queryByTestId('policies-list')).not.toBeInTheDocument()
  })

  it('surfaces an error when the list query fails', () => {
    ;(useListTemplatePolicies as Mock).mockReturnValue({
      data: undefined,
      isPending: false,
      error: new Error('backend unreachable'),
    })
    ;(useGetFolder as Mock).mockReturnValue({
      data: { name: 'test-folder', organization: 'test-org', userRole: Role.OWNER },
      isPending: false,
      error: null,
    })
    render(<FolderTemplatePoliciesIndexPage folderName="test-folder" />)
    expect(screen.getByText('backend unreachable')).toBeInTheDocument()
  })
})

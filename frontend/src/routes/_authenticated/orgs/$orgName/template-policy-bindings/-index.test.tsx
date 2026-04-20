import { render, screen } from '@testing-library/react'
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
    useListTemplatePolicyBindings: vi.fn(),
  }
})

vi.mock('@/queries/organizations', () => ({
  useGetOrganization: vi.fn(),
}))

import { useListTemplatePolicyBindings } from '@/queries/templatePolicyBindings'
import { useGetOrganization } from '@/queries/organizations'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { OrgTemplatePolicyBindingsIndexPage } from './index'

function makeBinding(
  name: string,
  options: {
    description?: string
    creatorEmail?: string
    targets?: number
    policyName?: string
  } = {},
) {
  return {
    name,
    displayName: name,
    description: options.description ?? '',
    creatorEmail: options.creatorEmail ?? '',
    policyRef: options.policyName
      ? { namespace: "holos-org-test-org", name: options.policyName }
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
) {
  ;(useListTemplatePolicyBindings as Mock).mockReturnValue({
    data: bindings,
    isPending: false,
    error: null,
  })
  ;(useGetOrganization as Mock).mockReturnValue({
    data: { name: 'test-org', userRole },
    isPending: false,
    error: null,
  })
}

describe('OrgTemplatePolicyBindingsIndexPage', () => {
  beforeEach(() => vi.clearAllMocks())

  it('renders empty state when no bindings exist', () => {
    setup(Role.OWNER, [])
    render(<OrgTemplatePolicyBindingsIndexPage orgName="test-org" />)
    expect(
      screen.getByText(/no template policy bindings yet/i),
    ).toBeInTheDocument()
  })

  it('renders populated list with target-count and policy badges', () => {
    setup(Role.OWNER, [
      makeBinding('bind-a', {
        targets: 3,
        policyName: 'require-http',
        creatorEmail: 'jane@example.com',
      }),
      makeBinding('bind-b', { targets: 1, policyName: 'exclude-http' }),
    ])
    render(<OrgTemplatePolicyBindingsIndexPage orgName="test-org" />)

    expect(screen.getByText('bind-a')).toBeInTheDocument()
    expect(screen.getByText('bind-b')).toBeInTheDocument()
    expect(screen.getByText(/3 targets/)).toBeInTheDocument()
    expect(screen.getByText(/1 target\b/)).toBeInTheDocument()
    expect(screen.getByText(/policy: require-http/)).toBeInTheDocument()
    expect(screen.getByText(/policy: exclude-http/)).toBeInTheDocument()
    expect(screen.getByText(/Created by jane@example.com/)).toBeInTheDocument()
  })

  it('shows Create Binding for OWNER and EDITOR', () => {
    setup(Role.OWNER, [])
    const { unmount } = render(
      <OrgTemplatePolicyBindingsIndexPage orgName="test-org" />,
    )
    expect(
      screen.getByRole('link', { name: /create binding/i }),
    ).toBeInTheDocument()
    unmount()

    setup(Role.EDITOR, [])
    render(<OrgTemplatePolicyBindingsIndexPage orgName="test-org" />)
    expect(
      screen.getByRole('link', { name: /create binding/i }),
    ).toBeInTheDocument()
  })

  it('hides Create Binding for VIEWER', () => {
    setup(Role.VIEWER, [])
    render(<OrgTemplatePolicyBindingsIndexPage orgName="test-org" />)
    expect(
      screen.queryByRole('link', { name: /create binding/i }),
    ).not.toBeInTheDocument()
  })

  it('surfaces an error when the list query fails', () => {
    ;(useListTemplatePolicyBindings as Mock).mockReturnValue({
      data: undefined,
      isPending: false,
      error: new Error('backend unreachable'),
    })
    ;(useGetOrganization as Mock).mockReturnValue({
      data: { name: 'test-org', userRole: Role.OWNER },
      isPending: false,
      error: null,
    })
    render(<OrgTemplatePolicyBindingsIndexPage orgName="test-org" />)
    expect(screen.getByText('backend unreachable')).toBeInTheDocument()
  })
})

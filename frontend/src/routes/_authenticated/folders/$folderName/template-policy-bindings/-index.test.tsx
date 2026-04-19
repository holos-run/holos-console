import { render, screen } from '@testing-library/react'
import { describe, it, expect, beforeEach, vi } from 'vitest'
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

vi.mock('@/queries/templatePolicyBindings', async () => {
  const actual = await vi.importActual<
    typeof import('@/queries/templatePolicyBindings')
  >('@/queries/templatePolicyBindings')
  return {
    ...actual,
    useListTemplatePolicyBindings: vi.fn(),
  }
})

vi.mock('@/queries/templates', async () => {
  const actual = await vi.importActual<typeof import('@/queries/templates')>(
    '@/queries/templates',
  )
  return {
    ...actual,
    makeFolderScope: vi
      .fn()
      .mockReturnValue({ scope: 2, scopeName: 'test-folder' }),
  }
})

vi.mock('@/queries/folders', () => ({
  useGetFolder: vi.fn(),
}))

import { useListTemplatePolicyBindings } from '@/queries/templatePolicyBindings'
import { useGetFolder } from '@/queries/folders'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { FolderTemplatePolicyBindingsIndexPage } from './index'

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
      ? {
          namespace: "holos-fld-test-folder",
          name: options.policyName,
        }
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
  ;(useGetFolder as Mock).mockReturnValue({
    data: { name: 'test-folder', organization: 'test-org', userRole },
    isPending: false,
    error: null,
  })
}

describe('FolderTemplatePolicyBindingsIndexPage', () => {
  beforeEach(() => vi.clearAllMocks())

  it('renders empty state when no bindings exist', () => {
    setup(Role.OWNER, [])
    render(<FolderTemplatePolicyBindingsIndexPage folderName="test-folder" />)
    expect(
      screen.getByText(/no template policy bindings yet/i),
    ).toBeInTheDocument()
  })

  it('renders populated list with target-count and policy badges', () => {
    setup(Role.OWNER, [
      makeBinding('bind-a', { targets: 2, policyName: 'require-http' }),
    ])
    render(<FolderTemplatePolicyBindingsIndexPage folderName="test-folder" />)
    expect(screen.getByText('bind-a')).toBeInTheDocument()
    expect(screen.getByText(/2 targets/)).toBeInTheDocument()
    expect(screen.getByText(/policy: require-http/)).toBeInTheDocument()
  })

  it('shows Create Binding for EDITOR (cascade covers PERMISSION_TEMPLATE_POLICIES_WRITE)', () => {
    setup(Role.EDITOR, [])
    render(<FolderTemplatePolicyBindingsIndexPage folderName="test-folder" />)
    const link = screen.getByRole('link', { name: /create binding/i })
    expect(link).toHaveAttribute(
      'href',
      '/folders/$folderName/template-policy-bindings/new',
    )
  })

  it('hides Create Binding for VIEWER', () => {
    setup(Role.VIEWER, [])
    render(<FolderTemplatePolicyBindingsIndexPage folderName="test-folder" />)
    expect(
      screen.queryByRole('link', { name: /create binding/i }),
    ).not.toBeInTheDocument()
  })
})

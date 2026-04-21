import { render, screen } from '@testing-library/react'
import { describe, it, expect, beforeEach, vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => ({ folderName: 'test-folder', bindingName: 'bind-a' }),
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

vi.mock('@/queries/folders', () => ({
  useGetFolder: vi.fn(),
}))

import {
  useGetTemplatePolicyBinding,
  useUpdateTemplatePolicyBinding,
  useDeleteTemplatePolicyBinding,
  TemplatePolicyBindingTargetKind,
} from '@/queries/templatePolicyBindings'
import { useGetFolder } from '@/queries/folders'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { FolderTemplatePolicyBindingDetailPage } from './$bindingName'

function makeBinding() {
  return {
    name: 'bind-a',
    displayName: 'Folder Bind A',
    description: 'Folder-level binding',
    creatorEmail: 'jane@example.com',
    policyRef: {
      namespace: "holos-fld-test-folder",
      name: 'folder-policy',
    },
    targetRefs: [
      {
        kind: TemplatePolicyBindingTargetKind.DEPLOYMENT,
        name: 'web',
        projectName: 'proj-a',
      },
    ],
  }
}

function setupMocks(userRole: Role = Role.OWNER) {
  ;(useGetTemplatePolicyBinding as Mock).mockReturnValue({
    data: makeBinding(),
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
  ;(useGetFolder as Mock).mockReturnValue({
    data: { name: 'test-folder', organization: 'test-org', userRole },
    isPending: false,
    error: null,
  })
}

describe('FolderTemplatePolicyBindingDetailPage', () => {
  beforeEach(() => vi.clearAllMocks())

  it('shows the Delete Binding button for OWNER', () => {
    setupMocks(Role.OWNER)
    render(
      <FolderTemplatePolicyBindingDetailPage
        folderName="test-folder"
        bindingName="bind-a"
      />,
    )
    expect(
      screen.getByRole('button', { name: /delete binding/i }),
    ).toBeInTheDocument()
  })

  it('hides the Delete Binding button for EDITOR', () => {
    setupMocks(Role.EDITOR)
    render(
      <FolderTemplatePolicyBindingDetailPage
        folderName="test-folder"
        bindingName="bind-a"
      />,
    )
    expect(
      screen.queryByRole('button', { name: /delete binding/i }),
    ).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: /^save$/i })).not.toBeDisabled()
  })

  it('hides the Delete Binding button for VIEWER', () => {
    setupMocks(Role.VIEWER)
    render(
      <FolderTemplatePolicyBindingDetailPage
        folderName="test-folder"
        bindingName="bind-a"
      />,
    )
    expect(
      screen.queryByRole('button', { name: /delete binding/i }),
    ).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: /^save$/i })).toBeDisabled()
  })

  it('seeds the form with initial values from the proto binding', () => {
    setupMocks(Role.OWNER)
    render(
      <FolderTemplatePolicyBindingDetailPage
        folderName="test-folder"
        bindingName="bind-a"
      />,
    )
    expect(screen.getByLabelText(/display name/i)).toHaveValue('Folder Bind A')
    expect(screen.getByLabelText(/^description$/i)).toHaveValue(
      'Folder-level binding',
    )
    expect(screen.getByLabelText(/name slug/i)).toBeDisabled()
  })
})

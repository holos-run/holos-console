// HOL-1021: Tests for the org-scoped template-requirement detail/edit page.
//
// Tests cover:
// 1. Shows Delete button for OWNER.
// 2. Hides Delete button for VIEWER.
// 3. Hides Delete button for EDITOR.
// 4. Delete dialog invokes the delete mutation on confirm.
// 5. Renders with prefilled values in edit mode.

import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => ({ orgName: 'test-org', requirementName: 'req-a' }),
      useSearch: () => ({}),
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

vi.mock('@/lib/console-config', () => ({
  getConsoleConfig: vi.fn().mockReturnValue({
    namespacePrefix: '',
    organizationPrefix: 'org-',
    folderPrefix: 'folder-',
    projectPrefix: 'project-',
  }),
}))

vi.mock('@/queries/templateRequirements', async () => {
  const actual = await vi.importActual<typeof import('@/queries/templateRequirements')>(
    '@/queries/templateRequirements',
  )
  return {
    ...actual,
    useGetTemplateRequirement: vi.fn(),
    useUpdateTemplateRequirement: vi.fn(),
    useDeleteTemplateRequirement: vi.fn(),
  }
})

vi.mock('@/queries/organizations', () => ({
  useGetOrganization: vi.fn(),
}))

import {
  useGetTemplateRequirement,
  useUpdateTemplateRequirement,
  useDeleteTemplateRequirement,
} from '@/queries/templateRequirements'
import { useGetOrganization } from '@/queries/organizations'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { mockResourcePermissionsForRole } from '@/test/resource-permissions'
import { OrgTemplateRequirementDetailPage } from './$requirementName'

function makeMockRequirement() {
  return {
    name: 'req-a',
    namespace: 'holos-org-test-org',
    requires: {
      $typeName: 'holos.console.v1.LinkedTemplateRef' as const,
      namespace: 'holos-org-my-org',
      name: 'base-template',
      versionConstraint: '',
    },
    targetRefs: [],
    cascadeDelete: true,
    creatorEmail: 'test@example.com',
    createdAt: undefined,
    status: undefined,
  }
}

function setupMocks(
  userRole: Role = Role.OWNER,
  requirement: ReturnType<typeof makeMockRequirement> | undefined = makeMockRequirement(),
) {
  mockResourcePermissionsForRole(userRole)
  ;(useGetTemplateRequirement as Mock).mockReturnValue({
    data: requirement,
    isPending: false,
    error: null,
  })
  ;(useUpdateTemplateRequirement as Mock).mockReturnValue({
    mutateAsync: vi.fn().mockResolvedValue({}),
    isPending: false,
  })
  ;(useDeleteTemplateRequirement as Mock).mockReturnValue({
    mutateAsync: vi.fn().mockResolvedValue({}),
    isPending: false,
  })
  ;(useGetOrganization as Mock).mockReturnValue({
    data: { name: 'test-org', userRole },
    isPending: false,
    error: null,
  })
}

describe('OrgTemplateRequirementDetailPage (HOL-1021)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('shows the Delete Requirement button for OWNER', () => {
    setupMocks(Role.OWNER)
    render(
      <OrgTemplateRequirementDetailPage
        orgName="test-org"
        requirementName="req-a"
        namespaceOverride="holos-org-test-org"
      />,
    )
    expect(
      screen.getByRole('button', { name: /delete requirement/i }),
    ).toBeInTheDocument()
  })

  it('hides the Delete Requirement button for VIEWER', () => {
    setupMocks(Role.VIEWER)
    render(
      <OrgTemplateRequirementDetailPage
        orgName="test-org"
        requirementName="req-a"
        namespaceOverride="holos-org-test-org"
      />,
    )
    expect(
      screen.queryByRole('button', { name: /delete requirement/i }),
    ).not.toBeInTheDocument()
  })

  it('hides the Delete Requirement button for EDITOR (OWNER-only delete)', () => {
    setupMocks(Role.EDITOR)
    render(
      <OrgTemplateRequirementDetailPage
        orgName="test-org"
        requirementName="req-a"
        namespaceOverride="holos-org-test-org"
      />,
    )
    expect(
      screen.queryByRole('button', { name: /delete requirement/i }),
    ).not.toBeInTheDocument()
    // But the form controls should still be enabled for editors.
    expect(screen.getByRole('button', { name: /^save$/i })).not.toBeDisabled()
  })

  it('renders with prefilled values from the requirement', () => {
    setupMocks(Role.OWNER)
    render(
      <OrgTemplateRequirementDetailPage
        orgName="test-org"
        requirementName="req-a"
        namespaceOverride="holos-org-test-org"
      />,
    )
    expect(screen.getByLabelText('Requirement name')).toHaveValue('req-a')
    expect(screen.getByLabelText('Requires name')).toHaveValue('base-template')
  })

  it('opens the confirm delete dialog when Delete Requirement is clicked', async () => {
    setupMocks(Role.OWNER)
    render(
      <OrgTemplateRequirementDetailPage
        orgName="test-org"
        requirementName="req-a"
        namespaceOverride="holos-org-test-org"
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: /delete requirement/i }))
    await waitFor(() => {
      expect(screen.getByText(/delete resource/i)).toBeInTheDocument()
    })
  })

  it('invokes the delete mutation on confirm', async () => {
    const deleteMutateAsync = vi.fn().mockResolvedValue({})
    setupMocks(Role.OWNER)
    ;(useDeleteTemplateRequirement as Mock).mockReturnValue({
      mutateAsync: deleteMutateAsync,
      isPending: false,
    })
    render(
      <OrgTemplateRequirementDetailPage
        orgName="test-org"
        requirementName="req-a"
        namespaceOverride="holos-org-test-org"
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: /delete requirement/i }))
    await waitFor(() => {
      expect(screen.getByText(/delete resource/i)).toBeInTheDocument()
    })
    const deleteButtons = screen.getAllByRole('button', { name: /^delete$/i })
    fireEvent.click(deleteButtons[deleteButtons.length - 1])
    await waitFor(() => {
      expect(deleteMutateAsync).toHaveBeenCalledWith({ name: 'req-a' })
    })
  })
})

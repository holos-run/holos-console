// HOL-1022: Tests for the org-scoped template-grant detail/edit page.
//
// Tests cover:
// 1. Shows Delete button for OWNER.
// 2. Hides Delete button for VIEWER.
// 3. Hides Delete button for EDITOR (OWNER-only delete).
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
      useParams: () => ({ orgName: 'test-org', grantName: 'allow-project-foo' }),
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

vi.mock('@/queries/templateGrants', async () => {
  const actual = await vi.importActual<typeof import('@/queries/templateGrants')>(
    '@/queries/templateGrants',
  )
  return {
    ...actual,
    useGetTemplateGrant: vi.fn(),
    useUpdateTemplateGrant: vi.fn(),
    useDeleteTemplateGrant: vi.fn(),
  }
})

vi.mock('@/queries/organizations', () => ({
  useGetOrganization: vi.fn(),
}))

import {
  useGetTemplateGrant,
  useUpdateTemplateGrant,
  useDeleteTemplateGrant,
} from '@/queries/templateGrants'
import { useGetOrganization } from '@/queries/organizations'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { mockResourcePermissionsForRole } from '@/test/resource-permissions'
import { OrgTemplateGrantDetailPage } from './$grantName'

function makeMockGrant() {
  return {
    name: 'allow-project-foo',
    namespace: 'holos-org-test-org',
    from: [
      {
        $typeName: 'holos.console.v1.TemplateGrantFromRef' as const,
        namespace: 'holos-project-foo',
      },
    ],
    to: [
      {
        $typeName: 'holos.console.v1.TemplateGrantToRef' as const,
        namespace: 'holos-org-test-org',
        name: 'base-template',
      },
    ],
    creatorEmail: 'test@example.com',
    createdAt: undefined,
    status: undefined,
  }
}

function setupMocks(
  userRole: Role = Role.OWNER,
  grant: ReturnType<typeof makeMockGrant> | undefined = makeMockGrant(),
) {
  mockResourcePermissionsForRole(userRole)
  ;(useGetTemplateGrant as Mock).mockReturnValue({
    data: grant,
    isPending: false,
    error: null,
  })
  ;(useUpdateTemplateGrant as Mock).mockReturnValue({
    mutateAsync: vi.fn().mockResolvedValue({}),
    isPending: false,
  })
  ;(useDeleteTemplateGrant as Mock).mockReturnValue({
    mutateAsync: vi.fn().mockResolvedValue({}),
    isPending: false,
  })
  ;(useGetOrganization as Mock).mockReturnValue({
    data: { name: 'test-org', userRole },
    isPending: false,
    error: null,
  })
}

describe('OrgTemplateGrantDetailPage (HOL-1022)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('shows the Delete Grant button for OWNER', () => {
    setupMocks(Role.OWNER)
    render(
      <OrgTemplateGrantDetailPage
        orgName="test-org"
        grantName="allow-project-foo"
        namespaceOverride="holos-org-test-org"
      />,
    )
    expect(screen.getByRole('button', { name: /delete grant/i })).toBeInTheDocument()
  })

  it('hides the Delete Grant button for VIEWER', () => {
    setupMocks(Role.VIEWER)
    render(
      <OrgTemplateGrantDetailPage
        orgName="test-org"
        grantName="allow-project-foo"
        namespaceOverride="holos-org-test-org"
      />,
    )
    expect(screen.queryByRole('button', { name: /delete grant/i })).not.toBeInTheDocument()
  })

  it('hides the Delete Grant button for EDITOR (OWNER-only delete)', () => {
    setupMocks(Role.EDITOR)
    render(
      <OrgTemplateGrantDetailPage
        orgName="test-org"
        grantName="allow-project-foo"
        namespaceOverride="holos-org-test-org"
      />,
    )
    expect(screen.queryByRole('button', { name: /delete grant/i })).not.toBeInTheDocument()
    // But form controls should still be enabled for editors.
    expect(screen.getByRole('button', { name: /^save$/i })).not.toBeDisabled()
  })

  it('renders with prefilled values from the grant', () => {
    setupMocks(Role.OWNER)
    render(
      <OrgTemplateGrantDetailPage
        orgName="test-org"
        grantName="allow-project-foo"
        namespaceOverride="holos-org-test-org"
      />,
    )
    expect(screen.getByLabelText('Grant name')).toHaveValue('allow-project-foo')
    expect(screen.getByLabelText('From namespace')).toHaveValue('holos-project-foo')
    expect(screen.getByLabelText('To namespace')).toHaveValue('holos-org-test-org')
    expect(screen.getByLabelText('To name')).toHaveValue('base-template')
  })

  it('opens the confirm delete dialog when Delete Grant is clicked', async () => {
    setupMocks(Role.OWNER)
    render(
      <OrgTemplateGrantDetailPage
        orgName="test-org"
        grantName="allow-project-foo"
        namespaceOverride="holos-org-test-org"
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: /delete grant/i }))
    await waitFor(() => {
      expect(screen.getByText(/delete resource/i)).toBeInTheDocument()
    })
  })

  it('invokes the delete mutation on confirm', async () => {
    const deleteMutateAsync = vi.fn().mockResolvedValue({})
    setupMocks(Role.OWNER)
    ;(useDeleteTemplateGrant as Mock).mockReturnValue({
      mutateAsync: deleteMutateAsync,
      isPending: false,
    })
    render(
      <OrgTemplateGrantDetailPage
        orgName="test-org"
        grantName="allow-project-foo"
        namespaceOverride="holos-org-test-org"
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: /delete grant/i }))
    await waitFor(() => {
      expect(screen.getByText(/delete resource/i)).toBeInTheDocument()
    })
    // Click the Delete button inside the dialog.
    const deleteButtons = screen.getAllByRole('button', { name: /^delete$/i })
    fireEvent.click(deleteButtons[deleteButtons.length - 1])
    await waitFor(() => {
      expect(deleteMutateAsync).toHaveBeenCalledWith({ name: 'allow-project-foo' })
    })
  })
})

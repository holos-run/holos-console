// HOL-1020: Tests for the org-scoped template-dependency detail/edit page.
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
      useParams: () => ({ orgName: 'test-org', dependencyName: 'dep-a' }),
      useSearch: () => ({ namespace: 'holos-project-test-project' }),
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

vi.mock('@/queries/templateDependencies', async () => {
  const actual = await vi.importActual<typeof import('@/queries/templateDependencies')>(
    '@/queries/templateDependencies',
  )
  return {
    ...actual,
    useGetTemplateDependency: vi.fn(),
    useUpdateTemplateDependency: vi.fn(),
    useDeleteTemplateDependency: vi.fn(),
  }
})

vi.mock('@/queries/organizations', () => ({
  useGetOrganization: vi.fn(),
}))

vi.mock('@/lib/project-context', () => ({
  useProject: vi.fn(),
}))

import {
  useGetTemplateDependency,
  useUpdateTemplateDependency,
  useDeleteTemplateDependency,
} from '@/queries/templateDependencies'
import { useGetOrganization } from '@/queries/organizations'
import { useProject } from '@/lib/project-context'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { OrgTemplateDependencyDetailPage } from './$dependencyName'

function makeMockDependency() {
  return {
    name: 'dep-a',
    namespace: 'holos-project-test-project',
    dependent: {
      $typeName: 'holos.console.v1.LinkedTemplateRef' as const,
      namespace: 'holos-project-test-project',
      name: 'my-template',
      versionConstraint: '',
    },
    requires: {
      $typeName: 'holos.console.v1.LinkedTemplateRef' as const,
      namespace: 'holos-org-my-org',
      name: 'base-template',
      versionConstraint: '',
    },
    cascadeDelete: true,
    creatorEmail: 'test@example.com',
    createdAt: undefined,
    status: undefined,
  }
}

function setupMocks(
  userRole: Role = Role.OWNER,
  dependency: ReturnType<typeof makeMockDependency> | undefined = makeMockDependency(),
) {
  ;(useGetTemplateDependency as Mock).mockReturnValue({
    data: dependency,
    isPending: false,
    error: null,
  })
  ;(useUpdateTemplateDependency as Mock).mockReturnValue({
    mutateAsync: vi.fn().mockResolvedValue({}),
    isPending: false,
  })
  ;(useDeleteTemplateDependency as Mock).mockReturnValue({
    mutateAsync: vi.fn().mockResolvedValue({}),
    isPending: false,
  })
  ;(useGetOrganization as Mock).mockReturnValue({
    data: { name: 'test-org', userRole },
    isPending: false,
    error: null,
  })
  ;(useProject as Mock).mockReturnValue({
    selectedProject: 'test-project',
    setSelectedProject: vi.fn(),
    projects: [],
    isLoading: false,
  })
}

describe('OrgTemplateDependencyDetailPage (HOL-1020)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('shows the Delete Dependency button for OWNER', () => {
    setupMocks(Role.OWNER)
    render(
      <OrgTemplateDependencyDetailPage
        orgName="test-org"
        dependencyName="dep-a"
        namespaceOverride="holos-project-test-project"
      />,
    )
    expect(
      screen.getByRole('button', { name: /delete dependency/i }),
    ).toBeInTheDocument()
  })

  it('hides the Delete Dependency button for VIEWER', () => {
    setupMocks(Role.VIEWER)
    render(
      <OrgTemplateDependencyDetailPage
        orgName="test-org"
        dependencyName="dep-a"
        namespaceOverride="holos-project-test-project"
      />,
    )
    expect(
      screen.queryByRole('button', { name: /delete dependency/i }),
    ).not.toBeInTheDocument()
  })

  it('hides the Delete Dependency button for EDITOR (OWNER-only delete)', () => {
    setupMocks(Role.EDITOR)
    render(
      <OrgTemplateDependencyDetailPage
        orgName="test-org"
        dependencyName="dep-a"
        namespaceOverride="holos-project-test-project"
      />,
    )
    expect(
      screen.queryByRole('button', { name: /delete dependency/i }),
    ).not.toBeInTheDocument()
    // But the form controls should still be enabled for editors.
    expect(screen.getByRole('button', { name: /^save$/i })).not.toBeDisabled()
  })

  it('renders with prefilled values from the dependency', () => {
    setupMocks(Role.OWNER)
    render(
      <OrgTemplateDependencyDetailPage
        orgName="test-org"
        dependencyName="dep-a"
        namespaceOverride="holos-project-test-project"
      />,
    )
    expect(screen.getByLabelText('Dependency name')).toHaveValue('dep-a')
    expect(screen.getByLabelText('Dependent name')).toHaveValue('my-template')
    expect(screen.getByLabelText('Requires name')).toHaveValue('base-template')
  })

  it('opens the confirm delete dialog when Delete Dependency is clicked', async () => {
    setupMocks(Role.OWNER)
    render(
      <OrgTemplateDependencyDetailPage
        orgName="test-org"
        dependencyName="dep-a"
        namespaceOverride="holos-project-test-project"
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: /delete dependency/i }))
    await waitFor(() => {
      // ConfirmDeleteDialog renders "Delete Resource" as dialog title.
      expect(screen.getByText(/delete resource/i)).toBeInTheDocument()
    })
  })

  it('invokes the delete mutation on confirm', async () => {
    const deleteMutateAsync = vi.fn().mockResolvedValue({})
    ;(useDeleteTemplateDependency as Mock).mockReturnValue({
      mutateAsync: deleteMutateAsync,
      isPending: false,
    })
    setupMocks(Role.OWNER)
    ;(useDeleteTemplateDependency as Mock).mockReturnValue({
      mutateAsync: deleteMutateAsync,
      isPending: false,
    })
    render(
      <OrgTemplateDependencyDetailPage
        orgName="test-org"
        dependencyName="dep-a"
        namespaceOverride="holos-project-test-project"
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: /delete dependency/i }))
    await waitFor(() => {
      expect(screen.getByText(/delete resource/i)).toBeInTheDocument()
    })
    // Click the Delete button inside the dialog.
    const deleteButtons = screen.getAllByRole('button', { name: /^delete$/i })
    fireEvent.click(deleteButtons[deleteButtons.length - 1])
    await waitFor(() => {
      expect(deleteMutateAsync).toHaveBeenCalledWith({ name: 'dep-a' })
    })
  })
})

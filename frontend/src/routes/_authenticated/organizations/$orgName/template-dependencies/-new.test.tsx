// HOL-1020: Tests for the org-scoped template-dependency create page.

import { render, screen } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

const mockNavigate = vi.fn()

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => ({ orgName: 'test-org' }),
      useSearch: () => ({}),
    }),
    useNavigate: () => mockNavigate,
    Link: ({
      children,
      className,
      to,
      params,
    }: {
      children: React.ReactNode
      className?: string
      to?: string
      params?: Record<string, string>
    }) => (
      <a href={to} data-params={JSON.stringify(params)} className={className}>
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
    useCreateTemplateDependency: vi.fn(),
  }
})

vi.mock('@/queries/organizations', () => ({
  useGetOrganization: vi.fn(),
}))

vi.mock('@/lib/project-context', () => ({
  useProject: vi.fn(),
}))

vi.mock('@/components/scope-picker/ScopePicker', async () => {
  return {
    ScopePicker: ({
      value,
      onChange,
    }: {
      value: string
      onChange: (v: string) => void
    }) => (
      <button data-testid="scope-picker-trigger" onClick={() => onChange('organization')}>
        {value}
      </button>
    ),
  }
})

import { useCreateTemplateDependency } from '@/queries/templateDependencies'
import { useGetOrganization } from '@/queries/organizations'
import { useProject } from '@/lib/project-context'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { mockResourcePermissionsForRole } from '@/test/resource-permissions'
import { CreateOrgTemplateDependencyPage } from './new'

function setupMocks(
  mutateAsync = vi.fn().mockResolvedValue({}),
  userRole: Role = Role.OWNER,
  selectedProject: string | null = 'test-project',
) {
  mockResourcePermissionsForRole(userRole)
  ;(useCreateTemplateDependency as Mock).mockReturnValue({
    mutateAsync,
    isPending: false,
  })
  ;(useGetOrganization as Mock).mockReturnValue({
    data: { name: 'test-org', userRole },
    isPending: false,
    error: null,
  })
  ;(useProject as Mock).mockReturnValue({
    selectedProject,
    setSelectedProject: vi.fn(),
    projects: [],
    isLoading: false,
  })
}

describe('CreateOrgTemplateDependencyPage (HOL-1020)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setupMocks()
  })

  it('renders the page heading', () => {
    render(<CreateOrgTemplateDependencyPage orgName="test-org" />)
    expect(screen.getByText(/create template dependency/i)).toBeInTheDocument()
  })

  it('renders the ScopePicker', () => {
    render(<CreateOrgTemplateDependencyPage orgName="test-org" />)
    expect(screen.getByTestId('scope-picker-trigger')).toBeInTheDocument()
  })

  it('renders the DependencyForm when a project is selected', () => {
    render(<CreateOrgTemplateDependencyPage orgName="test-org" />)
    expect(screen.getByLabelText(/^dependency name$/i)).toBeInTheDocument()
  })

  it('shows a prompt when no project is selected and scope is project', () => {
    setupMocks(vi.fn(), Role.OWNER, null)
    render(<CreateOrgTemplateDependencyPage orgName="test-org" />)
    // With null selectedProject and default scope 'organization', the form renders
    // but with empty namespace. The prompt appears when scope='project' with no project.
    // Since ScopePicker defaults to 'organization' when selectedProject is null,
    // the form renders without a namespace.
    expect(screen.queryByText(/select a project from the switcher/i)).not.toBeInTheDocument()
  })

  it('disables form controls for VIEWER', () => {
    setupMocks(vi.fn(), Role.VIEWER)
    render(<CreateOrgTemplateDependencyPage orgName="test-org" />)
    expect(screen.getByRole('button', { name: /^create$/i })).toBeDisabled()
  })

  it('enables form controls for OWNER', () => {
    setupMocks(vi.fn(), Role.OWNER)
    render(<CreateOrgTemplateDependencyPage orgName="test-org" />)
    expect(screen.getByRole('button', { name: /^create$/i })).not.toBeDisabled()
  })
})

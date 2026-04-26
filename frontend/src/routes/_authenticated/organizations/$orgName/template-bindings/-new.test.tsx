// HOL-1024: Tests for the org-scope TemplatePolicyBinding create page.
//
// Covers:
// 1. Page heading renders.
// 2. ScopePicker is rendered; defaults to 'organization' scope.
// 3. Switching to project scope with a project selected shows the form.
// 4. Switching to project scope with no project selected shows a prompt.
// 5. VIEWER role disables controls.
// 6. OWNER/EDITOR roles enable controls.

import { render, screen, fireEvent } from '@testing-library/react'
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
    organizationPrefix: 'holos-org-',
    folderPrefix: 'holos-folder-',
    projectPrefix: 'holos-project-',
  }),
}))

vi.mock('@/queries/templatePolicyBindings', async () => {
  const actual = await vi.importActual<typeof import('@/queries/templatePolicyBindings')>(
    '@/queries/templatePolicyBindings',
  )
  return {
    ...actual,
    useCreateTemplatePolicyBinding: vi.fn(),
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
      disabled,
    }: {
      value: string
      onChange: (v: string) => void
      disabled?: boolean
    }) => (
      <button
        data-testid="scope-picker-trigger"
        disabled={disabled}
        onClick={() => onChange(value === 'organization' ? 'project' : 'organization')}
      >
        {value}
      </button>
    ),
  }
})

// BindingForm has deep dependencies; mock it to keep tests focused on the page.
vi.mock('@/components/template-policy-bindings/BindingForm', () => ({
  BindingForm: ({ scopeType }: { scopeType: string }) => (
    <div data-testid="binding-form" data-scope={scopeType}>
      <input aria-label="Display Name" />
      <button>Create</button>
    </div>
  ),
}))

import { useCreateTemplatePolicyBinding } from '@/queries/templatePolicyBindings'
import { useGetOrganization } from '@/queries/organizations'
import { useProject } from '@/lib/project-context'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { CreateOrgTemplateBindingPage } from './new'

function setupMocks(
  mutateAsync = vi.fn().mockResolvedValue({}),
  userRole: Role = Role.OWNER,
  selectedProject: string | null = 'test-project',
) {
  ;(useCreateTemplatePolicyBinding as Mock).mockReturnValue({
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

describe('CreateOrgTemplateBindingPage (HOL-1024)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setupMocks()
  })

  it('renders the page heading', () => {
    render(<CreateOrgTemplateBindingPage orgName="test-org" />)
    expect(screen.getByText(/create template binding/i)).toBeInTheDocument()
  })

  it('renders the ScopePicker', () => {
    render(<CreateOrgTemplateBindingPage orgName="test-org" />)
    expect(screen.getByTestId('scope-picker-trigger')).toBeInTheDocument()
  })

  it('defaults to organization scope', () => {
    render(<CreateOrgTemplateBindingPage orgName="test-org" />)
    expect(screen.getByTestId('scope-picker-trigger')).toHaveTextContent('organization')
  })

  it('shows the form with organization scopeType by default', () => {
    render(<CreateOrgTemplateBindingPage orgName="test-org" />)
    expect(screen.getByTestId('binding-form')).toHaveAttribute('data-scope', 'organization')
  })

  it('shows the form with project scopeType after switching to project scope', () => {
    render(<CreateOrgTemplateBindingPage orgName="test-org" />)
    // Switch to project scope.
    fireEvent.click(screen.getByTestId('scope-picker-trigger'))
    expect(screen.getByTestId('binding-form')).toHaveAttribute('data-scope', 'project')
  })

  it('shows a prompt when scope=project and no project is selected', () => {
    setupMocks(vi.fn(), Role.OWNER, null)
    render(<CreateOrgTemplateBindingPage orgName="test-org" />)
    // Switch to project scope via the mock picker.
    fireEvent.click(screen.getByTestId('scope-picker-trigger'))
    expect(
      screen.getByText(/select a project from the switcher/i),
    ).toBeInTheDocument()
  })

  it('disables the scope picker for VIEWER', () => {
    setupMocks(vi.fn(), Role.VIEWER)
    render(<CreateOrgTemplateBindingPage orgName="test-org" />)
    expect(screen.getByTestId('scope-picker-trigger')).toBeDisabled()
  })

  it('enables the scope picker for OWNER', () => {
    setupMocks(vi.fn(), Role.OWNER)
    render(<CreateOrgTemplateBindingPage orgName="test-org" />)
    expect(screen.getByTestId('scope-picker-trigger')).not.toBeDisabled()
  })

  it('enables the scope picker for EDITOR', () => {
    setupMocks(vi.fn(), Role.EDITOR)
    render(<CreateOrgTemplateBindingPage orgName="test-org" />)
    expect(screen.getByTestId('scope-picker-trigger')).not.toBeDisabled()
  })

  // HOL-1024: When scope=project and a project is selected, the mutation is
  // called with the project namespace.
  it('calls useCreateTemplatePolicyBinding with project namespace when scope=project', () => {
    render(<CreateOrgTemplateBindingPage orgName="test-org" />)
    // Switch to project scope.
    fireEvent.click(screen.getByTestId('scope-picker-trigger'))
    // The mutation hook should have been called with the project namespace.
    expect(useCreateTemplatePolicyBinding).toHaveBeenCalledWith('holos-project-test-project')
  })

  // HOL-1024: Default org scope routes mutation to org namespace.
  it('calls useCreateTemplatePolicyBinding with org namespace by default', () => {
    render(<CreateOrgTemplateBindingPage orgName="test-org" />)
    expect(useCreateTemplatePolicyBinding).toHaveBeenCalledWith('holos-org-test-org')
  })
})

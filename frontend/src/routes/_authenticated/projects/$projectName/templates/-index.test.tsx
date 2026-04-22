/**
 * Tests for the project-scoped unified Templates index (HOL-859).
 *
 * Exercises ResourceGrid v1 with all three template-family kinds:
 *   Template, TemplatePolicy, TemplatePolicyBinding
 *
 * All query hooks are mocked. The test directly renders ProjectTemplatesIndexPage.
 */

import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'
import { Role } from '@/gen/holos/console/v1/rbac_pb'

// ---------------------------------------------------------------------------
// Router mock
// ---------------------------------------------------------------------------

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => ({ projectName: 'test-project' }),
      useSearch: () => ({}),
      fullPath: '/projects/test-project/templates/',
    }),
    Link: ({
      children,
      to,
      className,
    }: {
      children: React.ReactNode
      to?: string
      className?: string
    }) => (
      <a href={to ?? '#'} className={className}>
        {children}
      </a>
    ),
    useNavigate: () => vi.fn(),
  }
})

// ---------------------------------------------------------------------------
// Console-config mock — predictable namespace prefixes
// ---------------------------------------------------------------------------

vi.mock('@/lib/console-config', () => ({
  getConsoleConfig: vi.fn().mockReturnValue({
    namespacePrefix: '',
    organizationPrefix: 'org-',
    folderPrefix: 'folder-',
    projectPrefix: 'project-',
  }),
}))

// ---------------------------------------------------------------------------
// Query mocks
// ---------------------------------------------------------------------------

vi.mock('@/queries/templates', () => ({
  useAllTemplatesForOrg: vi.fn(),
}))

vi.mock('@/queries/templatePolicies', () => ({
  useAllTemplatePoliciesForOrg: vi.fn(),
}))

vi.mock('@/queries/templatePolicyBindings', () => ({
  useAllTemplatePolicyBindingsForOrg: vi.fn(),
}))

vi.mock('@/queries/projects', () => ({
  useGetProject: vi.fn(),
}))

vi.mock('@/queries/organizations', () => ({
  useGetOrganization: vi.fn(),
}))

// OrgContext mock
vi.mock('@/lib/org-context', () => ({
  useOrg: vi.fn(),
}))

// ConnectRPC transport + query client mocks (for delete path)
vi.mock('@connectrpc/connect-query', () => ({
  useTransport: vi.fn().mockReturnValue({}),
}))

vi.mock('@tanstack/react-query', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-query')>()
  return {
    ...actual,
    useQueryClient: vi.fn().mockReturnValue({
      invalidateQueries: vi.fn().mockResolvedValue(undefined),
    }),
  }
})

vi.mock('@connectrpc/connect', () => ({
  createClient: vi.fn().mockReturnValue({
    deleteTemplate: vi.fn().mockResolvedValue({}),
    deleteTemplatePolicy: vi.fn().mockResolvedValue({}),
    deleteTemplatePolicyBinding: vi.fn().mockResolvedValue({}),
  }),
}))

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

// ---------------------------------------------------------------------------
// Imports after mocks
// ---------------------------------------------------------------------------

import { useAllTemplatesForOrg } from '@/queries/templates'
import { useAllTemplatePoliciesForOrg } from '@/queries/templatePolicies'
import { useAllTemplatePolicyBindingsForOrg } from '@/queries/templatePolicyBindings'
import { useGetProject } from '@/queries/projects'
import { useGetOrganization } from '@/queries/organizations'
import { useOrg } from '@/lib/org-context'
import { ProjectTemplatesIndexPage } from './index'

// ---------------------------------------------------------------------------
// Test data helpers
// ---------------------------------------------------------------------------

function makeTemplate(name: string, namespace = 'project-test-project') {
  return { name, namespace, displayName: name, description: '', cueTemplate: '' }
}

function makePolicy(name: string, namespace = 'org-acme') {
  return { name, namespace, displayName: name, description: '', rules: [] }
}

function makeBinding(name: string, namespace = 'org-acme') {
  return { name, namespace, displayName: name, description: '' }
}

// ---------------------------------------------------------------------------
// Setup helpers
// ---------------------------------------------------------------------------

function setupMocks({
  templates = [makeTemplate('my-template')],
  policies = [] as ReturnType<typeof makePolicy>[],
  bindings = [] as ReturnType<typeof makeBinding>[],
  templatesPending = false,
  policiesPending = false,
  bindingsPending = false,
  templatesError = null,
  policiesError = null,
  bindingsError = null,
  projectRole = Role.OWNER,
  orgRole = Role.OWNER,
  orgName = 'acme',
}: {
  templates?: ReturnType<typeof makeTemplate>[]
  policies?: ReturnType<typeof makePolicy>[]
  bindings?: ReturnType<typeof makeBinding>[]
  templatesPending?: boolean
  policiesPending?: boolean
  bindingsPending?: boolean
  templatesError?: Error | null
  policiesError?: Error | null
  bindingsError?: Error | null
  projectRole?: number
  orgRole?: number
  orgName?: string | null
} = {}) {
  ;(useOrg as Mock).mockReturnValue({ selectedOrg: orgName })
  ;(useGetProject as Mock).mockReturnValue({
    data: { name: 'test-project', userRole: projectRole },
    isPending: false,
  })
  ;(useGetOrganization as Mock).mockReturnValue({
    data: { name: orgName, userRole: orgRole },
    isPending: false,
  })
  ;(useAllTemplatesForOrg as Mock).mockReturnValue({
    data: templates,
    isPending: templatesPending,
    error: templatesError,
  })
  ;(useAllTemplatePoliciesForOrg as Mock).mockReturnValue({
    data: policies,
    isPending: policiesPending,
    error: policiesError,
  })
  ;(useAllTemplatePolicyBindingsForOrg as Mock).mockReturnValue({
    data: bindings,
    isPending: bindingsPending,
    error: bindingsError,
  })
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('ProjectTemplatesIndexPage (ResourceGrid v1)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  // -------------------------------------------------------------------------
  // Default view
  // -------------------------------------------------------------------------

  it('renders default view showing only Template rows from the current project', () => {
    // Default URL: kind=Template, lineage=descendants → only Template rows visible.
    setupMocks({
      templates: [makeTemplate('web-template', 'project-test-project')],
      policies: [makePolicy('strict-policy', 'org-acme')],
      bindings: [makeBinding('prod-binding', 'org-acme')],
    })
    render(<ProjectTemplatesIndexPage projectName="test-project" />)

    // Template row visible (kind=Template is the default kind filter)
    expect(screen.getByText('web-template')).toBeInTheDocument()

    // Policy and binding rows are present in the DOM but filtered out by the
    // kind filter default (kind=Template). They should not appear.
    expect(screen.queryByText('strict-policy')).not.toBeInTheDocument()
    expect(screen.queryByText('prod-binding')).not.toBeInTheDocument()
  })

  it('shows loading skeleton while any fan-out is pending', () => {
    setupMocks({ templatesPending: true, templates: [] })
    render(<ProjectTemplatesIndexPage projectName="test-project" />)
    expect(screen.getByTestId('resource-grid-loading')).toBeInTheDocument()
  })

  it('shows error when templates fetch fails and no rows available', () => {
    setupMocks({
      templates: [],
      policies: [],
      bindings: [],
      templatesError: new Error('templates fetch failed'),
    })
    render(<ProjectTemplatesIndexPage projectName="test-project" />)
    expect(screen.getByText(/templates fetch failed/i)).toBeInTheDocument()
  })

  // -------------------------------------------------------------------------
  // No org selected
  // -------------------------------------------------------------------------

  it('renders "select an organization" message when orgName is null', () => {
    setupMocks({ orgName: null })
    render(<ProjectTemplatesIndexPage projectName="test-project" />)
    expect(screen.getByText(/select an organization/i)).toBeInTheDocument()
  })

  // -------------------------------------------------------------------------
  // Kind filter
  // -------------------------------------------------------------------------

  it('kind filter renders three kind checkboxes', () => {
    setupMocks({
      templates: [makeTemplate('my-template')],
      policies: [makePolicy('my-policy')],
    })
    render(<ProjectTemplatesIndexPage projectName="test-project" />)
    const kindFilter = screen.getByTestId('kind-filter')
    expect(kindFilter).toBeInTheDocument()
    expect(screen.getByLabelText(/filter template$/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/filter template policy$/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/filter template policy binding/i)).toBeInTheDocument()
  })

  // -------------------------------------------------------------------------
  // New dropdown
  // -------------------------------------------------------------------------

  it('New dropdown lists three entries for org OWNER', () => {
    setupMocks({ orgRole: Role.OWNER, projectRole: Role.OWNER })
    render(<ProjectTemplatesIndexPage projectName="test-project" />)
    // The "New" button should be a dropdown when all three kinds have canCreate+newHref
    const newBtn = screen.getByRole('button', { name: /new/i })
    expect(newBtn).toBeInTheDocument()
    fireEvent.click(newBtn)
    expect(screen.getByText('Template')).toBeInTheDocument()
    expect(screen.getByText('Template Policy')).toBeInTheDocument()
    expect(screen.getByText('Template Policy Binding')).toBeInTheDocument()
  })

  it('only Template "New" button shown for org VIEWER who can still create project templates', () => {
    // projectRole=OWNER can create Templates; orgRole=VIEWER cannot create policies/bindings.
    setupMocks({ orgRole: Role.VIEWER, projectRole: Role.OWNER })
    render(<ProjectTemplatesIndexPage projectName="test-project" />)
    // With only one creatable kind, the New button should be a single link, not a dropdown.
    expect(screen.getByRole('button', { name: /new template$/i })).toBeInTheDocument()
  })

  // -------------------------------------------------------------------------
  // Delete flow
  // -------------------------------------------------------------------------

  it('delete button opens ConfirmDeleteDialog', async () => {
    setupMocks({
      templates: [makeTemplate('my-template', 'project-test-project')],
    })
    render(<ProjectTemplatesIndexPage projectName="test-project" />)
    const deleteBtn = screen.getByRole('button', { name: /delete my-template/i })
    fireEvent.click(deleteBtn)
    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })
  })

  // -------------------------------------------------------------------------
  // Parent column
  // -------------------------------------------------------------------------

  it('Parent column shown when rows span multiple scopes', () => {
    setupMocks({
      templates: [
        makeTemplate('proj-tpl', 'project-test-project'),
        makeTemplate('org-tpl', 'org-acme'),
      ],
      policies: [],
      bindings: [],
    })
    render(<ProjectTemplatesIndexPage projectName="test-project" />)
    // With all kinds selected (clearing default Template-only filter isn't needed
    // here — the parent column visibility is driven by having >1 unique parentId).
    // Both templates are in the row list (filtered by kind=Template which shows both).
    expect(screen.getByRole('columnheader', { name: /parent/i })).toBeInTheDocument()
  })

  it('Parent column hidden when all rows share the same parent', () => {
    setupMocks({
      templates: [
        makeTemplate('tpl-a', 'project-test-project'),
        makeTemplate('tpl-b', 'project-test-project'),
      ],
    })
    render(<ProjectTemplatesIndexPage projectName="test-project" />)
    expect(screen.queryByRole('columnheader', { name: /parent/i })).not.toBeInTheDocument()
  })

  // -------------------------------------------------------------------------
  // Fan-out hooks called with orgName
  // -------------------------------------------------------------------------

  it('calls all three fan-out hooks with the selected orgName', () => {
    setupMocks({ orgName: 'my-org' })
    render(<ProjectTemplatesIndexPage projectName="test-project" />)
    expect(useAllTemplatesForOrg).toHaveBeenCalledWith('my-org')
    expect(useAllTemplatePoliciesForOrg).toHaveBeenCalledWith('my-org')
    expect(useAllTemplatePolicyBindingsForOrg).toHaveBeenCalledWith('my-org')
  })
})

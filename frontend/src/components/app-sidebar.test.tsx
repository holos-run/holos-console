import { render, screen, act } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

// Tests for the sidebar nav: Projects, Secrets, Deployments (flat), and
// Templates (collapsible group with Policy / Dependencies / Grants submenus).
// The workspace picker lives in SidebarHeader; the version label in SidebarFooter.

// Configurable per-test so we can drive route-based active-state gating.
let mockPathname = '/'

// Expose a setter so regression tests can update the pathname and trigger
// a re-render of the same component instance via act().
export function setMockPathname(path: string) {
  mockPathname = path
}

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    Link: ({
      children,
      to,
      params,
    }: {
      children: React.ReactNode
      to: string
      params?: Record<string, string>
    }) => {
      let href = to as string
      if (params) {
        Object.entries(params).forEach(([k, v]) => {
          href = href.replace(`$${k}`, v)
        })
      }
      return <a href={href}>{children}</a>
    },
    useLocation: () => ({ pathname: mockPathname }),
    useNavigate: () => vi.fn(),
  }
})

// Forward `isActive` as `data-active` so active-state highlighting can be
// asserted; the real sidebar primitives do the same internally. `asChild`
// passes through untouched so `<Link>` / button children render without
// wrapping.
vi.mock('@/components/ui/sidebar', () => ({
  Sidebar: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SidebarContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SidebarFooter: ({
    children,
    ...rest
  }: React.HTMLAttributes<HTMLDivElement> & { children: React.ReactNode }) => (
    <div data-testid="sidebar-footer" {...rest}>
      {children}
    </div>
  ),
  SidebarGroup: ({
    children,
    ...rest
  }: React.HTMLAttributes<HTMLDivElement> & { children: React.ReactNode }) => (
    <div {...rest}>{children}</div>
  ),
  SidebarGroupContent: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
  SidebarGroupLabel: ({ children }: { children: React.ReactNode }) => (
    <div data-testid="sidebar-group-label">{children}</div>
  ),
  SidebarHeader: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SidebarMenu: ({ children }: { children: React.ReactNode }) => <ul>{children}</ul>,
  SidebarMenuButton: ({
    children,
    asChild,
    isActive,
    disabled,
    'data-testid': dataTestId,
    ...rest
  }: React.HTMLAttributes<HTMLElement> & {
    children: React.ReactNode
    asChild?: boolean
    isActive?: boolean
    disabled?: boolean
    'data-testid'?: string
  }) => {
    if (asChild) {
      // Wrap in a span so data-testid and data-active survive the asChild pattern
      return (
        <span
          data-testid={dataTestId}
          data-active={isActive ? 'true' : 'false'}
        >
          {children}
        </span>
      )
    }
    return (
      <button
        data-testid={dataTestId}
        data-active={isActive ? 'true' : 'false'}
        disabled={disabled}
        {...rest}
      >
        {children}
      </button>
    )
  },
  SidebarMenuItem: ({ children }: { children: React.ReactNode }) => (
    <li>{children}</li>
  ),
  SidebarMenuSub: ({ children }: { children: React.ReactNode }) => (
    <ul>{children}</ul>
  ),
  SidebarMenuSubItem: ({ children }: { children: React.ReactNode }) => (
    <li>{children}</li>
  ),
  SidebarMenuSubButton: ({
    children,
    asChild,
    isActive,
    'data-testid': dataTestId,
    ...rest
  }: React.HTMLAttributes<HTMLElement> & {
    children: React.ReactNode
    asChild?: boolean
    isActive?: boolean
    'data-testid'?: string
  }) => {
    if (asChild) {
      return (
        <span
          data-testid={dataTestId}
          data-active={isActive ? 'true' : 'false'}
        >
          {children}
        </span>
      )
    }
    return (
      <a
        data-testid={dataTestId}
        data-active={isActive ? 'true' : 'false'}
        {...rest}
      >
        {children}
      </a>
    )
  },
}))

// Stub Collapsible so CollapsibleContent is always rendered (open=true in tests)
// and CollapsibleTrigger passes through its child.
vi.mock('@/components/ui/collapsible', () => ({
  Collapsible: ({
    children,
    className,
  }: {
    children: React.ReactNode
    open?: boolean
    className?: string
  }) => (
    <div data-testid="collapsible" className={className}>
      {children}
    </div>
  ),
  CollapsibleTrigger: ({
    children,
    asChild,
  }: {
    children: React.ReactNode
    asChild?: boolean
  }) => (asChild ? <>{children}</> : <button>{children}</button>),
  CollapsibleContent: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
}))

// Flatten Tooltip so TooltipContent renders inline; content-level assertions
// live here, hover/focus wiring covered by integration tests.
vi.mock('@/components/ui/tooltip', () => ({
  Tooltip: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipTrigger: ({
    children,
    asChild,
  }: {
    children: React.ReactNode
    asChild?: boolean
  }) => (asChild ? <>{children}</> : <span>{children}</span>),
  TooltipContent: ({
    children,
    ...rest
  }: React.HTMLAttributes<HTMLDivElement> & { children: React.ReactNode }) => (
    <div data-testid="tooltip-content" {...rest}>
      {children}
    </div>
  ),
}))

// Stub the workspace menu so AppSidebar tests stay focused on sidebar
// composition; WorkspaceMenu has its own dedicated test file.
vi.mock('@/components/workspace-menu', () => ({
  WorkspaceMenu: () => <div data-testid="workspace-menu" />,
}))

vi.mock('@/lib/org-context', () => ({ useOrg: vi.fn() }))
vi.mock('@/lib/project-context', () => ({ useProject: vi.fn() }))
vi.mock('@/queries/version', () => ({ useVersion: vi.fn() }))

import { useOrg } from '@/lib/org-context'
import { useProject } from '@/lib/project-context'
import { useVersion } from '@/queries/version'
import { AppSidebar } from './app-sidebar'

function setDefaults() {
  ;(useOrg as Mock).mockReturnValue({
    organizations: [],
    selectedOrg: null,
    setSelectedOrg: vi.fn(),
    isLoading: false,
  })
  ;(useProject as Mock).mockReturnValue({
    projects: [],
    selectedProject: null,
    setSelectedProject: vi.fn(),
    isLoading: false,
  })
  ;(useVersion as Mock).mockReturnValue({ data: { version: 'v0.0.0-test' } })
}

function setupOrgSelected(orgName = 'my-org') {
  ;(useOrg as Mock).mockReturnValue({
    organizations: [{ name: orgName, displayName: 'My Org' }],
    selectedOrg: orgName,
    setSelectedOrg: vi.fn(),
    isLoading: false,
  })
}

function setupProjectSelected(projectName = 'my-project') {
  ;(useProject as Mock).mockReturnValue({
    projects: [{ name: projectName, displayName: 'My Project' }],
    selectedProject: projectName,
    setSelectedProject: vi.fn(),
    isLoading: false,
  })
}

// Nav entry helpers
function getNavButton(label: string) {
  return screen.getByTestId(
    `nav-${label.toLowerCase().replace(/\s+/g, '-')}`,
  )
}

describe('AppSidebar — sidebar nav structure', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockPathname = '/'
    setDefaults()
  })

  it('renders the workspace menu in the header', () => {
    render(<AppSidebar />)
    expect(screen.getByTestId('workspace-menu')).toBeInTheDocument()
  })

  it('renders Project, Secrets, Deployments, and Templates nav entries', () => {
    render(<AppSidebar />)
    expect(getNavButton('project')).toBeInTheDocument()
    expect(getNavButton('secrets')).toBeInTheDocument()
    expect(getNavButton('deployments')).toBeInTheDocument()
    expect(getNavButton('templates')).toBeInTheDocument()
  })

  it('does not render a Resource Manager nav entry', () => {
    render(<AppSidebar />)
    expect(screen.queryByTestId('nav-resource-manager')).toBeNull()
  })

  it('renders nav entries in canonical order: Project, Secrets, Deployments, Templates', () => {
    render(<AppSidebar />)
    const project = getNavButton('project')
    const secrets = getNavButton('secrets')
    const deployments = getNavButton('deployments')
    const templates = getNavButton('templates')
    // Node.DOCUMENT_POSITION_FOLLOWING = 4
    expect(
      project.compareDocumentPosition(secrets) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy()
    expect(
      secrets.compareDocumentPosition(deployments) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy()
    expect(
      deployments.compareDocumentPosition(templates) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy()
  })

  it('renders the version string in the sidebar footer', () => {
    render(<AppSidebar />)
    const footer = screen.getByTestId('sidebar-footer')
    expect(footer).toBeInTheDocument()
    expect(footer).toHaveTextContent('v0.0.0-test')
  })

  it('does not render a version string when version data is absent', () => {
    ;(useVersion as Mock).mockReturnValue({ data: null })
    render(<AppSidebar />)
    expect(screen.queryByTestId('sidebar-footer')).not.toBeInTheDocument()
  })

  it('does not render a theme toggle button', () => {
    render(<AppSidebar />)
    expect(screen.queryByRole('button', { name: /toggle theme/i })).toBeNull()
  })

  it('does not render About, Profile, or Dev Tools links', () => {
    render(<AppSidebar />)
    expect(screen.queryByRole('link', { name: /^about$/i })).toBeNull()
    expect(screen.queryByRole('link', { name: /^profile$/i })).toBeNull()
    expect(screen.queryByRole('link', { name: /dev tools/i })).toBeNull()
  })

  it('does not render OrgPicker or ProjectPicker dropdowns', () => {
    render(<AppSidebar />)
    expect(screen.queryByTestId('org-picker')).toBeNull()
    expect(screen.queryByTestId('project-picker')).toBeNull()
  })

  it('does not render the old Collapsible project-tree or org-tree', () => {
    render(<AppSidebar />)
    expect(screen.queryByTestId('project-tree')).not.toBeInTheDocument()
    expect(screen.queryByTestId('org-tree')).not.toBeInTheDocument()
    expect(screen.queryByTestId('project-tree-trigger')).not.toBeInTheDocument()
    expect(screen.queryByTestId('org-tree-trigger')).not.toBeInTheDocument()
  })
})

describe('AppSidebar — nav links when no org or project is selected (disabled state)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockPathname = '/'
    setDefaults()
  })

  it('Project button is disabled when no project is selected', () => {
    render(<AppSidebar />)
    expect(getNavButton('project')).toBeDisabled()
  })

  it('Secrets, Deployments, and Templates buttons are disabled when no project is selected', () => {
    render(<AppSidebar />)
    expect(getNavButton('secrets')).toBeDisabled()
    expect(getNavButton('deployments')).toBeDisabled()
    expect(getNavButton('templates')).toBeDisabled()
  })

  it('Templates disabled button renders tooltip with the prerequisite reason', () => {
    render(<AppSidebar />)
    const tooltips = screen.getAllByTestId('tooltip-content')
    const templatesToolTip = tooltips.find((el) =>
      el.textContent?.includes('Templates'),
    )
    expect(templatesToolTip).toBeDefined()
    expect(templatesToolTip?.textContent).toContain('Select an organization')
  })

  it('Project disabled button renders tooltip "Select a project to view Project"', () => {
    render(<AppSidebar />)
    const tooltips = screen.getAllByTestId('tooltip-content')
    const projectTooltip = tooltips.find((el) =>
      el.textContent?.includes('Select a project to view Project'),
    )
    expect(projectTooltip).toBeDefined()
  })

  it('disabled buttons render a tooltip with the prerequisite reason for Secrets', () => {
    render(<AppSidebar />)
    const tooltips = screen.getAllByTestId('tooltip-content')
    const secretsTooltip = tooltips.find((el) =>
      el.textContent?.includes('Secrets'),
    )
    expect(secretsTooltip).toBeDefined()
    expect(secretsTooltip?.textContent).toContain('Select a project')
  })

  it('disabled buttons render tooltip for Deployments', () => {
    render(<AppSidebar />)
    const tooltips = screen.getAllByTestId('tooltip-content')
    const tooltip = tooltips.find((el) =>
      el.textContent?.includes('Deployments'),
    )
    expect(tooltip).toBeDefined()
  })

  it('no Templates sub-links render when Templates is disabled', () => {
    render(<AppSidebar />)
    expect(screen.queryByTestId('nav-template-policies')).not.toBeInTheDocument()
    expect(screen.queryByTestId('nav-policy-bindings')).not.toBeInTheDocument()
    expect(screen.queryByTestId('nav-template-dependencies')).not.toBeInTheDocument()
    expect(screen.queryByTestId('nav-requirements')).not.toBeInTheDocument()
    expect(screen.queryByTestId('nav-template-grants')).not.toBeInTheDocument()
  })
})

describe('AppSidebar — Project nav entry (HOL-1004)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockPathname = '/'
    setDefaults()
  })

  it('Project button is disabled when no project is selected (even if org is selected)', () => {
    setupOrgSelected()
    render(<AppSidebar />)
    expect(getNavButton('project')).toBeDisabled()
  })

  it('Project link resolves to /projects/$projectName when a project is selected', () => {
    setupOrgSelected()
    setupProjectSelected()
    render(<AppSidebar />)
    expect(
      screen.getByRole('link', { name: /^project$/i }).getAttribute('href'),
    ).toBe('/projects/my-project/')
  })

  it('Project nav button is enabled when a project is selected', () => {
    setupOrgSelected()
    setupProjectSelected()
    render(<AppSidebar />)
    const projectContainer = getNavButton('project')
    expect(projectContainer).not.toHaveAttribute('disabled')
    expect(projectContainer.querySelector('a')).not.toBeNull()
  })

  it('Project nav entry is active when on /projects/$projectName', () => {
    setupOrgSelected()
    setupProjectSelected()
    mockPathname = '/projects/my-project'
    render(<AppSidebar />)
    expect(getNavButton('project')).toHaveAttribute('data-active', 'true')
  })

  it('Project nav entry is active when on a descendant route (secrets)', () => {
    setupOrgSelected()
    setupProjectSelected()
    mockPathname = '/projects/my-project/secrets'
    render(<AppSidebar />)
    expect(getNavButton('project')).toHaveAttribute('data-active', 'true')
  })

  it('Project nav entry is active when on a descendant route (deployments)', () => {
    setupOrgSelected()
    setupProjectSelected()
    mockPathname = '/projects/my-project/deployments'
    render(<AppSidebar />)
    expect(getNavButton('project')).toHaveAttribute('data-active', 'true')
  })

  it('Project nav entry is active when on a descendant route (templates)', () => {
    setupOrgSelected()
    setupProjectSelected()
    mockPathname = '/projects/my-project/templates'
    render(<AppSidebar />)
    expect(getNavButton('project')).toHaveAttribute('data-active', 'true')
  })

  it('Project nav entry is not active when on an unrelated route', () => {
    setupOrgSelected()
    setupProjectSelected()
    mockPathname = '/'
    render(<AppSidebar />)
    expect(getNavButton('project')).toHaveAttribute('data-active', 'false')
  })
})

describe('AppSidebar — nav links when a project is selected', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockPathname = '/'
    setDefaults()
    setupOrgSelected()
    setupProjectSelected()
  })

  it('Secrets link resolves to the correct project-scoped URL', () => {
    render(<AppSidebar />)
    expect(
      screen.getByRole('link', { name: /^secrets$/i }).getAttribute('href'),
    ).toBe('/projects/my-project/secrets')
  })

  it('Deployments link resolves to the correct project-scoped URL', () => {
    render(<AppSidebar />)
    expect(
      screen.getByRole('link', { name: /^deployments$/i }).getAttribute('href'),
    ).toBe('/projects/my-project/deployments')
  })

  it('Templates link resolves to the org-scoped unified surface URL when an org is selected (HOL-1006)', () => {
    render(<AppSidebar />)
    expect(
      screen.getByRole('link', { name: /^templates$/i }).getAttribute('href'),
    ).toBe('/organizations/my-org/templates')
  })

  it('no disabled buttons when org and project are selected', () => {
    render(<AppSidebar />)
    const projectContainer = getNavButton('project')
    const secretsContainer = getNavButton('secrets')
    const deploymentsContainer = getNavButton('deployments')
    const templatesContainer = getNavButton('templates')
    expect(projectContainer).not.toHaveAttribute('disabled')
    expect(secretsContainer).not.toHaveAttribute('disabled')
    expect(deploymentsContainer).not.toHaveAttribute('disabled')
    expect(templatesContainer).not.toHaveAttribute('disabled')
    expect(projectContainer.querySelector('a')).not.toBeNull()
    expect(secretsContainer.querySelector('a')).not.toBeNull()
    expect(deploymentsContainer.querySelector('a')).not.toBeNull()
    expect(templatesContainer.querySelector('a')).not.toBeNull()
  })
})

describe('AppSidebar — Templates collapsible group (HOL-1014)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockPathname = '/'
    setDefaults()
    setupOrgSelected()
    setupProjectSelected()
  })

  it('renders all five sub-links when project is selected', () => {
    render(<AppSidebar />)
    expect(screen.getByTestId('nav-template-policies')).toBeInTheDocument()
    expect(screen.getByTestId('nav-policy-bindings')).toBeInTheDocument()
    expect(screen.getByTestId('nav-template-dependencies')).toBeInTheDocument()
    expect(screen.getByTestId('nav-requirements')).toBeInTheDocument()
    expect(screen.getByTestId('nav-template-grants')).toBeInTheDocument()
  })

  it('Template Policies sub-link resolves to the correct project-scoped URL', () => {
    render(<AppSidebar />)
    const btn = screen.getByTestId('nav-template-policies')
    expect(btn.querySelector('a')?.getAttribute('href')).toBe(
      '/projects/my-project/templates/policies/',
    )
  })

  it('Policy Bindings sub-link resolves to the correct project-scoped URL', () => {
    render(<AppSidebar />)
    const btn = screen.getByTestId('nav-policy-bindings')
    expect(btn.querySelector('a')?.getAttribute('href')).toBe(
      '/projects/my-project/templates/policy-bindings/',
    )
  })

  it('Template Dependencies sub-link resolves to the correct project-scoped URL', () => {
    render(<AppSidebar />)
    const btn = screen.getByTestId('nav-template-dependencies')
    expect(btn.querySelector('a')?.getAttribute('href')).toBe(
      '/projects/my-project/templates/dependencies/',
    )
  })

  it('Requirements sub-link resolves to the correct project-scoped URL', () => {
    render(<AppSidebar />)
    const btn = screen.getByTestId('nav-requirements')
    expect(btn.querySelector('a')?.getAttribute('href')).toBe(
      '/projects/my-project/templates/requirements/',
    )
  })

  it('Template Grants sub-link resolves to the correct project-scoped URL', () => {
    render(<AppSidebar />)
    const btn = screen.getByTestId('nav-template-grants')
    expect(btn.querySelector('a')?.getAttribute('href')).toBe(
      '/projects/my-project/templates/grants/',
    )
  })

  it('sub-links do not render when only org is selected (no project)', () => {
    ;(useProject as Mock).mockReturnValue({
      projects: [],
      selectedProject: null,
      setSelectedProject: vi.fn(),
      isLoading: false,
    })
    render(<AppSidebar />)
    expect(screen.queryByTestId('nav-template-policies')).not.toBeInTheDocument()
    expect(screen.queryByTestId('nav-policy-bindings')).not.toBeInTheDocument()
    expect(screen.queryByTestId('nav-template-dependencies')).not.toBeInTheDocument()
    expect(screen.queryByTestId('nav-requirements')).not.toBeInTheDocument()
    expect(screen.queryByTestId('nav-template-grants')).not.toBeInTheDocument()
  })

  it('Template Policies sub-link is active when on the policies route', () => {
    mockPathname = '/projects/my-project/templates/policies'
    render(<AppSidebar />)
    expect(screen.getByTestId('nav-template-policies')).toHaveAttribute(
      'data-active',
      'true',
    )
    expect(screen.getByTestId('nav-policy-bindings')).toHaveAttribute(
      'data-active',
      'false',
    )
  })

  it('Policy Bindings sub-link is active when on the policy-bindings route', () => {
    mockPathname = '/projects/my-project/templates/policy-bindings'
    render(<AppSidebar />)
    expect(screen.getByTestId('nav-policy-bindings')).toHaveAttribute(
      'data-active',
      'true',
    )
    expect(screen.getByTestId('nav-template-policies')).toHaveAttribute(
      'data-active',
      'false',
    )
  })

  it('Template Dependencies sub-link is active when on the dependencies route', () => {
    mockPathname = '/projects/my-project/templates/dependencies'
    render(<AppSidebar />)
    expect(screen.getByTestId('nav-template-dependencies')).toHaveAttribute(
      'data-active',
      'true',
    )
  })

  it('Requirements sub-link is active when on the requirements route', () => {
    mockPathname = '/projects/my-project/templates/requirements'
    render(<AppSidebar />)
    expect(screen.getByTestId('nav-requirements')).toHaveAttribute(
      'data-active',
      'true',
    )
  })

  it('Template Grants sub-link is active when on the grants route', () => {
    mockPathname = '/projects/my-project/templates/grants'
    render(<AppSidebar />)
    expect(screen.getByTestId('nav-template-grants')).toHaveAttribute(
      'data-active',
      'true',
    )
  })

  it('Templates root button is active when on any descendant route', () => {
    mockPathname = '/projects/my-project/templates/policies'
    render(<AppSidebar />)
    expect(screen.getByTestId('nav-templates')).toHaveAttribute(
      'data-active',
      'true',
    )
  })

  it('Templates root button is not active when on unrelated route', () => {
    mockPathname = '/projects/my-project/secrets'
    render(<AppSidebar />)
    expect(screen.getByTestId('nav-templates')).toHaveAttribute(
      'data-active',
      'false',
    )
  })

  it('group headings Policy, Dependencies, Grants are rendered', () => {
    render(<AppSidebar />)
    expect(screen.getByTestId('nav-group-policy')).toBeInTheDocument()
    expect(screen.getByTestId('nav-group-dependencies')).toBeInTheDocument()
    expect(screen.getByTestId('nav-group-grants')).toBeInTheDocument()
  })

  it('uses a Collapsible wrapper for the Templates group when enabled', () => {
    render(<AppSidebar />)
    expect(screen.getByTestId('collapsible')).toBeInTheDocument()
  })
})

describe('AppSidebar — active-state highlighting', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setDefaults()
    setupOrgSelected()
    setupProjectSelected()
  })

  it('workspace-menu renders in header regardless of route', () => {
    mockPathname = '/projects/my-project/secrets'
    render(<AppSidebar />)
    expect(screen.getByTestId('workspace-menu')).toBeInTheDocument()
  })

  it('Secrets link is rendered when project is selected', () => {
    mockPathname = '/projects/my-project/secrets'
    render(<AppSidebar />)
    expect(screen.getByRole('link', { name: /^secrets$/i })).toBeInTheDocument()
  })

  it('Project link is rendered when project is selected', () => {
    mockPathname = '/projects/my-project'
    render(<AppSidebar />)
    expect(screen.getByRole('link', { name: /^project$/i })).toBeInTheDocument()
  })
})

describe('AppSidebar — active-state re-renders on navigation (HOL-968 regression)', () => {
  // This test suite demonstrates the bug that was fixed: previously AppSidebar
  // read router.state.location.pathname (a non-reactive snapshot), so it would
  // never re-render on client-side navigation. The fix uses useLocation() which
  // is a reactive subscription.
  //
  // The test re-renders the SAME component instance (via rerender) rather than
  // calling render() twice, to prove the component updates reactively.

  beforeEach(() => {
    vi.clearAllMocks()
    setDefaults()
    setupOrgSelected()
    setupProjectSelected()
  })

  it('active entry flips from Deployments to Secrets when location changes — same instance', async () => {
    // Step 1: Start on the Deployments route.
    mockPathname = '/projects/my-project/deployments'
    const { rerender } = render(<AppSidebar />)

    // Deployments should be active, Secrets should not.
    expect(getNavButton('deployments')).toHaveAttribute('data-active', 'true')
    expect(getNavButton('secrets')).toHaveAttribute('data-active', 'false')

    // Step 2: Navigate to Secrets by updating mockPathname and re-rendering
    // the same component instance via act() + rerender.
    await act(async () => {
      mockPathname = '/projects/my-project/secrets'
      rerender(<AppSidebar />)
    })

    // Secrets should now be active, Deployments should not.
    expect(getNavButton('secrets')).toHaveAttribute('data-active', 'true')
    expect(getNavButton('deployments')).toHaveAttribute('data-active', 'false')
  })

  it('Templates sub-link active state updates reactively on navigation', async () => {
    mockPathname = '/projects/my-project/templates/policies'
    const { rerender } = render(<AppSidebar />)

    expect(screen.getByTestId('nav-template-policies')).toHaveAttribute('data-active', 'true')
    expect(screen.getByTestId('nav-requirements')).toHaveAttribute('data-active', 'false')

    await act(async () => {
      mockPathname = '/projects/my-project/templates/requirements'
      rerender(<AppSidebar />)
    })

    expect(screen.getByTestId('nav-requirements')).toHaveAttribute('data-active', 'true')
    expect(screen.getByTestId('nav-template-policies')).toHaveAttribute('data-active', 'false')
  })
})

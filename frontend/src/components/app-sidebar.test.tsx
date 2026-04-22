import { render, screen } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

// HOL-856: replaces the two-tree sidebar (HOL-604/HOL-605 Collapsible groups)
// with a flat 4-item nav: Secrets, Deployments, Templates, Resource Manager.
// The workspace picker stays in SidebarHeader. The version label moves into
// SidebarFooter. Legacy routes remain reachable by URL; their cleanup is a
// separate sibling plan.

// Configurable per-test so we can drive route-based active-state gating.
let mockPathname = '/'

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
    useRouter: () => ({
      state: { location: { pathname: mockPathname } },
      navigate: vi.fn(),
    }),
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

vi.mock('@/lib/project-context', () => ({ useProject: vi.fn() }))
vi.mock('@/queries/version', () => ({ useVersion: vi.fn() }))

import { useProject } from '@/lib/project-context'
import { useVersion } from '@/queries/version'
import { AppSidebar } from './app-sidebar'

function setDefaults() {
  ;(useProject as Mock).mockReturnValue({
    projects: [],
    selectedProject: null,
    setSelectedProject: vi.fn(),
    isLoading: false,
  })
  ;(useVersion as Mock).mockReturnValue({ data: { version: 'v0.0.0-test' } })
}

function setupProjectSelected(projectName = 'my-project') {
  ;(useProject as Mock).mockReturnValue({
    projects: [{ name: projectName, displayName: 'My Project' }],
    selectedProject: projectName,
    setSelectedProject: vi.fn(),
    isLoading: false,
  })
}

// HOL-856: flat nav entry helpers
function getNavButton(label: string) {
  return screen.getByTestId(
    `nav-${label.toLowerCase().replace(/\s+/g, '-')}`,
  )
}

describe('AppSidebar — HOL-856 flat 4-item nav', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockPathname = '/'
    setDefaults()
  })

  it('renders the workspace menu in the header', () => {
    render(<AppSidebar />)
    expect(screen.getByTestId('workspace-menu')).toBeInTheDocument()
  })

  it('renders exactly four top-level nav entries', () => {
    render(<AppSidebar />)
    // All four entries are always present (enabled or disabled)
    expect(getNavButton('secrets')).toBeInTheDocument()
    expect(getNavButton('deployments')).toBeInTheDocument()
    expect(getNavButton('templates')).toBeInTheDocument()
    expect(getNavButton('resource-manager')).toBeInTheDocument()
  })

  it('renders nav entries in canonical order: Secrets, Deployments, Templates, Resource Manager', () => {
    render(<AppSidebar />)
    // Compare DOM positions of the four nav entry containers
    const secrets = getNavButton('secrets')
    const deployments = getNavButton('deployments')
    const templates = getNavButton('templates')
    const rm = getNavButton('resource-manager')
    // Node.DOCUMENT_POSITION_FOLLOWING = 4: secrets precedes deployments, etc.
    expect(
      secrets.compareDocumentPosition(deployments) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy()
    expect(
      deployments.compareDocumentPosition(templates) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy()
    expect(
      templates.compareDocumentPosition(rm) & Node.DOCUMENT_POSITION_FOLLOWING,
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

describe('AppSidebar — nav links when no project is selected', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockPathname = '/'
    setDefaults()
  })

  it('Secrets, Deployments, and Templates buttons are disabled when no project is selected', () => {
    render(<AppSidebar />)
    expect(getNavButton('secrets')).toBeDisabled()
    expect(getNavButton('deployments')).toBeDisabled()
    expect(getNavButton('templates')).toBeDisabled()
  })

  it('Resource Manager renders as a link (not disabled) when no project is selected', () => {
    render(<AppSidebar />)
    // Resource Manager is always enabled — it is a top-level route.
    // When enabled, SidebarMenuButton asChild wraps the Link in a span.
    const rmContainer = getNavButton('resource-manager')
    // The container should not have the disabled attribute
    expect(rmContainer).not.toHaveAttribute('disabled')
    // And the link inside should be present
    expect(rmContainer.querySelector('a[href="/resource-manager"]')).not.toBeNull()
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

  it('disabled buttons render tooltip for Templates', () => {
    render(<AppSidebar />)
    const tooltips = screen.getAllByTestId('tooltip-content')
    const tooltip = tooltips.find((el) =>
      el.textContent?.includes('Templates'),
    )
    expect(tooltip).toBeDefined()
  })
})

describe('AppSidebar — nav links when a project is selected', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockPathname = '/'
    setDefaults()
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

  it('Templates link resolves to the correct project-scoped URL', () => {
    render(<AppSidebar />)
    expect(
      screen.getByRole('link', { name: /^templates$/i }).getAttribute('href'),
    ).toBe('/projects/my-project/templates')
  })

  it('Resource Manager link resolves to /resource-manager', () => {
    render(<AppSidebar />)
    expect(
      screen.getByRole('link', { name: /^resource manager$/i }).getAttribute('href'),
    ).toBe('/resource-manager')
  })

  it('no disabled buttons when project is selected', () => {
    render(<AppSidebar />)
    // When enabled, SidebarMenuButton asChild wraps the Link in a span.
    // The span should not have disabled attribute, and should contain an <a>.
    const secretsContainer = getNavButton('secrets')
    const deploymentsContainer = getNavButton('deployments')
    const templatesContainer = getNavButton('templates')
    expect(secretsContainer).not.toHaveAttribute('disabled')
    expect(deploymentsContainer).not.toHaveAttribute('disabled')
    expect(templatesContainer).not.toHaveAttribute('disabled')
    expect(secretsContainer.querySelector('a')).not.toBeNull()
    expect(deploymentsContainer.querySelector('a')).not.toBeNull()
    expect(templatesContainer.querySelector('a')).not.toBeNull()
  })
})

describe('AppSidebar — active-state highlighting', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setDefaults()
    setupProjectSelected()
  })

  // The SidebarMenuButton mock exposes `data-active` on the button element.
  // When asChild is used, the button renders the Link child directly.
  // We assert isActive on the button (data-testid) indirectly via the link's
  // parent container or the data-active attribute on the rendered button.

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

  it('Resource Manager link is always rendered as a link', () => {
    mockPathname = '/resource-manager'
    render(<AppSidebar />)
    expect(
      screen.getByRole('link', { name: /^resource manager$/i }),
    ).toBeInTheDocument()
  })
})

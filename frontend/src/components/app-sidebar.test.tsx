import { render, screen } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

// Mock router and sidebar dependencies
const mockNavigate = vi.fn()
// Configurable per-test so we can drive route-based gating (active-state
// highlighting on the Project tree children).
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
    useRouter: () => ({ state: { location: { pathname: mockPathname } }, navigate: mockNavigate }),
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
  SidebarGroup: ({ children, ...rest }: React.HTMLAttributes<HTMLDivElement> & { children: React.ReactNode }) => (
    <div {...rest}>{children}</div>
  ),
  SidebarGroupContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SidebarGroupLabel: ({ children }: { children: React.ReactNode }) => (
    <div data-testid="sidebar-group-label">{children}</div>
  ),
  SidebarHeader: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SidebarMenu: ({ children }: { children: React.ReactNode }) => <ul>{children}</ul>,
  SidebarMenuButton: ({
    children,
    asChild,
    isActive,
    ...rest
  }: React.HTMLAttributes<HTMLElement> & { children: React.ReactNode; asChild?: boolean; isActive?: boolean }) =>
    asChild ? <>{children}</> : (
      <button data-active={isActive ? 'true' : 'false'} {...rest}>
        {children}
      </button>
    ),
  SidebarMenuItem: ({ children }: { children: React.ReactNode }) => <li>{children}</li>,
  SidebarMenuSub: ({ children }: { children: React.ReactNode }) => (
    <ul data-testid="sidebar-menu-sub">{children}</ul>
  ),
  SidebarMenuSubItem: ({ children }: { children: React.ReactNode }) => (
    <li>{children}</li>
  ),
  SidebarMenuSubButton: ({
    children,
    asChild,
    isActive,
    ...rest
  }: React.HTMLAttributes<HTMLElement> & { children: React.ReactNode; asChild?: boolean; isActive?: boolean }) => {
    const activeAttr = isActive ? 'true' : 'false'
    if (asChild) {
      // Wrap the single child in a span that carries the data-active
      // attribute so tests can assert the active state without caring how
      // the child (Link / button / etc.) renders.
      return <span data-active={activeAttr}>{children}</span>
    }
    return (
      <a data-active={activeAttr} {...rest}>
        {children}
      </a>
    )
  },
  SidebarSeparator: () => <hr />,
}))

// Flatten Collapsible so CollapsibleContent is always rendered. The primitive
// open/close state is Radix-driven and covered separately by the integration
// test in -app-sidebar.tree.test.tsx.
vi.mock('@/components/ui/collapsible', () => ({
  Collapsible: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  CollapsibleTrigger: ({
    children,
    asChild,
  }: {
    children: React.ReactNode
    asChild?: boolean
  }) => (asChild ? <>{children}</> : <button>{children}</button>),
  CollapsibleContent: ({
    children,
    ...rest
  }: React.HTMLAttributes<HTMLDivElement> & { children: React.ReactNode }) => (
    <div {...rest}>{children}</div>
  ),
}))

// Flatten Tooltip so TooltipContent renders inline; content-level assertions
// live here, hover/focus wiring lives in the integration test.
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
    <div {...rest}>{children}</div>
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
vi.mock('@/queries/project-settings', () => ({ useGetProjectSettings: vi.fn() }))

import { useOrg } from '@/lib/org-context'
import { useProject } from '@/lib/project-context'
import { useVersion } from '@/queries/version'
import { useGetProjectSettings } from '@/queries/project-settings'
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
  ;(useGetProjectSettings as Mock).mockReturnValue({ data: { deploymentsEnabled: false }, isPending: false })
}

describe('AppSidebar', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockNavigate.mockReset()
    mockPathname = '/'
    setDefaults()
  })

  it('renders the workspace menu in the header', () => {
    render(<AppSidebar />)
    expect(screen.getByTestId('workspace-menu')).toBeInTheDocument()
  })

  it('renders without a theme toggle button', () => {
    render(<AppSidebar />)
    expect(screen.queryByRole('button', { name: /toggle theme/i })).toBeNull()
  })

  it('renders no org/project nav items when no project is selected', () => {
    render(<AppSidebar />)
    expect(screen.queryByText('Organizations')).toBeNull()
    expect(screen.queryByText('Projects')).toBeNull()
  })

  it('renders version info', () => {
    render(<AppSidebar />)
    expect(screen.getByText('v0.0.0-test')).toBeDefined()
  })

  // HOL-603 moves Profile, About, and Dev Tools off the sidebar footer and
  // into the workspace menu. The footer is no longer rendered at all in this
  // phase. These regression tests guard against re-introducing those entries
  // at the sidebar level by accident.
  it('does not render an About link in the sidebar (moved to workspace menu)', () => {
    render(<AppSidebar />)
    expect(screen.queryByRole('link', { name: /^about$/i })).toBeNull()
  })

  it('does not render a Profile link in the sidebar (moved to workspace menu)', () => {
    render(<AppSidebar />)
    expect(screen.queryByRole('link', { name: /^profile$/i })).toBeNull()
  })

  it('does not render a Dev Tools link in the sidebar (moved to workspace menu)', () => {
    render(<AppSidebar />)
    expect(screen.queryByRole('link', { name: /dev tools/i })).toBeNull()
  })

  it('does not render OrgPicker or ProjectPicker dropdowns (replaced by workspace menu)', () => {
    render(<AppSidebar />)
    expect(screen.queryByTestId('org-picker')).toBeNull()
    expect(screen.queryByTestId('project-picker')).toBeNull()
  })

  it('does not render project nav links when no project is selected', () => {
    render(<AppSidebar />)
    expect(screen.queryByText('Secrets')).toBeNull()
    expect(screen.queryByText(/^settings$/i)).toBeNull()
  })

  // HOL-604: the Project tree itself is hidden when no project is selected.
  it('does not render the Project tree when no project is selected', () => {
    render(<AppSidebar />)
    expect(screen.queryByTestId('project-tree')).not.toBeInTheDocument()
    expect(screen.queryByTestId('project-tree-trigger')).not.toBeInTheDocument()
  })
})

describe('AppSidebar — org selected', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockPathname = '/'
    setDefaults()
    ;(useOrg as Mock).mockReturnValue({
      organizations: [{ name: 'my-org', displayName: 'My Org' }],
      selectedOrg: 'my-org',
      setSelectedOrg: vi.fn(),
      isLoading: false,
    })
  })

  it('renders org Settings link labeled "Org Settings" with correct href', () => {
    render(<AppSidebar />)
    const link = screen.getByRole('link', { name: /org settings/i })
    expect(link.getAttribute('href')).toBe('/orgs/my-org/settings/')
  })

  it('renders org Projects link with correct href', () => {
    render(<AppSidebar />)
    const link = screen.getByRole('link', { name: /projects/i })
    expect(link.getAttribute('href')).toBe('/orgs/my-org/projects')
  })

  it('renders org display name as group label', () => {
    render(<AppSidebar />)
    const labels = screen.getAllByTestId('sidebar-group-label')
    const labelTexts = labels.map((l) => l.textContent)
    expect(labelTexts).toContain('My Org')
  })

  it('shows "Org Settings" label instead of "Settings" in org nav', () => {
    render(<AppSidebar />)
    expect(screen.queryByRole('link', { name: /^org settings$/i })).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /^settings$/i })).toBeNull()
  })

  it('hides org nav group when selectedOrg is null', () => {
    ;(useOrg as Mock).mockReturnValue({
      organizations: [],
      selectedOrg: null,
      setSelectedOrg: vi.fn(),
      isLoading: false,
    })
    render(<AppSidebar />)
    expect(screen.queryByTestId('sidebar-group-label')).toBeNull()
  })
})

// HOL-604: the project section becomes a collapsible tree labeled "Project"
// with a tooltip surfacing the display name + slug. Children render inside a
// SidebarMenuSub in the canonical order: Secrets, Deployments, Templates,
// Settings.
//
// This suite flattens the Collapsible / Tooltip primitives so content-level
// assertions (order, routing, active state, tooltip contents) are direct.
// The real click-toggle behavior over the asChild prop-merging chain is
// covered by -app-sidebar.tree.test.tsx which renders with the unmocked
// primitives.
describe('AppSidebar — Project tree (HOL-604)', () => {
  function setupProjectSelected() {
    ;(useOrg as Mock).mockReturnValue({
      organizations: [{ name: 'my-org', displayName: 'My Org' }],
      selectedOrg: 'my-org',
      setSelectedOrg: vi.fn(),
      isLoading: false,
    })
    ;(useProject as Mock).mockReturnValue({
      projects: [{ name: 'my-project', displayName: 'My Project' }],
      selectedProject: 'my-project',
      setSelectedProject: vi.fn(),
      isLoading: false,
    })
  }

  beforeEach(() => {
    vi.clearAllMocks()
    mockPathname = '/'
    setDefaults()
    setupProjectSelected()
  })

  it('renders the Project tree when a project is selected', () => {
    render(<AppSidebar />)
    expect(screen.getByTestId('project-tree')).toBeInTheDocument()
    expect(screen.getByTestId('project-tree-trigger')).toBeInTheDocument()
  })

  it('uses a static "Project" label instead of the project display name', () => {
    render(<AppSidebar />)
    const trigger = screen.getByTestId('project-tree-trigger')
    expect(trigger.textContent).toContain('Project')
    // Display name belongs in the tooltip, not the label itself.
    expect(trigger.textContent).not.toContain('My Project')
  })

  it('renders a tooltip whose first line is the display name and second is the slug', () => {
    render(<AppSidebar />)
    const tooltip = screen.getByTestId('project-tree-tooltip')
    // Direct-child divs carry the two lines; nested descendants (if any)
    // are intentionally excluded to lock in the order.
    const lineDivs = Array.from(tooltip.children).filter(
      (el): el is HTMLElement => el.tagName === 'DIV',
    )
    expect(lineDivs.map((el) => el.textContent)).toEqual(['My Project', 'my-project'])
  })

  it('falls back to the slug for the display-name line when displayName is empty', () => {
    ;(useProject as Mock).mockReturnValue({
      projects: [{ name: 'my-project', displayName: '' }],
      selectedProject: 'my-project',
      setSelectedProject: vi.fn(),
      isLoading: false,
    })
    render(<AppSidebar />)
    const tooltip = screen.getByTestId('project-tree-tooltip')
    const lineDivs = Array.from(tooltip.children).filter(
      (el): el is HTMLElement => el.tagName === 'DIV',
    )
    // Both lines collapse to the slug when there is no displayName.
    expect(lineDivs.map((el) => el.textContent)).toEqual(['my-project', 'my-project'])
  })

  it('renders only Secrets and Settings (no Deployments/Templates) when deploymentsEnabled is false', () => {
    ;(useGetProjectSettings as Mock).mockReturnValue({ data: { deploymentsEnabled: false }, isPending: false })
    render(<AppSidebar />)
    const sub = screen.getByTestId('sidebar-menu-sub')
    const labels = Array.from(sub.querySelectorAll('li')).map((li) => li.textContent?.trim())
    expect(labels).toEqual(['Secrets', 'Settings'])
  })

  it('renders children in canonical order Secrets, Deployments, Templates, Settings when deploymentsEnabled', () => {
    ;(useGetProjectSettings as Mock).mockReturnValue({ data: { deploymentsEnabled: true }, isPending: false })
    render(<AppSidebar />)
    const sub = screen.getByTestId('sidebar-menu-sub')
    const labels = Array.from(sub.querySelectorAll('li')).map((li) => li.textContent?.trim())
    expect(labels).toEqual(['Secrets', 'Deployments', 'Templates', 'Settings'])
  })

  it('routes each child link to the existing /projects/$projectName/... URL', () => {
    ;(useGetProjectSettings as Mock).mockReturnValue({ data: { deploymentsEnabled: true }, isPending: false })
    render(<AppSidebar />)
    expect(screen.getByRole('link', { name: /^secrets$/i }).getAttribute('href')).toBe(
      '/projects/my-project/secrets',
    )
    expect(screen.getByRole('link', { name: /^deployments$/i }).getAttribute('href')).toBe(
      '/projects/my-project/deployments',
    )
    expect(screen.getByRole('link', { name: /^templates$/i }).getAttribute('href')).toBe(
      '/projects/my-project/templates',
    )
    // The child Settings link routes to the project-scope settings route;
    // the org nav still exposes a separate Org Settings link.
    const settingsLinks = screen.getAllByRole('link', { name: /^settings$/i })
    expect(settingsLinks).toHaveLength(1)
    expect(settingsLinks[0].getAttribute('href')).toBe('/projects/my-project/settings/')
  })

  it('the org nav Org Settings link continues to render alongside the Project Settings child', () => {
    ;(useGetProjectSettings as Mock).mockReturnValue({ data: { deploymentsEnabled: true }, isPending: false })
    render(<AppSidebar />)
    expect(screen.getByRole('link', { name: /^org settings$/i })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /^settings$/i })).toBeInTheDocument()
  })

  it('does not render the Project tree when selectedProject is cleared', () => {
    ;(useProject as Mock).mockReturnValue({
      projects: [{ name: 'my-project', displayName: 'My Project' }],
      selectedProject: null,
      setSelectedProject: vi.fn(),
      isLoading: false,
    })
    render(<AppSidebar />)
    expect(screen.queryByTestId('project-tree')).not.toBeInTheDocument()
  })

  // Active-state highlighting: the `isActive` prop on each child is surfaced
  // on the wrapping <span data-active="..."> by the mock so we can assert
  // the route-based gate without caring about the internal primitive.
  describe('active-state highlighting', () => {
    beforeEach(() => {
      ;(useGetProjectSettings as Mock).mockReturnValue({ data: { deploymentsEnabled: true }, isPending: false })
    })

    function activeOf(linkName: RegExp) {
      const link = screen.getByRole('link', { name: linkName })
      // The mock wraps the <a> in a <span data-active="..."> when asChild
      // is used (SidebarMenuSubButton asChild -> Link).
      return link.parentElement?.getAttribute('data-active')
    }

    it('marks the Secrets child active when the pathname is /projects/<name>/secrets', () => {
      mockPathname = '/projects/my-project/secrets'
      render(<AppSidebar />)
      expect(activeOf(/^secrets$/i)).toBe('true')
      expect(activeOf(/^deployments$/i)).toBe('false')
      expect(activeOf(/^settings$/i)).toBe('false')
    })

    it('marks the Settings child active when the pathname is /projects/<name>/settings (trailing slash stripped)', () => {
      mockPathname = '/projects/my-project/settings'
      render(<AppSidebar />)
      expect(activeOf(/^settings$/i)).toBe('true')
      expect(activeOf(/^secrets$/i)).toBe('false')
    })

    it('marks only the matching child active when the pathname is a deeper sub-route', () => {
      // Secrets detail page, e.g. /projects/my-project/secrets/foo — the
      // Secrets child should be active, not the other children.
      mockPathname = '/projects/my-project/secrets/api-key'
      render(<AppSidebar />)
      expect(activeOf(/^secrets$/i)).toBe('true')
      expect(activeOf(/^deployments$/i)).toBe('false')
      expect(activeOf(/^templates$/i)).toBe('false')
      expect(activeOf(/^settings$/i)).toBe('false')
    })
  })
})

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
  SidebarMenuSub: ({ children, ...rest }: React.HTMLAttributes<HTMLElement> & { children: React.ReactNode }) => (
    <ul {...rest}>{children}</ul>
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

  // HOL-605: the Organization tree is hidden when no org is selected.
  it('does not render the Organization tree when no org is selected', () => {
    render(<AppSidebar />)
    expect(screen.queryByTestId('org-tree')).not.toBeInTheDocument()
    expect(screen.queryByTestId('org-tree-trigger')).not.toBeInTheDocument()
  })
})

// HOL-605: the organization section becomes a collapsible tree labeled
// "Organization" with a tooltip surfacing the org display name + identifier.
// Children render inside a SidebarMenuSub in the canonical order: Resources,
// Templates, Template Policies.
//
// This suite flattens the Collapsible / Tooltip primitives so content-level
// assertions (order, routing, active state, tooltip contents) are direct.
// The real click-toggle behavior over the asChild prop-merging chain is
// covered by the integration tests for HOL-604 (the same prop-merging chain
// is reused here verbatim).
describe('AppSidebar — Organization tree (HOL-605)', () => {
  function setupOrgSelected() {
    ;(useOrg as Mock).mockReturnValue({
      organizations: [{ name: 'my-org', displayName: 'My Org' }],
      selectedOrg: 'my-org',
      setSelectedOrg: vi.fn(),
      isLoading: false,
    })
  }

  beforeEach(() => {
    vi.clearAllMocks()
    mockPathname = '/'
    setDefaults()
    setupOrgSelected()
  })

  it('renders the Organization tree when an org is selected', () => {
    render(<AppSidebar />)
    expect(screen.getByTestId('org-tree')).toBeInTheDocument()
    expect(screen.getByTestId('org-tree-trigger')).toBeInTheDocument()
  })

  it('uses a static "Organization" label instead of the org display name', () => {
    render(<AppSidebar />)
    const trigger = screen.getByTestId('org-tree-trigger')
    expect(trigger.textContent).toContain('Organization')
    // Display name belongs in the tooltip, not the label itself.
    expect(trigger.textContent).not.toContain('My Org')
  })

  it('renders a tooltip whose first line is the display name and second is the identifier', () => {
    render(<AppSidebar />)
    const tooltip = screen.getByTestId('org-tree-tooltip')
    const lineDivs = Array.from(tooltip.children).filter(
      (el): el is HTMLElement => el.tagName === 'DIV',
    )
    expect(lineDivs.map((el) => el.textContent)).toEqual(['My Org', 'my-org'])
  })

  it('falls back to the identifier for the display-name line when displayName is empty', () => {
    ;(useOrg as Mock).mockReturnValue({
      organizations: [{ name: 'my-org', displayName: '' }],
      selectedOrg: 'my-org',
      setSelectedOrg: vi.fn(),
      isLoading: false,
    })
    render(<AppSidebar />)
    const tooltip = screen.getByTestId('org-tree-tooltip')
    const lineDivs = Array.from(tooltip.children).filter(
      (el): el is HTMLElement => el.tagName === 'DIV',
    )
    expect(lineDivs.map((el) => el.textContent)).toEqual(['my-org', 'my-org'])
  })

  it('renders children in canonical order Resources, Templates, Template Policies', () => {
    render(<AppSidebar />)
    const content = screen.getByTestId('org-tree-content')
    const labels = Array.from(content.querySelectorAll('li')).map((li) => li.textContent?.trim())
    expect(labels).toEqual(['Resources', 'Templates', 'Template Policies'])
  })

  it('routes each Organization child link to the correct /orgs/$orgName/... URL', () => {
    render(<AppSidebar />)
    expect(screen.getByRole('link', { name: /^resources$/i }).getAttribute('href')).toBe(
      '/orgs/my-org/resources',
    )
    expect(screen.getByRole('link', { name: /^templates$/i }).getAttribute('href')).toBe(
      '/orgs/my-org/templates',
    )
    expect(screen.getByRole('link', { name: /^template policies$/i }).getAttribute('href')).toBe(
      '/orgs/my-org/template-policies',
    )
  })

  it('does not render the former Folders, Projects, or Org Settings sidebar entries', () => {
    render(<AppSidebar />)
    expect(screen.queryByRole('link', { name: /^folders$/i })).toBeNull()
    expect(screen.queryByRole('link', { name: /^projects$/i })).toBeNull()
    // "Org Settings" moved to the workspace menu (HOL-603).
    expect(screen.queryByRole('link', { name: /^org settings$/i })).toBeNull()
  })

  it('does not render the Organization tree when selectedOrg is null', () => {
    ;(useOrg as Mock).mockReturnValue({
      organizations: [],
      selectedOrg: null,
      setSelectedOrg: vi.fn(),
      isLoading: false,
    })
    render(<AppSidebar />)
    expect(screen.queryByTestId('org-tree')).not.toBeInTheDocument()
  })

  // Active-state highlighting: the `isActive` prop on each child is surfaced
  // on the wrapping <span data-active="..."> by the mock.
  describe('active-state highlighting', () => {
    function activeOf(linkName: RegExp) {
      const link = screen.getByRole('link', { name: linkName })
      return link.parentElement?.getAttribute('data-active')
    }

    it('marks the Resources child active when the pathname is /orgs/<name>/resources', () => {
      mockPathname = '/orgs/my-org/resources'
      render(<AppSidebar />)
      expect(activeOf(/^resources$/i)).toBe('true')
      expect(activeOf(/^templates$/i)).toBe('false')
      expect(activeOf(/^template policies$/i)).toBe('false')
    })

    it('marks the Templates child active when the pathname is /orgs/<name>/templates', () => {
      mockPathname = '/orgs/my-org/templates'
      render(<AppSidebar />)
      expect(activeOf(/^templates$/i)).toBe('true')
      expect(activeOf(/^resources$/i)).toBe('false')
      expect(activeOf(/^template policies$/i)).toBe('false')
    })

    it('marks the Template Policies child active when the pathname is /orgs/<name>/template-policies', () => {
      mockPathname = '/orgs/my-org/template-policies'
      render(<AppSidebar />)
      expect(activeOf(/^template policies$/i)).toBe('true')
      expect(activeOf(/^resources$/i)).toBe('false')
      expect(activeOf(/^templates$/i)).toBe('false')
    })

    it('marks only the matching child active when the pathname is a deeper sub-route', () => {
      mockPathname = '/orgs/my-org/template-policies/my-policy'
      render(<AppSidebar />)
      expect(activeOf(/^template policies$/i)).toBe('true')
      expect(activeOf(/^resources$/i)).toBe('false')
      expect(activeOf(/^templates$/i)).toBe('false')
    })
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
  // Project tree suite isolates the Project tree by leaving selectedOrg at
  // the default (null) so the Organization tree does not render and
  // duplicate link names (e.g. "Templates") stay unambiguous.
  function setupProjectSelected() {
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
    const content = screen.getByTestId('project-tree-content')
    const labels = Array.from(content.querySelectorAll('li')).map((li) => li.textContent?.trim())
    expect(labels).toEqual(['Secrets', 'Settings'])
  })

  it('renders children in canonical order Secrets, Deployments, Templates, Settings when deploymentsEnabled', () => {
    ;(useGetProjectSettings as Mock).mockReturnValue({ data: { deploymentsEnabled: true }, isPending: false })
    render(<AppSidebar />)
    const content = screen.getByTestId('project-tree-content')
    const labels = Array.from(content.querySelectorAll('li')).map((li) => li.textContent?.trim())
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
    // The child Settings link routes to the project-scope settings route.
    // After HOL-605, Org Settings is no longer in the sidebar; only the
    // project-scoped Settings remains.
    const settingsLinks = screen.getAllByRole('link', { name: /^settings$/i })
    expect(settingsLinks).toHaveLength(1)
    expect(settingsLinks[0].getAttribute('href')).toBe('/projects/my-project/settings/')
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

// HOL-605: assert that when BOTH an org and a project are selected, the
// Project tree renders BEFORE the Organization tree (AC: "sits below the
// Project tree"). This locks the canonical vertical order into place.
describe('AppSidebar — Project tree precedes Organization tree (HOL-605)', () => {
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
    ;(useProject as Mock).mockReturnValue({
      projects: [{ name: 'my-project', displayName: 'My Project' }],
      selectedProject: 'my-project',
      setSelectedProject: vi.fn(),
      isLoading: false,
    })
    ;(useGetProjectSettings as Mock).mockReturnValue({ data: { deploymentsEnabled: true }, isPending: false })
  })

  it('renders the Project tree before the Organization tree in the DOM order', () => {
    render(<AppSidebar />)
    const project = screen.getByTestId('project-tree')
    const org = screen.getByTestId('org-tree')
    // Node.DOCUMENT_POSITION_FOLLOWING = 4. If project precedes org, the
    // comparison should include the FOLLOWING bit when checked from project
    // to org.
    expect(project.compareDocumentPosition(org) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })
})

import { render, screen } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

// Mock TanStack Router. The mocked pathname is configurable per-test via the
// `mockPathname` module-scoped variable so we can exercise the pathname-based
// gate for the Template Policies sidebar entry (HOL-558).
let mockPathname = '/orgs/test-org/projects'

vi.mock('@tanstack/react-router', () => ({
  Link: ({ children, ...props }: React.AnchorHTMLAttributes<HTMLAnchorElement> & { children: React.ReactNode }) =>
    <a {...props}>{children}</a>,
  useRouter: () => ({ state: { location: { pathname: mockPathname } } }),
}))

// Mock org and project contexts
vi.mock('@/lib/org-context', () => ({
  useOrg: vi.fn(),
}))
vi.mock('@/lib/project-context', () => ({
  useProject: vi.fn(),
}))
vi.mock('@/lib/console-config', () => ({
  getConsoleConfig: () => ({ devToolsEnabled: false }),
}))
vi.mock('@/queries/version', () => ({
  useVersion: () => ({ data: { version: '0.1.0' } }),
}))
vi.mock('@/queries/project-settings', () => ({
  useGetProjectSettings: () => ({ data: null }),
}))

// Mock sidebar UI components to simplify rendering
vi.mock('@/components/ui/sidebar', () => ({
  Sidebar: ({ children }: { children: React.ReactNode }) => <div data-testid="sidebar">{children}</div>,
  SidebarContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SidebarFooter: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SidebarGroup: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SidebarGroupContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SidebarGroupLabel: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SidebarHeader: ({ children, ...props }: React.HTMLAttributes<HTMLDivElement> & { children: React.ReactNode }) => <div {...props}>{children}</div>,
  SidebarMenu: ({ children }: { children: React.ReactNode }) => <ul data-testid="sidebar-menu">{children}</ul>,
  SidebarMenuButton: ({ children, ...props }: React.HTMLAttributes<HTMLDivElement> & { children: React.ReactNode; asChild?: boolean; isActive?: boolean }) => <div {...props}>{children}</div>,
  SidebarMenuItem: ({ children }: { children: React.ReactNode }) => <li>{children}</li>,
  SidebarSeparator: () => <hr />,
}))

vi.mock('@/components/ui/dropdown-menu', () => ({
  DropdownMenu: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DropdownMenuContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DropdownMenuItem: ({ children, onClick }: { children: React.ReactNode; onClick?: () => void }) => <div onClick={onClick}>{children}</div>,
  DropdownMenuSeparator: () => <hr />,
  DropdownMenuTrigger: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}))

vi.mock('@/components/ui/button', () => ({
  Button: ({ children, ...props }: React.ButtonHTMLAttributes<HTMLButtonElement> & { children: React.ReactNode; variant?: string; size?: string }) => <button {...props}>{children}</button>,
}))

vi.mock('@/components/create-org-dialog', () => ({
  CreateOrgDialog: () => null,
}))
vi.mock('@/components/create-project-dialog', () => ({
  CreateProjectDialog: () => null,
}))

import { useOrg } from '@/lib/org-context'
import { useProject } from '@/lib/project-context'
import { AppSidebar } from './app-sidebar'

describe('AppSidebar', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    // Reset the mocked pathname before every test so tests that rely on the
    // default org-scope path don't bleed into each other.
    mockPathname = '/orgs/test-org/projects'
  })

  it('renders Folders before Projects in the org nav', () => {
    ;(useOrg as Mock).mockReturnValue({
      selectedOrg: 'test-org',
      organizations: [{ name: 'test-org', displayName: 'Test Org' }],
      setSelectedOrg: vi.fn(),
      isLoading: false,
    })
    ;(useProject as Mock).mockReturnValue({
      projects: [],
      selectedProject: null,
      setSelectedProject: vi.fn(),
      isLoading: false,
    })

    render(<AppSidebar />)

    const foldersItem = screen.getByText('Folders')
    const projectsItem = screen.getByText('Projects')

    // Both items should be rendered
    expect(foldersItem).toBeInTheDocument()
    expect(projectsItem).toBeInTheDocument()

    // Folders should appear before Projects in DOM order
    const sidebarMenus = screen.getAllByTestId('sidebar-menu')
    // The org nav menu is the first menu in SidebarContent
    const orgMenu = sidebarMenus[0]
    const items = orgMenu.querySelectorAll('li')
    const labels = Array.from(items).map((li) => li.textContent)

    expect(labels.indexOf('Folders')).toBeLessThan(labels.indexOf('Projects'))
  })

  // HOL-558 sidebar visibility guarantee: Template Policies appears under the
  // org nav on folder and org detail routes but NEVER under the project nav.
  // Policies are a folder/org-only concept and must not look authorable from
  // within a project.
  describe('Template Policies sidebar visibility (HOL-558)', () => {
    it('renders a Template Policies entry under the org nav when an org is selected', () => {
      ;(useOrg as Mock).mockReturnValue({
        selectedOrg: 'test-org',
        organizations: [{ name: 'test-org', displayName: 'Test Org' }],
        setSelectedOrg: vi.fn(),
        isLoading: false,
      })
      ;(useProject as Mock).mockReturnValue({
        projects: [],
        selectedProject: null,
        setSelectedProject: vi.fn(),
        isLoading: false,
      })

      render(<AppSidebar />)
      const link = screen.getByText('Template Policies')
      expect(link).toBeInTheDocument()
    })

    it('does NOT render Template Policies under the project nav (folder/org-only)', () => {
      ;(useOrg as Mock).mockReturnValue({
        selectedOrg: 'test-org',
        organizations: [{ name: 'test-org', displayName: 'Test Org' }],
        setSelectedOrg: vi.fn(),
        isLoading: false,
      })
      ;(useProject as Mock).mockReturnValue({
        projects: [{ name: 'test-project', displayName: 'Test Project' }],
        selectedProject: 'test-project',
        setSelectedProject: vi.fn(),
        isLoading: false,
      })

      render(<AppSidebar />)

      // The project nav is the second SidebarMenu under SidebarContent.
      const sidebarMenus = screen.getAllByTestId('sidebar-menu')
      // There are at least 2 menus: org nav + project nav. The project nav is
      // always the last sidebar menu inside SidebarContent in the current
      // layout (the footer menu is rendered outside SidebarContent).
      const projectMenu = sidebarMenus[1]
      expect(projectMenu).toBeDefined()

      const projectLabels = Array.from(projectMenu.querySelectorAll('li')).map(
        (li) => li.textContent,
      )
      expect(projectLabels).not.toContain('Template Policies')
    })

    // Regression test for codex review round 1 (project-route focus): when
    // the user is actually on a /projects/... route the Template Policies
    // tab must be hidden everywhere in the sidebar because policies are
    // not a project concept. The original gating relied on selectedOrg
    // only, which left the tab visible on project detail routes where
    // the org nav is still rendered for breadcrumb navigation.
    it('hides Template Policies when pathname is a /projects/... route', () => {
      mockPathname = '/projects/test-project/secrets'
      ;(useOrg as Mock).mockReturnValue({
        selectedOrg: 'test-org',
        organizations: [{ name: 'test-org', displayName: 'Test Org' }],
        setSelectedOrg: vi.fn(),
        isLoading: false,
      })
      ;(useProject as Mock).mockReturnValue({
        projects: [{ name: 'test-project', displayName: 'Test Project' }],
        selectedProject: 'test-project',
        setSelectedProject: vi.fn(),
        isLoading: false,
      })

      render(<AppSidebar />)

      // Assert against the entire sidebar DOM, not just the project nav, so
      // we catch regressions where the tab sneaks back into the org nav.
      expect(screen.queryByText('Template Policies')).not.toBeInTheDocument()
    })

    // Regression test for codex review round 2 (sticky selectedProject):
    // `selectedProject` in ProjectProvider persists across navigations
    // within the same org (it is only cleared when the org changes). If
    // the sidebar gates Template Policies on `!selectedProject`, a user
    // who visits a project route and then clicks Folders / Projects /
    // Org Settings in the same org keeps the tab hidden even though they
    // are back on an org-scope route. The pathname-based gate fixes this
    // by looking at the actual route rather than context state.
    it('shows Template Policies on org-scope routes even when selectedProject is still set', () => {
      // User clicked Folders after visiting a project; selectedProject is
      // still set from the prior visit but pathname is now an org route.
      mockPathname = '/orgs/test-org/folders'
      ;(useOrg as Mock).mockReturnValue({
        selectedOrg: 'test-org',
        organizations: [{ name: 'test-org', displayName: 'Test Org' }],
        setSelectedOrg: vi.fn(),
        isLoading: false,
      })
      ;(useProject as Mock).mockReturnValue({
        projects: [{ name: 'test-project', displayName: 'Test Project' }],
        selectedProject: 'test-project',
        setSelectedProject: vi.fn(),
        isLoading: false,
      })

      render(<AppSidebar />)

      expect(screen.getByText('Template Policies')).toBeInTheDocument()
    })
  })
})

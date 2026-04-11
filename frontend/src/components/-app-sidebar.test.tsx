import { render, screen } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

// Mock TanStack Router
vi.mock('@tanstack/react-router', () => ({
  Link: ({ children, ...props }: React.AnchorHTMLAttributes<HTMLAnchorElement> & { children: React.ReactNode }) =>
    <a {...props}>{children}</a>,
  useRouter: () => ({ state: { location: { pathname: '/orgs/test-org/projects' } } }),
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
})

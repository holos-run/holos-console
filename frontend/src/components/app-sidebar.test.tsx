import { render, screen } from '@testing-library/react'
import { vi } from 'vitest'
import React from 'react'

// Mock router and sidebar dependencies
vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    Link: ({ children }: { children: React.ReactNode }) => <>{children}</>,
    useRouter: () => ({ state: { location: { pathname: '/' } } }),
  }
})

vi.mock('@/components/ui/sidebar', () => ({
  Sidebar: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SidebarContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SidebarFooter: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SidebarGroup: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SidebarGroupContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SidebarHeader: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SidebarMenu: ({ children }: { children: React.ReactNode }) => <ul>{children}</ul>,
  SidebarMenuButton: ({ children }: { children: React.ReactNode }) => <li>{children}</li>,
  SidebarMenuItem: ({ children }: { children: React.ReactNode }) => <li>{children}</li>,
  SidebarSeparator: () => <hr />,
}))

vi.mock('@/components/ui/dropdown-menu', () => ({
  DropdownMenu: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DropdownMenuContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DropdownMenuItem: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DropdownMenuTrigger: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}))

vi.mock('@/lib/org-context', () => ({
  useOrg: () => ({ organizations: [], selectedOrg: null, setSelectedOrg: vi.fn(), isLoading: false }),
}))

vi.mock('@/lib/project-context', () => ({
  useProject: () => ({ projects: [], selectedProject: null, setSelectedProject: vi.fn(), isLoading: false }),
}))

vi.mock('@/queries/version', () => ({
  useVersion: () => ({ data: { version: 'v0.0.0-test' } }),
}))

import { AppSidebar } from './app-sidebar'

describe('AppSidebar', () => {
  it('renders without a theme toggle button', () => {
    render(<AppSidebar />)
    expect(screen.queryByRole('button', { name: /toggle theme/i })).toBeNull()
  })

  it('renders navigation items', () => {
    render(<AppSidebar />)
    expect(screen.getByText('Organizations')).toBeDefined()
    expect(screen.getByText('Projects')).toBeDefined()
  })

  it('renders version info', () => {
    render(<AppSidebar />)
    expect(screen.getByText('v0.0.0-test')).toBeDefined()
  })
})

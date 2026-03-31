import { render, screen } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

// Mock router and sidebar dependencies
vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    Link: ({ children }: { children: React.ReactNode }) => <>{children}</>,
    useRouter: () => ({ state: { location: { pathname: '/' } } }),
    useNavigate: () => vi.fn(),
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

describe('AppSidebar', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setDefaults()
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

  it('renders About link in sidebar footer', () => {
    render(<AppSidebar />)
    expect(screen.getByText('About')).toBeInTheDocument()
  })

  it('renders Profile link in sidebar footer', () => {
    render(<AppSidebar />)
    expect(screen.getByText('Profile')).toBeInTheDocument()
  })

  it('About appears before Profile in DOM order', () => {
    render(<AppSidebar />)
    const items = screen.getAllByRole('listitem')
    const aboutIdx = items.findIndex((el) => el.textContent?.includes('About'))
    const profileIdx = items.findIndex((el) => el.textContent?.includes('Profile'))
    expect(aboutIdx).toBeGreaterThanOrEqual(0)
    expect(profileIdx).toBeGreaterThan(aboutIdx)
  })

  it('does not render project nav links when no project is selected', () => {
    render(<AppSidebar />)
    expect(screen.queryByText('Secrets')).toBeNull()
    expect(screen.queryByText('Settings')).toBeNull()
  })
})

describe('AppSidebar — org selected', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setDefaults()
    ;(useOrg as Mock).mockReturnValue({
      organizations: [{ name: 'my-org', displayName: 'My Org' }],
      selectedOrg: 'my-org',
      setSelectedOrg: vi.fn(),
      isLoading: false,
    })
  })

  it('renders project picker when an org is selected', () => {
    render(<AppSidebar />)
    // ProjectPicker shows when selectedOrg is set; the picker button label defaults to All Projects.
    // Use getAllByText since the DropdownMenuContent mock also renders the menu item text.
    const matches = screen.getAllByText('All Projects')
    expect(matches.length).toBeGreaterThanOrEqual(1)
  })
})

describe('AppSidebar — project selected', () => {
  beforeEach(() => {
    vi.clearAllMocks()
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
  })

  it('renders Secrets nav link when a project is selected', () => {
    render(<AppSidebar />)
    expect(screen.getByText('Secrets')).toBeInTheDocument()
  })

  it('renders Settings nav link when a project is selected', () => {
    render(<AppSidebar />)
    expect(screen.getByText('Settings')).toBeInTheDocument()
  })
})

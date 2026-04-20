import { render, screen } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

// Mock router and sidebar dependencies
const mockNavigate = vi.fn()

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
    useRouter: () => ({ state: { location: { pathname: '/' } }, navigate: mockNavigate }),
    useNavigate: () => vi.fn(),
  }
})

vi.mock('@/components/ui/sidebar', () => ({
  Sidebar: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SidebarContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SidebarGroup: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SidebarGroupContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SidebarGroupLabel: ({ children }: { children: React.ReactNode }) => (
    <div data-testid="sidebar-group-label">{children}</div>
  ),
  SidebarHeader: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SidebarMenu: ({ children }: { children: React.ReactNode }) => <ul>{children}</ul>,
  SidebarMenuButton: ({ children, asChild }: { children: React.ReactNode; asChild?: boolean }) =>
    asChild ? <>{children}</> : <li>{children}</li>,
  SidebarMenuItem: ({ children }: { children: React.ReactNode }) => <li>{children}</li>,
  SidebarSeparator: () => <hr />,
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
    expect(screen.queryByText('Project Settings')).toBeNull()
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

  it('renders project Settings nav link labeled "Project Settings" when a project is selected', () => {
    render(<AppSidebar />)
    expect(screen.getByRole('link', { name: /^project settings$/i })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /^org settings$/i })).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /^settings$/i })).toBeNull()
  })

  it('project Settings link points to /projects/$projectName/settings', () => {
    render(<AppSidebar />)
    const links = screen.getAllByRole('link', { name: /project settings/i })
    const projectSettingsLink = links.find((l) =>
      l.getAttribute('href')?.startsWith('/projects/'),
    )
    expect(projectSettingsLink?.getAttribute('href')).toBe('/projects/my-project/settings/')
  })

  it('renders project display name as group label in project nav section', () => {
    render(<AppSidebar />)
    const labels = screen.getAllByTestId('sidebar-group-label')
    const labelTexts = labels.map((l) => l.textContent)
    expect(labelTexts).toContain('My Project')
  })

  it('org nav group is also visible when a project is selected', () => {
    render(<AppSidebar />)
    const labels = screen.getAllByTestId('sidebar-group-label')
    const labelTexts = labels.map((l) => l.textContent)
    expect(labelTexts).toContain('My Org')
  })
})

describe('AppSidebar — Templates nav item conditional visibility', () => {
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
    setDefaults()
    setupProjectSelected()
  })

  it('does not show Templates nav when deploymentsEnabled is false', () => {
    ;(useGetProjectSettings as Mock).mockReturnValue({ data: { deploymentsEnabled: false }, isPending: false })
    render(<AppSidebar />)
    expect(screen.queryByRole('link', { name: /^templates$/i })).not.toBeInTheDocument()
  })

  it('shows Templates nav when deploymentsEnabled is true', () => {
    ;(useGetProjectSettings as Mock).mockReturnValue({ data: { deploymentsEnabled: true }, isPending: false })
    render(<AppSidebar />)
    expect(screen.getByRole('link', { name: /^templates$/i })).toBeInTheDocument()
  })

  it('Templates link points to /projects/$projectName/templates', () => {
    ;(useGetProjectSettings as Mock).mockReturnValue({ data: { deploymentsEnabled: true }, isPending: false })
    render(<AppSidebar />)
    const link = screen.getByRole('link', { name: /^templates$/i })
    expect(link.getAttribute('href')).toBe('/projects/my-project/templates')
  })

  it('does not show Deployments nav when deploymentsEnabled is false', () => {
    ;(useGetProjectSettings as Mock).mockReturnValue({ data: { deploymentsEnabled: false }, isPending: false })
    render(<AppSidebar />)
    expect(screen.queryByRole('link', { name: /^deployments$/i })).not.toBeInTheDocument()
  })

  it('shows Deployments nav when deploymentsEnabled is true', () => {
    ;(useGetProjectSettings as Mock).mockReturnValue({ data: { deploymentsEnabled: true }, isPending: false })
    render(<AppSidebar />)
    expect(screen.getByRole('link', { name: /^deployments$/i })).toBeInTheDocument()
  })

  it('Deployments link points to /projects/$projectName/deployments', () => {
    ;(useGetProjectSettings as Mock).mockReturnValue({ data: { deploymentsEnabled: true }, isPending: false })
    render(<AppSidebar />)
    const link = screen.getByRole('link', { name: /^deployments$/i })
    expect(link.getAttribute('href')).toBe('/projects/my-project/deployments')
  })
})

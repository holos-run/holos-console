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
  SidebarFooter: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
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

vi.mock('@/components/ui/dropdown-menu', () => ({
  DropdownMenu: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DropdownMenuContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DropdownMenuItem: ({
    children,
    onClick,
  }: {
    children: React.ReactNode
    onClick?: () => void
  }) => <div onClick={onClick}>{children}</div>,
  DropdownMenuTrigger: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DropdownMenuSeparator: () => <hr />,
}))

vi.mock('@/lib/org-context', () => ({ useOrg: vi.fn() }))
vi.mock('@/lib/project-context', () => ({ useProject: vi.fn() }))
vi.mock('@/queries/version', () => ({ useVersion: vi.fn() }))
vi.mock('@/queries/project-settings', () => ({ useGetProjectSettings: vi.fn() }))
vi.mock('@/queries/organizations', () => ({
  useListOrganizations: vi.fn().mockReturnValue({ data: { organizations: [] }, isLoading: false }),
  useCreateOrganization: vi.fn().mockReturnValue({ mutateAsync: vi.fn(), isPending: false }),
}))
vi.mock('@/queries/projects', () => ({
  useListProjects: vi.fn().mockReturnValue({ data: { projects: [] }, isLoading: false }),
  useCreateProject: vi.fn().mockReturnValue({ mutateAsync: vi.fn(), isPending: false }),
}))
vi.mock('@/components/create-org-dialog', () => ({
  CreateOrgDialog: () => <div data-testid="create-org-dialog" />,
}))
vi.mock('@/components/create-project-dialog', () => ({
  CreateProjectDialog: () => <div data-testid="create-project-dialog" />,
}))

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

  it('renders project picker area when an org is selected', () => {
    render(<AppSidebar />)
    // With no projects, ProjectPicker shows the empty state with "New Project" button.
    expect(screen.getByRole('button', { name: /new project/i })).toBeDefined()
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

describe('AppSidebar — OrgPicker navigation', () => {
  const setSelectedOrg = vi.fn()

  beforeEach(() => {
    vi.clearAllMocks()
    mockNavigate.mockReset()
    setDefaults()
    ;(useOrg as Mock).mockReturnValue({
      organizations: [
        { name: 'org-a', displayName: 'Org A' },
        { name: 'org-b', displayName: 'Org B' },
      ],
      selectedOrg: 'org-a',
      setSelectedOrg,
      isLoading: false,
    })
  })

  it('navigates to org projects page when an org is selected in the picker', async () => {
    const { userEvent } = await import('@testing-library/user-event')
    const user = userEvent.setup()
    render(<AppSidebar />)
    const orgBItem = screen.getByText('Org B')
    await user.click(orgBItem)
    expect(setSelectedOrg).toHaveBeenCalledWith('org-b')
    expect(mockNavigate).toHaveBeenCalledWith({
      to: '/orgs/$orgName/projects',
      params: { orgName: 'org-b' },
    })
  })
})

describe('AppSidebar — OrgPicker empty state', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setDefaults()
    // organizations is empty and not loading
  })

  it('renders "New Organization" button when no orgs and not loading', () => {
    render(<AppSidebar />)
    expect(screen.getByRole('button', { name: /new organization/i })).toBeDefined()
  })

  it('does not render org picker dropdown when no orgs', () => {
    render(<AppSidebar />)
    expect(screen.queryByTestId('org-picker')).toBeNull()
  })
})

describe('AppSidebar — ProjectPicker empty state', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setDefaults()
    ;(useOrg as Mock).mockReturnValue({
      organizations: [{ name: 'my-org', displayName: 'My Org' }],
      selectedOrg: 'my-org',
      setSelectedOrg: vi.fn(),
      isLoading: false,
    })
    // projects is empty and not loading
  })

  it('renders "New Project" button when org is selected but no projects', () => {
    render(<AppSidebar />)
    expect(screen.getByRole('button', { name: /new project/i })).toBeDefined()
  })

  it('does not render project picker dropdown when no projects', () => {
    render(<AppSidebar />)
    expect(screen.queryByTestId('project-picker')).toBeNull()
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

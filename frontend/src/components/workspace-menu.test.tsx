import { render, screen } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

vi.mock('@tanstack/react-router', () => ({
  Link: ({
    children,
    to,
    params,
    ...rest
  }: {
    children: React.ReactNode
    to: string
    params?: Record<string, string>
  } & React.AnchorHTMLAttributes<HTMLAnchorElement>) => {
    let href = to
    if (params) {
      Object.entries(params).forEach(([k, v]) => {
        href = href.replace(`$${k}`, v)
      })
    }
    return (
      <a href={href} {...rest}>
        {children}
      </a>
    )
  },
}))

// shadcn dropdown is portaled and animation-driven; flatten it for the unit
// tests so menu items are queryable without simulating an open click.
vi.mock('@/components/ui/dropdown-menu', () => ({
  DropdownMenu: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DropdownMenuContent: ({ children }: { children: React.ReactNode }) => (
    <div data-testid="workspace-menu-content">{children}</div>
  ),
  DropdownMenuItem: ({
    children,
    asChild,
  }: {
    children: React.ReactNode
    asChild?: boolean
  }) => (asChild ? <>{children}</> : <div>{children}</div>),
  DropdownMenuSeparator: () => <hr />,
  DropdownMenuTrigger: ({
    children,
    asChild,
  }: {
    children: React.ReactNode
    asChild?: boolean
  }) => (asChild ? <>{children}</> : <div>{children}</div>),
}))

vi.mock('@/lib/org-context', () => ({ useOrg: vi.fn() }))
vi.mock('@/lib/project-context', () => ({ useProject: vi.fn() }))
vi.mock('@/lib/console-config', () => ({ getConsoleConfig: vi.fn() }))

import { useOrg } from '@/lib/org-context'
import { useProject } from '@/lib/project-context'
import { getConsoleConfig } from '@/lib/console-config'
import { WorkspaceMenu } from './workspace-menu'

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
  ;(getConsoleConfig as Mock).mockReturnValue({ devToolsEnabled: false })
}

describe('WorkspaceMenu — trigger label', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setDefaults()
  })

  it('falls back to "Holos Console" when no org or project is selected', () => {
    render(<WorkspaceMenu />)
    const trigger = screen.getByTestId('workspace-menu')
    expect(trigger.textContent).toContain('Holos Console')
  })

  it('shows the org display name when only an org is selected', () => {
    ;(useOrg as Mock).mockReturnValue({
      organizations: [{ name: 'my-org', displayName: 'My Org' }],
      selectedOrg: 'my-org',
      setSelectedOrg: vi.fn(),
      isLoading: false,
    })
    render(<WorkspaceMenu />)
    const trigger = screen.getByTestId('workspace-menu')
    expect(trigger.textContent).toContain('My Org')
  })

  it('falls back to org slug when displayName is empty', () => {
    ;(useOrg as Mock).mockReturnValue({
      organizations: [{ name: 'org-slug', displayName: '' }],
      selectedOrg: 'org-slug',
      setSelectedOrg: vi.fn(),
      isLoading: false,
    })
    render(<WorkspaceMenu />)
    expect(screen.getByTestId('workspace-menu').textContent).toContain('org-slug')
  })

  it('prefers the project display name when a project is selected', () => {
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
    render(<WorkspaceMenu />)
    const trigger = screen.getByTestId('workspace-menu')
    expect(trigger.textContent).toContain('My Project')
    expect(trigger.textContent).not.toContain('My Org')
  })
})

describe('WorkspaceMenu — items', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setDefaults()
  })

  it('renders About → /about', () => {
    render(<WorkspaceMenu />)
    const link = screen.getByTestId('workspace-menu-item-about')
    expect(link.getAttribute('href')).toBe('/about')
  })

  it('renders Switch organization → /organizations', () => {
    render(<WorkspaceMenu />)
    const link = screen.getByTestId('workspace-menu-item-switch-organization')
    expect(link.getAttribute('href')).toBe('/organizations')
  })

  it('renders Profile → /profile', () => {
    render(<WorkspaceMenu />)
    const link = screen.getByTestId('workspace-menu-item-profile')
    expect(link.getAttribute('href')).toBe('/profile')
  })

  it('hides Settings when no org is selected (no global settings route exists)', () => {
    render(<WorkspaceMenu />)
    expect(screen.queryByTestId('workspace-menu-item-settings')).toBeNull()
  })

  it('renders Settings → /orgs/$orgName/settings when an org is selected', () => {
    ;(useOrg as Mock).mockReturnValue({
      organizations: [{ name: 'my-org', displayName: 'My Org' }],
      selectedOrg: 'my-org',
      setSelectedOrg: vi.fn(),
      isLoading: false,
    })
    render(<WorkspaceMenu />)
    const link = screen.getByTestId('workspace-menu-item-settings')
    expect(link.getAttribute('href')).toBe('/orgs/my-org/settings')
  })

  it('renders Dev Tools when devToolsEnabled is true', () => {
    ;(getConsoleConfig as Mock).mockReturnValue({ devToolsEnabled: true })
    render(<WorkspaceMenu />)
    const link = screen.getByTestId('workspace-menu-item-dev-tools')
    expect(link.getAttribute('href')).toBe('/dev-tools')
  })

  it('hides Dev Tools when devToolsEnabled is false', () => {
    render(<WorkspaceMenu />)
    expect(screen.queryByTestId('workspace-menu-item-dev-tools')).toBeNull()
  })

  it('renders items in the canonical order: About, Settings, Switch organization, Profile, Dev Tools', () => {
    ;(useOrg as Mock).mockReturnValue({
      organizations: [{ name: 'my-org', displayName: 'My Org' }],
      selectedOrg: 'my-org',
      setSelectedOrg: vi.fn(),
      isLoading: false,
    })
    ;(getConsoleConfig as Mock).mockReturnValue({ devToolsEnabled: true })
    render(<WorkspaceMenu />)

    const content = screen.getByTestId('workspace-menu-content')
    const labels = Array.from(
      content.querySelectorAll('a[data-testid^="workspace-menu-item-"]'),
    ).map((el) => el.getAttribute('data-testid'))

    expect(labels).toEqual([
      'workspace-menu-item-about',
      'workspace-menu-item-settings',
      'workspace-menu-item-switch-organization',
      'workspace-menu-item-profile',
      'workspace-menu-item-dev-tools',
    ])
  })
})

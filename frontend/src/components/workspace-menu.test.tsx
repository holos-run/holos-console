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
    disabled,
    ...rest
  }: {
    children: React.ReactNode
    asChild?: boolean
    disabled?: boolean
  } & React.HTMLAttributes<HTMLDivElement>) =>
    asChild ? (
      <>{children}</>
    ) : (
      <div aria-disabled={disabled ? 'true' : undefined} {...rest}>
        {children}
      </div>
    ),
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

import { useOrg } from '@/lib/org-context'
import { useProject } from '@/lib/project-context'
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
}

describe('WorkspaceMenu — Holos brand label', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setDefaults()
  })

  it('renders a "Holos" brand label above the workspace-menu trigger', () => {
    render(<WorkspaceMenu />)
    // The brand label should be present in the document
    expect(screen.getByText('Holos')).toBeInTheDocument()
    // The workspace-menu trigger should appear after the brand label in the DOM
    const brandLabel = screen.getByText('Holos')
    const trigger = screen.getByTestId('workspace-menu')
    expect(
      brandLabel.compareDocumentPosition(trigger) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy()
  })
})

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

  it('renders Switch Organization → /organizations', () => {
    render(<WorkspaceMenu />)
    const link = screen.getByTestId('workspace-menu-item-switch-organization')
    expect(link.getAttribute('href')).toBe('/organizations')
  })

  it('renders Profile → /profile', () => {
    render(<WorkspaceMenu />)
    const link = screen.getByTestId('workspace-menu-item-profile')
    expect(link.getAttribute('href')).toBe('/profile')
  })

  it('does not render About', () => {
    render(<WorkspaceMenu />)
    expect(screen.queryByTestId('workspace-menu-item-about')).toBeNull()
  })

  it('does not render Dev Tools', () => {
    render(<WorkspaceMenu />)
    expect(screen.queryByTestId('workspace-menu-item-dev-tools')).toBeNull()
  })

  it('renders Project Settings disabled (not a link) when no project is selected', () => {
    render(<WorkspaceMenu />)
    const settings = screen.getByTestId('workspace-menu-item-project-settings')
    expect(settings.tagName).toBe('DIV')
    expect(settings.getAttribute('aria-disabled')).toBe('true')
    expect(settings.textContent).toContain('Project Settings')
  })

  it('renders Project Settings → /projects/$projectName/settings when a project is selected', () => {
    ;(useProject as Mock).mockReturnValue({
      projects: [{ name: 'my-project', displayName: 'My Project' }],
      selectedProject: 'my-project',
      setSelectedProject: vi.fn(),
      isLoading: false,
    })
    render(<WorkspaceMenu />)
    const link = screen.getByTestId('workspace-menu-item-project-settings')
    expect(link.getAttribute('href')).toBe('/projects/my-project/settings')
  })

  it('renders Organization Settings disabled (not a link) when no org is selected', () => {
    render(<WorkspaceMenu />)
    const settings = screen.getByTestId('workspace-menu-item-org-settings')
    expect(settings.tagName).toBe('DIV')
    expect(settings.getAttribute('aria-disabled')).toBe('true')
    expect(settings.textContent).toContain('Organization Settings')
  })

  it('renders Organization Settings → /organizations/$orgName/settings when an org is selected', () => {
    ;(useOrg as Mock).mockReturnValue({
      organizations: [{ name: 'my-org', displayName: 'My Org' }],
      selectedOrg: 'my-org',
      setSelectedOrg: vi.fn(),
      isLoading: false,
    })
    render(<WorkspaceMenu />)
    const link = screen.getByTestId('workspace-menu-item-org-settings')
    expect(link.getAttribute('href')).toBe('/organizations/my-org/settings')
  })

  it('renders Switch Projects → /organizations/$orgName/projects when an org is selected', () => {
    ;(useOrg as Mock).mockReturnValue({
      organizations: [{ name: 'my-org', displayName: 'My Org' }],
      selectedOrg: 'my-org',
      setSelectedOrg: vi.fn(),
      isLoading: false,
    })
    render(<WorkspaceMenu />)
    const link = screen.getByTestId('workspace-menu-item-switch-projects')
    expect(link.getAttribute('href')).toBe('/organizations/my-org/projects')
  })

  it('renders Switch Projects disabled when no org is selected', () => {
    render(<WorkspaceMenu />)
    const switchProjects = screen.getByTestId('workspace-menu-item-switch-projects')
    expect(switchProjects.tagName).toBe('DIV')
    expect(switchProjects.getAttribute('aria-disabled')).toBe('true')
    expect(switchProjects.textContent).toContain('Switch Projects')
  })

  it('renders Project Settings as a link but Organization Settings disabled when project is selected without an org (stale localStorage state)', () => {
    ;(useProject as Mock).mockReturnValue({
      projects: [{ name: 'my-project', displayName: 'My Project' }],
      selectedProject: 'my-project',
      setSelectedProject: vi.fn(),
      isLoading: false,
    })
    render(<WorkspaceMenu />)
    const projectSettings = screen.getByTestId('workspace-menu-item-project-settings')
    expect(projectSettings.getAttribute('href')).toBe('/projects/my-project/settings')
    const orgSettings = screen.getByTestId('workspace-menu-item-org-settings')
    expect(orgSettings.tagName).toBe('DIV')
    expect(orgSettings.getAttribute('aria-disabled')).toBe('true')
  })

  it('renders items in the canonical order: Project Settings, Organization Settings, Switch Projects, Switch Organization, Profile', () => {
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

    const content = screen.getByTestId('workspace-menu-content')
    const labels = Array.from(
      content.querySelectorAll('[data-testid^="workspace-menu-item-"]'),
    ).map((el) => el.getAttribute('data-testid'))

    expect(labels).toEqual([
      'workspace-menu-item-project-settings',
      'workspace-menu-item-org-settings',
      'workspace-menu-item-switch-projects',
      'workspace-menu-item-switch-organization',
      'workspace-menu-item-profile',
    ])
  })

  it('keeps the canonical order when no org/project selected (Project Settings and Org Settings are disabled but present)', () => {
    render(<WorkspaceMenu />)

    const content = screen.getByTestId('workspace-menu-content')
    const labels = Array.from(
      content.querySelectorAll('[data-testid^="workspace-menu-item-"]'),
    ).map((el) => el.getAttribute('data-testid'))

    expect(labels).toEqual([
      'workspace-menu-item-project-settings',
      'workspace-menu-item-org-settings',
      'workspace-menu-item-switch-projects',
      'workspace-menu-item-switch-organization',
      'workspace-menu-item-profile',
    ])
  })
})

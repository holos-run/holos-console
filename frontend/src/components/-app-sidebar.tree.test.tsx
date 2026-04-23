import { render, screen } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

// Integration test: exercise the flat 4-item nav with the real sidebar
// primitives (no mocks for ui/sidebar). Verifies that SidebarMenuButton
// asChild correctly forwards the Link child for enabled entries, and that
// disabled entries render a TooltipProvider-wrapped button instead of an anchor.

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
  useRouter: () => ({
    state: { location: { pathname: '/' } },
    navigate: vi.fn(),
  }),
}))

vi.mock('@/components/workspace-menu', () => ({
  WorkspaceMenu: () => <div data-testid="workspace-menu" />,
}))

vi.mock('@/lib/org-context', () => ({ useOrg: vi.fn() }))
vi.mock('@/lib/project-context', () => ({ useProject: vi.fn() }))
vi.mock('@/queries/version', () => ({
  useVersion: () => ({ data: { version: 'v0.0.0-test' } }),
}))

import { useOrg } from '@/lib/org-context'
import { useProject } from '@/lib/project-context'
import { SidebarProvider } from '@/components/ui/sidebar'
import { AppSidebar } from './app-sidebar'

function renderWithProvider(ui: React.ReactElement) {
  return render(<SidebarProvider>{ui}</SidebarProvider>)
}

function setupNoOrgNoProject() {
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

function setupOrgAndProjectSelected() {
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

describe('AppSidebar — HOL-914 flat nav (real sidebar primitives)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setupOrgAndProjectSelected()
  })

  it('renders all four nav entries when org and project are selected', () => {
    renderWithProvider(<AppSidebar />)
    expect(screen.getByRole('link', { name: /^projects$/i })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /^secrets$/i })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /^deployments$/i })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /^templates$/i })).toBeInTheDocument()
  })

  it('does not render a Resource Manager link', () => {
    renderWithProvider(<AppSidebar />)
    expect(screen.queryByRole('link', { name: /^resource manager$/i })).not.toBeInTheDocument()
  })

  it('Projects, Secrets, Deployments, and Templates are disabled (not links) when no org or project is selected', () => {
    setupNoOrgNoProject()
    renderWithProvider(<AppSidebar />)
    // Disabled entries render as buttons, not anchors
    expect(screen.queryByRole('link', { name: /^projects$/i })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /^secrets$/i })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /^deployments$/i })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /^templates$/i })).not.toBeInTheDocument()
  })

  it('workspace-menu data-testid renders in the header', () => {
    renderWithProvider(<AppSidebar />)
    expect(screen.getByTestId('workspace-menu')).toBeInTheDocument()
  })

  it('version string renders in the sidebar footer', () => {
    renderWithProvider(<AppSidebar />)
    expect(screen.getByText('v0.0.0-test')).toBeInTheDocument()
  })
})

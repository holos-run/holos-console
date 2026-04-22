import { render, screen } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

// HOL-856 integration test: exercise the flat 4-item nav with the real
// sidebar primitives (no mocks for ui/sidebar). This verifies that
// SidebarMenuButton asChild correctly forwards the Link child for enabled
// entries, and that disabled entries render a TooltipProvider-wrapped
// button instead of an anchor.
//
// The Collapsible toggle tests from HOL-604 are intentionally removed: the
// two-tree Collapsible layout is replaced by a flat SidebarMenu in HOL-856.

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

vi.mock('@/lib/project-context', () => ({ useProject: vi.fn() }))
vi.mock('@/queries/version', () => ({
  useVersion: () => ({ data: { version: 'v0.0.0-test' } }),
}))

import { useProject } from '@/lib/project-context'
import { SidebarProvider } from '@/components/ui/sidebar'
import { AppSidebar } from './app-sidebar'

function renderWithProvider(ui: React.ReactElement) {
  return render(<SidebarProvider>{ui}</SidebarProvider>)
}

function setupNoProject() {
  ;(useProject as Mock).mockReturnValue({
    projects: [],
    selectedProject: null,
    setSelectedProject: vi.fn(),
    isLoading: false,
  })
}

function setupProjectSelected() {
  ;(useProject as Mock).mockReturnValue({
    projects: [{ name: 'my-project', displayName: 'My Project' }],
    selectedProject: 'my-project',
    setSelectedProject: vi.fn(),
    isLoading: false,
  })
}

describe('AppSidebar — HOL-856 flat nav (real sidebar primitives)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setupProjectSelected()
  })

  it('renders all four nav entries when a project is selected', () => {
    renderWithProvider(<AppSidebar />)
    expect(screen.getByRole('link', { name: /^secrets$/i })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /^deployments$/i })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /^templates$/i })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /^resource manager$/i })).toBeInTheDocument()
  })

  it('Secrets, Deployments, and Templates are disabled (not links) when no project is selected', () => {
    setupNoProject()
    renderWithProvider(<AppSidebar />)
    // Disabled entries render as buttons, not anchors
    expect(screen.queryByRole('link', { name: /^secrets$/i })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /^deployments$/i })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /^templates$/i })).not.toBeInTheDocument()
  })

  it('Resource Manager is always rendered as a link', () => {
    setupNoProject()
    renderWithProvider(<AppSidebar />)
    expect(
      screen.getByRole('link', { name: /^resource manager$/i }),
    ).toBeInTheDocument()
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

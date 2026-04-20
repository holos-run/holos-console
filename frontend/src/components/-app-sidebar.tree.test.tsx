import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

// HOL-604 integration test: exercise the Project tree with the real
// Collapsible + Tooltip primitives to verify the asChild prop-merging chain
// (CollapsibleTrigger asChild > TooltipTrigger asChild > SidebarMenuButton)
// correctly forwards click events from the Collapsible primitive through to
// the underlying button so the children toggle as expected. The primary
// app-sidebar.test.tsx suite flattens these primitives for content-level
// assertions.
//
// Tooltip open-on-hover is NOT exercised here: Radix Tooltip listens for
// `onPointerMove` and gates open on `event.pointerType === 'mouse'`, which
// jsdom does not reliably synthesize (neither user-event's hover() nor
// fireEvent.pointerMove produces a pointer event Radix treats as a real
// mouse move). The content-level tooltip assertions in app-sidebar.test.tsx
// (display name + slug rendered in TooltipContent) are sufficient coverage
// for HOL-604's acceptance criteria; the hover interaction itself is Radix
// Tooltip's concern and is covered by upstream tests.

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
  useRouter: () => ({ state: { location: { pathname: '/' } }, navigate: vi.fn() }),
}))

vi.mock('@/components/workspace-menu', () => ({
  WorkspaceMenu: () => <div data-testid="workspace-menu" />,
}))

vi.mock('@/lib/org-context', () => ({ useOrg: vi.fn() }))
vi.mock('@/lib/project-context', () => ({ useProject: vi.fn() }))
vi.mock('@/queries/version', () => ({ useVersion: () => ({ data: { version: 'v0.0.0-test' } }) }))
vi.mock('@/queries/project-settings', () => ({
  useGetProjectSettings: () => ({ data: { deploymentsEnabled: true }, isPending: false }),
}))

import { useOrg } from '@/lib/org-context'
import { useProject } from '@/lib/project-context'
import { SidebarProvider } from '@/components/ui/sidebar'
import { AppSidebar } from './app-sidebar'

function renderWithProvider(ui: React.ReactElement) {
  return render(<SidebarProvider>{ui}</SidebarProvider>)
}

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

describe('AppSidebar — Project tree toggle (real Collapsible + Tooltip primitives)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setupProjectSelected()
  })

  it('toggles child visibility when the Project label is clicked', async () => {
    const user = userEvent.setup()
    renderWithProvider(<AppSidebar />)

    // defaultOpen=true on the Collapsible means children render on mount.
    expect(screen.getByRole('link', { name: /^secrets$/i })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /^deployments$/i })).toBeInTheDocument()

    await user.click(screen.getByTestId('project-tree-trigger'))

    // After collapse, Radix hides the content via data-state=closed. jsdom
    // doesn't apply the `hidden until found` / CSS rules, but Radix sets the
    // `hidden` attribute and removes the content from the tree when closed.
    // Assert via queryByRole with { hidden: false } to exclude hidden nodes.
    expect(
      screen.queryByRole('link', { name: /^secrets$/i, hidden: false }),
    ).not.toBeInTheDocument()

    await user.click(screen.getByTestId('project-tree-trigger'))

    // Re-expanding restores visibility.
    expect(screen.getByRole('link', { name: /^secrets$/i })).toBeInTheDocument()
  })
})

import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

// ---------------------------------------------------------------------------
// Mock context hooks BEFORE importing the component under test.
// vi.mock calls are hoisted to the top of the file by Vitest.
// ---------------------------------------------------------------------------
vi.mock('@/lib/org-context', () => ({ useOrg: vi.fn() }))
vi.mock('@/lib/project-context', () => ({ useProject: vi.fn() }))

// Flatten the shadcn DropdownMenu so menu items are immediately queryable
// without simulating an open click (the portal + animation make it opaque
// to jsdom without this flatten).
vi.mock('@/components/ui/dropdown-menu', () => ({
  DropdownMenu: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DropdownMenuContent: ({ children }: { children: React.ReactNode }) => (
    <div data-testid="scope-picker-menu-content">{children}</div>
  ),
  DropdownMenuItem: ({
    children,
    disabled,
    onSelect,
    ...rest
  }: {
    children: React.ReactNode
    disabled?: boolean
    onSelect?: () => void
  } & React.HTMLAttributes<HTMLDivElement>) => (
    <div
      role="menuitem"
      aria-disabled={disabled ? 'true' : undefined}
      onClick={!disabled ? onSelect : undefined}
      {...rest}
    >
      {children}
    </div>
  ),
  DropdownMenuTrigger: ({
    children,
    asChild,
  }: {
    children: React.ReactNode
    asChild?: boolean
    disabled?: boolean
  }) => (asChild ? <>{children}</> : <div>{children}</div>),
}))

// Flatten tooltip so the tooltip content is visible in the DOM tree
vi.mock('@/components/ui/tooltip', () => ({
  TooltipProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  Tooltip: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipTrigger: ({
    children,
    asChild,
  }: {
    children: React.ReactNode
    asChild?: boolean
  }) => (asChild ? <>{children}</> : <span>{children}</span>),
  TooltipContent: ({ children }: { children: React.ReactNode }) => (
    <div data-testid="scope-picker-tooltip">{children}</div>
  ),
}))

import { useOrg } from '@/lib/org-context'
import { useProject } from '@/lib/project-context'
import { ScopePicker } from './ScopePicker'

// ---------------------------------------------------------------------------
// Default mock values
// ---------------------------------------------------------------------------
function setupMocks({
  selectedProject = 'my-project',
}: { selectedProject?: string | null } = {}) {
  ;(useOrg as Mock).mockReturnValue({
    organizations: [],
    selectedOrg: 'my-org',
    setSelectedOrg: vi.fn(),
    isLoading: false,
  })
  ;(useProject as Mock).mockReturnValue({
    projects: [],
    selectedProject,
    setSelectedProject: vi.fn(),
    isLoading: false,
  })
}

describe('ScopePicker — renders both menu items', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setupMocks()
  })

  it('renders an Organization menu item', () => {
    render(<ScopePicker value="organization" onChange={vi.fn()} />)
    expect(
      screen.getByTestId('scope-picker-item-organization'),
    ).toBeInTheDocument()
    expect(
      screen.getByTestId('scope-picker-item-organization'),
    ).toHaveTextContent('Organization')
  })

  it('renders a Project menu item', () => {
    render(<ScopePicker value="organization" onChange={vi.fn()} />)
    expect(
      screen.getByTestId('scope-picker-item-project'),
    ).toBeInTheDocument()
    expect(
      screen.getByTestId('scope-picker-item-project'),
    ).toHaveTextContent('Project')
  })

  it('renders exactly two menu items', () => {
    render(<ScopePicker value="organization" onChange={vi.fn()} />)
    const items = screen.getAllByRole('menuitem')
    expect(items).toHaveLength(2)
  })
})

describe('ScopePicker — fires onChange when an item is clicked', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setupMocks()
  })

  it('calls onChange with "project" when the Project item is clicked', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    render(<ScopePicker value="organization" onChange={onChange} />)
    await user.click(screen.getByTestId('scope-picker-item-project'))
    expect(onChange).toHaveBeenCalledOnce()
    expect(onChange).toHaveBeenCalledWith('project')
  })

  it('calls onChange with "organization" when the Organization item is clicked', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    render(<ScopePicker value="project" onChange={onChange} />)
    await user.click(screen.getByTestId('scope-picker-item-organization'))
    expect(onChange).toHaveBeenCalledOnce()
    expect(onChange).toHaveBeenCalledWith('organization')
  })

  it('does not call onChange when a disabled item is clicked', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    // No project selected — Project item is disabled
    setupMocks({ selectedProject: null })
    render(<ScopePicker value="organization" onChange={onChange} />)
    await user.click(screen.getByTestId('scope-picker-item-project'))
    expect(onChange).not.toHaveBeenCalled()
  })
})

describe('ScopePicker — disables Project when no project is selected', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('disables the Project item when selectedProject is null', () => {
    setupMocks({ selectedProject: null })
    render(<ScopePicker value="organization" onChange={vi.fn()} />)
    const projectItem = screen.getByTestId('scope-picker-item-project')
    expect(projectItem).toHaveAttribute('aria-disabled', 'true')
  })

  it('enables the Project item when selectedProject is non-null', () => {
    setupMocks({ selectedProject: 'my-project' })
    render(<ScopePicker value="organization" onChange={vi.fn()} />)
    const projectItem = screen.getByTestId('scope-picker-item-project')
    expect(projectItem).not.toHaveAttribute('aria-disabled', 'true')
  })

  it('shows a tooltip hint when Project is disabled', () => {
    setupMocks({ selectedProject: null })
    render(<ScopePicker value="organization" onChange={vi.fn()} />)
    // Tooltip content is rendered in the flattened mock
    expect(screen.getByTestId('scope-picker-tooltip')).toHaveTextContent(
      'Select a project first',
    )
  })

  it('does not show a tooltip when Project is enabled', () => {
    setupMocks({ selectedProject: 'my-project' })
    render(<ScopePicker value="organization" onChange={vi.fn()} />)
    expect(screen.queryByTestId('scope-picker-tooltip')).not.toBeInTheDocument()
  })
})

describe('ScopePicker — trigger label reflects current value', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setupMocks()
  })

  it('shows "Organization" in the trigger when value is organization', () => {
    render(<ScopePicker value="organization" onChange={vi.fn()} />)
    expect(
      screen.getByTestId('scope-picker-trigger'),
    ).toHaveTextContent('Organization')
  })

  it('shows "Project" in the trigger when value is project', () => {
    render(<ScopePicker value="project" onChange={vi.fn()} />)
    expect(
      screen.getByTestId('scope-picker-trigger'),
    ).toHaveTextContent('Project')
  })
})

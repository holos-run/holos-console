import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

vi.mock('@/queries/templates', () => ({
  useCheckUpdates: vi.fn(),
  useUpdateTemplate: vi.fn(),
}))

vi.mock('@/components/ui/dialog', () => ({
  Dialog: ({ children, open }: { children: React.ReactNode; open?: boolean }) =>
    open ? <div data-testid="dialog">{children}</div> : null,
  DialogContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogHeader: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogTitle: ({ children }: { children: React.ReactNode }) => <h2>{children}</h2>,
  DialogDescription: ({ children }: { children: React.ReactNode }) => <p>{children}</p>,
  DialogFooter: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}))

vi.mock('@/components/ui/badge', () => ({
  Badge: ({ children, ...rest }: { children: React.ReactNode; variant?: string; className?: string }) => (
    <span data-testid="badge" {...rest}>{children}</span>
  ),
}))

vi.mock('@/components/ui/button', () => ({
  Button: ({ children, onClick, disabled, ...rest }: {
    children: React.ReactNode
    onClick?: () => void
    disabled?: boolean
    variant?: string
    size?: string
    className?: string
  }) => (
    <button onClick={onClick} disabled={disabled}>
      {children}
    </button>
  ),
}))

vi.mock('@/components/ui/alert', () => ({
  Alert: ({ children }: { children: React.ReactNode }) => <div role="alert">{children}</div>,
  AlertDescription: ({ children }: { children: React.ReactNode }) => <span>{children}</span>,
}))

vi.mock('@/components/ui/table', () => ({
  Table: ({ children }: { children: React.ReactNode }) => <table>{children}</table>,
  TableHeader: ({ children }: { children: React.ReactNode }) => <thead>{children}</thead>,
  TableBody: ({ children }: { children: React.ReactNode }) => <tbody>{children}</tbody>,
  TableRow: ({ children }: { children: React.ReactNode }) => <tr>{children}</tr>,
  TableHead: ({ children }: { children: React.ReactNode }) => <th>{children}</th>,
  TableCell: ({ children }: { children: React.ReactNode }) => <td>{children}</td>,
}))

vi.mock('lucide-react', () => ({
  ArrowUpCircle: () => <span data-testid="icon-arrow-up-circle" />,
  AlertTriangle: () => <span data-testid="icon-alert-triangle" />,
}))

import { useCheckUpdates, useUpdateTemplate } from '@/queries/templates'
import { UpdatesAvailableBadge, UpgradeDialog } from './template-updates'
import { TemplateScope } from '@/gen/holos/console/v1/templates_pb.js'

const testScope = { scope: TemplateScope.PROJECT, scopeName: 'test-project' } as any

function makeUpdate(overrides: Record<string, unknown> = {}) {
  return {
    ref: {
      scope: TemplateScope.ORGANIZATION,
      scopeName: 'test-org',
      name: 'platform-security',
      versionConstraint: '>=1.0.0 <2.0.0',
    },
    currentVersion: '1.0.0',
    latestCompatibleVersion: '1.2.0',
    latestVersion: '1.2.0',
    breakingUpdateAvailable: false,
    ...overrides,
  }
}

describe('UpdatesAvailableBadge', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    ;(useCheckUpdates as Mock).mockReturnValue({
      data: undefined,
      isPending: false,
      error: null,
    })
  })

  it('renders nothing when no updates are available', () => {
    ;(useCheckUpdates as Mock).mockReturnValue({
      data: [],
      isPending: false,
      error: null,
    })
    const { container } = render(
      <UpdatesAvailableBadge scope={testScope} templateName="my-template" />
    )
    expect(container.textContent).toBe('')
  })

  it('renders nothing while loading', () => {
    ;(useCheckUpdates as Mock).mockReturnValue({
      data: undefined,
      isPending: true,
      error: null,
    })
    const { container } = render(
      <UpdatesAvailableBadge scope={testScope} templateName="my-template" />
    )
    expect(container.textContent).toBe('')
  })

  it('renders badge when one compatible update exists', () => {
    ;(useCheckUpdates as Mock).mockReturnValue({
      data: [makeUpdate()],
      isPending: false,
      error: null,
    })
    render(
      <UpdatesAvailableBadge scope={testScope} templateName="my-template" />
    )
    expect(screen.getByText(/1 update/i)).toBeInTheDocument()
  })

  it('renders badge with count for multiple updates', () => {
    ;(useCheckUpdates as Mock).mockReturnValue({
      data: [
        makeUpdate({ ref: { scope: 1, scopeName: 'org', name: 'tmpl-a', versionConstraint: '' } }),
        makeUpdate({ ref: { scope: 1, scopeName: 'org', name: 'tmpl-b', versionConstraint: '' } }),
      ],
      isPending: false,
      error: null,
    })
    render(
      <UpdatesAvailableBadge scope={testScope} templateName="my-template" />
    )
    expect(screen.getByText(/2 updates/i)).toBeInTheDocument()
  })

  it('calls onClick handler when badge is clicked', () => {
    ;(useCheckUpdates as Mock).mockReturnValue({
      data: [makeUpdate()],
      isPending: false,
      error: null,
    })
    const handleClick = vi.fn()
    render(
      <UpdatesAvailableBadge scope={testScope} templateName="my-template" onClick={handleClick} />
    )
    fireEvent.click(screen.getByText(/1 update/i))
    expect(handleClick).toHaveBeenCalled()
  })
})

describe('UpgradeDialog', () => {
  const mockMutateAsync = vi.fn()

  beforeEach(() => {
    vi.clearAllMocks()
    ;(useUpdateTemplate as Mock).mockReturnValue({
      mutateAsync: mockMutateAsync,
      isPending: false,
    })
  })

  it('renders update table with compatible update', () => {
    const updates = [makeUpdate()]
    render(
      <UpgradeDialog
        open={true}
        onOpenChange={() => {}}
        updates={updates as any}
        scope={testScope}
        templateName="my-template"
        linkedTemplates={[]}
      />
    )
    expect(screen.getByText('platform-security')).toBeInTheDocument()
    expect(screen.getByText('1.0.0')).toBeInTheDocument()
    expect(screen.getByText('1.2.0')).toBeInTheDocument()
  })

  it('shows breaking tag for breaking updates', () => {
    const updates = [makeUpdate({
      latestCompatibleVersion: '',
      latestVersion: '2.0.0',
      breakingUpdateAvailable: true,
    })]
    render(
      <UpgradeDialog
        open={true}
        onOpenChange={() => {}}
        updates={updates as any}
        scope={testScope}
        templateName="my-template"
        linkedTemplates={[]}
      />
    )
    expect(screen.getByText(/breaking/i)).toBeInTheDocument()
  })

  it('calls update mutation for compatible update', async () => {
    mockMutateAsync.mockResolvedValue({})
    const linkedTemplates = [
      {
        scope: TemplateScope.ORGANIZATION,
        scopeName: 'test-org',
        name: 'platform-security',
        versionConstraint: '>=1.0.0 <2.0.0',
      },
    ]
    const updates = [makeUpdate()]
    render(
      <UpgradeDialog
        open={true}
        onOpenChange={() => {}}
        updates={updates as any}
        scope={testScope}
        templateName="my-template"
        linkedTemplates={linkedTemplates as any}
      />
    )
    // Click the individual Update button
    const updateButtons = screen.getAllByText(/^update$/i)
    fireEvent.click(updateButtons[0])

    await waitFor(() => {
      expect(mockMutateAsync).toHaveBeenCalledWith(
        expect.objectContaining({
          updateLinkedTemplates: true,
        })
      )
    })
  })

  it('requires explicit confirmation for breaking upgrade', async () => {
    mockMutateAsync.mockResolvedValue({})
    const linkedTemplates = [
      {
        scope: TemplateScope.ORGANIZATION,
        scopeName: 'test-org',
        name: 'platform-security',
        versionConstraint: '>=1.0.0 <2.0.0',
      },
    ]
    const updates = [makeUpdate({
      latestCompatibleVersion: '',
      latestVersion: '2.0.0',
      breakingUpdateAvailable: true,
    })]
    render(
      <UpgradeDialog
        open={true}
        onOpenChange={() => {}}
        updates={updates as any}
        scope={testScope}
        templateName="my-template"
        linkedTemplates={linkedTemplates as any}
      />
    )
    // Click "Upgrade" — should not call mutation yet
    fireEvent.click(screen.getByText(/^upgrade$/i))
    expect(mockMutateAsync).not.toHaveBeenCalled()

    // Confirmation dialog should appear
    expect(screen.getByText(/confirm breaking upgrade/i)).toBeInTheDocument()

    // Click "Confirm Upgrade" to proceed
    fireEvent.click(screen.getByText(/confirm upgrade/i))
    await waitFor(() => {
      expect(mockMutateAsync).toHaveBeenCalledWith(
        expect.objectContaining({ updateLinkedTemplates: true })
      )
    })
  })

  it('selects compatible version when both compatible and breaking are available', async () => {
    mockMutateAsync.mockResolvedValue({})
    const linkedTemplates = [
      {
        scope: TemplateScope.ORGANIZATION,
        scopeName: 'test-org',
        name: 'platform-security',
        versionConstraint: '>=1.0.0 <2.0.0',
      },
    ]
    // Mixed case: compatible minor update + breaking major available
    const updates = [makeUpdate({
      latestCompatibleVersion: '1.3.0',
      latestVersion: '2.0.0',
      breakingUpdateAvailable: true,
    })]
    render(
      <UpgradeDialog
        open={true}
        onOpenChange={() => {}}
        updates={updates as any}
        scope={testScope}
        templateName="my-template"
        linkedTemplates={linkedTemplates as any}
      />
    )
    // Should show "Update" (not "Upgrade") since compatible version exists
    const updateButton = screen.getByText(/^update$/i)
    fireEvent.click(updateButton)

    await waitFor(() => {
      expect(mockMutateAsync).toHaveBeenCalledWith(
        expect.objectContaining({
          updateLinkedTemplates: true,
          linkedTemplates: expect.arrayContaining([
            expect.objectContaining({
              versionConstraint: '>=1.3.0 <2.0.0',
            }),
          ]),
        })
      )
    })
  })

  it('renders empty state when no updates', () => {
    render(
      <UpgradeDialog
        open={true}
        onOpenChange={() => {}}
        updates={[]}
        scope={testScope}
        templateName="my-template"
        linkedTemplates={[]}
      />
    )
    expect(screen.getByText(/no updates/i)).toBeInTheDocument()
  })

  it('shows Update All button when multiple compatible updates exist', () => {
    const updates = [
      makeUpdate({ ref: { scope: 1, scopeName: 'org', name: 'tmpl-a', versionConstraint: '' } }),
      makeUpdate({ ref: { scope: 1, scopeName: 'org', name: 'tmpl-b', versionConstraint: '' } }),
    ]
    render(
      <UpgradeDialog
        open={true}
        onOpenChange={() => {}}
        updates={updates as any}
        scope={testScope}
        templateName="my-template"
        linkedTemplates={[]}
      />
    )
    expect(screen.getByText(/update all compatible/i)).toBeInTheDocument()
  })
})

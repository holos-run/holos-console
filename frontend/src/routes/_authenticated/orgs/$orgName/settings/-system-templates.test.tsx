import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => ({ orgName: 'test-org', templateName: 'reference-grant' }),
    }),
    useNavigate: () => vi.fn(),
    Link: ({ children, ...props }: React.AnchorHTMLAttributes<HTMLAnchorElement> & { children: React.ReactNode }) =>
      <a {...props}>{children}</a>,
  }
})

vi.mock('@/queries/system-templates', () => ({
  useListSystemTemplates: vi.fn(),
  useGetSystemTemplate: vi.fn(),
  useUpdateSystemTemplate: vi.fn(),
  useCloneSystemTemplate: vi.fn(),
  useRenderSystemTemplate: vi.fn(),
}))

vi.mock('@/queries/organizations', () => ({
  useGetOrganization: vi.fn(),
}))

vi.mock('@/hooks/use-debounced-value', () => ({
  useDebouncedValue: vi.fn((value: unknown) => value),
}))

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

import {
  useListSystemTemplates,
  useGetSystemTemplate,
  useUpdateSystemTemplate,
  useCloneSystemTemplate,
  useRenderSystemTemplate,
} from '@/queries/system-templates'
import { useGetOrganization } from '@/queries/organizations'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { SystemTemplatesListPage } from './system-templates/index'
import { SystemTemplateDetailPage } from './system-templates/$templateName'

const mockTemplates = [
  {
    name: 'reference-grant',
    org: 'test-org',
    displayName: 'ReferenceGrant',
    description: 'Allows gateway HTTPRoutes to reference project Services',
    cueTemplate: '// reference grant template',
    mandatory: true,
    enabled: false,
  },
  {
    name: 'optional-template',
    org: 'test-org',
    displayName: 'Optional Template',
    description: 'An optional system template',
    cueTemplate: '// optional template',
    mandatory: false,
    enabled: true,
  },
]

function setupListMocks() {
  ;(useListSystemTemplates as Mock).mockReturnValue({
    data: mockTemplates,
    isPending: false,
    error: null,
  })
}

function setupDetailMocks(userRole = Role.OWNER, templateOverride?: Partial<typeof mockTemplates[0]>) {
  const template = { ...mockTemplates[0], ...templateOverride }
  ;(useGetSystemTemplate as Mock).mockReturnValue({
    data: template,
    isPending: false,
    error: null,
  })
  ;(useUpdateSystemTemplate as Mock).mockReturnValue({
    mutateAsync: vi.fn().mockResolvedValue({}),
    isPending: false,
  })
  ;(useCloneSystemTemplate as Mock).mockReturnValue({
    mutateAsync: vi.fn().mockResolvedValue({ name: 'new-template' }),
    isPending: false,
  })
  ;(useRenderSystemTemplate as Mock).mockReturnValue({
    data: { renderedYaml: '', renderedJson: '' },
    error: null,
    isFetching: false,
  })
  ;(useGetOrganization as Mock).mockReturnValue({
    data: { name: 'test-org', userRole },
    isPending: false,
    error: null,
  })
}

describe('SystemTemplatesListPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders template names', () => {
    setupListMocks()
    render(<SystemTemplatesListPage orgName="test-org" />)
    expect(screen.getByText('reference-grant')).toBeInTheDocument()
    expect(screen.getByText('optional-template')).toBeInTheDocument()
  })

  it('renders template descriptions', () => {
    setupListMocks()
    render(<SystemTemplatesListPage orgName="test-org" />)
    expect(screen.getByText('Allows gateway HTTPRoutes to reference project Services')).toBeInTheDocument()
  })

  it('shows mandatory badge for mandatory templates', () => {
    setupListMocks()
    render(<SystemTemplatesListPage orgName="test-org" />)
    expect(screen.getByText('Mandatory')).toBeInTheDocument()
  })

  it('does not show mandatory badge for non-mandatory templates', () => {
    ;(useListSystemTemplates as Mock).mockReturnValue({
      data: [mockTemplates[1]], // only non-mandatory
      isPending: false,
      error: null,
    })
    render(<SystemTemplatesListPage orgName="test-org" />)
    expect(screen.queryByText('Mandatory')).not.toBeInTheDocument()
  })

  it('shows enabled badge for enabled templates', () => {
    setupListMocks()
    render(<SystemTemplatesListPage orgName="test-org" />)
    expect(screen.getByText('Enabled')).toBeInTheDocument()
  })

  it('shows disabled badge for disabled templates', () => {
    setupListMocks()
    render(<SystemTemplatesListPage orgName="test-org" />)
    expect(screen.getByText('Disabled')).toBeInTheDocument()
  })

  it('shows skeleton while loading', () => {
    ;(useListSystemTemplates as Mock).mockReturnValue({
      data: undefined,
      isPending: true,
      error: null,
    })
    render(<SystemTemplatesListPage orgName="test-org" />)
    // Should not crash; skeleton elements rendered
    expect(screen.queryByText('reference-grant')).not.toBeInTheDocument()
  })

  it('shows error message when fetch fails', () => {
    ;(useListSystemTemplates as Mock).mockReturnValue({
      data: undefined,
      isPending: false,
      error: new Error('Failed to load templates'),
    })
    render(<SystemTemplatesListPage orgName="test-org" />)
    expect(screen.getByText('Failed to load templates')).toBeInTheDocument()
  })
})

describe('SystemTemplateDetailPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders template name and mandatory badge', () => {
    setupDetailMocks(Role.OWNER)
    render(<SystemTemplateDetailPage orgName="test-org" templateName="reference-grant" />)
    expect(screen.getByText('reference-grant')).toBeInTheDocument()
    expect(screen.getByText('Mandatory')).toBeInTheDocument()
  })

  it('renders template display name', () => {
    setupDetailMocks(Role.OWNER)
    render(<SystemTemplateDetailPage orgName="test-org" templateName="reference-grant" />)
    expect(screen.getByText('ReferenceGrant')).toBeInTheDocument()
  })

  it('shows Save button for org OWNER', () => {
    setupDetailMocks(Role.OWNER)
    render(<SystemTemplateDetailPage orgName="test-org" templateName="reference-grant" />)
    expect(screen.getByRole('button', { name: /save/i })).toBeInTheDocument()
  })

  it('hides Save button for org VIEWER (locked)', () => {
    setupDetailMocks(Role.VIEWER)
    render(<SystemTemplateDetailPage orgName="test-org" templateName="reference-grant" />)
    expect(screen.queryByRole('button', { name: /save/i })).not.toBeInTheDocument()
  })

  it('shows read-only message for non-owner', () => {
    setupDetailMocks(Role.VIEWER)
    render(<SystemTemplateDetailPage orgName="test-org" templateName="reference-grant" />)
    expect(screen.getByText(/org Owner permissions/i)).toBeInTheDocument()
  })

  it('does not show mandatory badge for non-mandatory template', () => {
    ;(useGetSystemTemplate as Mock).mockReturnValue({
      data: mockTemplates[1], // non-mandatory
      isPending: false,
      error: null,
    })
    ;(useUpdateSystemTemplate as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false })
    ;(useCloneSystemTemplate as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({ name: 'new' }), isPending: false })
    ;(useRenderSystemTemplate as Mock).mockReturnValue({ data: undefined, error: null, isFetching: false })
    ;(useGetOrganization as Mock).mockReturnValue({ data: { userRole: Role.OWNER }, isPending: false, error: null })
    render(<SystemTemplateDetailPage orgName="test-org" templateName="optional-template" />)
    expect(screen.queryByText('Mandatory')).not.toBeInTheDocument()
  })

  describe('enabled toggle', () => {
    it('shows enabled toggle for org OWNER', () => {
      setupDetailMocks(Role.OWNER)
      render(<SystemTemplateDetailPage orgName="test-org" templateName="reference-grant" />)
      expect(screen.getByRole('switch', { name: /enabled/i })).toBeInTheDocument()
    })

    it('toggle is checked when template is enabled', () => {
      setupDetailMocks(Role.OWNER, { enabled: true })
      render(<SystemTemplateDetailPage orgName="test-org" templateName="reference-grant" />)
      const toggle = screen.getByRole('switch', { name: /enabled/i })
      expect(toggle).toHaveAttribute('data-state', 'checked')
    })

    it('toggle is unchecked when template is disabled', () => {
      setupDetailMocks(Role.OWNER, { enabled: false })
      render(<SystemTemplateDetailPage orgName="test-org" templateName="reference-grant" />)
      const toggle = screen.getByRole('switch', { name: /enabled/i })
      expect(toggle).toHaveAttribute('data-state', 'unchecked')
    })

    it('clicking toggle calls updateSystemTemplate with new enabled state', async () => {
      setupDetailMocks(Role.OWNER, { enabled: false })
      const user = userEvent.setup()
      render(<SystemTemplateDetailPage orgName="test-org" templateName="reference-grant" />)
      const toggle = screen.getByRole('switch', { name: /enabled/i })
      await user.click(toggle)
      const mutateAsync = (useUpdateSystemTemplate as Mock).mock.results[0].value.mutateAsync
      await waitFor(() => {
        expect(mutateAsync).toHaveBeenCalledWith(expect.objectContaining({ enabled: true }))
      })
    })

    it('toggle is disabled for org VIEWER', () => {
      setupDetailMocks(Role.VIEWER)
      render(<SystemTemplateDetailPage orgName="test-org" templateName="reference-grant" />)
      const toggle = screen.getByRole('switch', { name: /enabled/i })
      expect(toggle).toBeDisabled()
    })
  })

  describe('clone button', () => {
    it('shows clone button for org OWNER', () => {
      setupDetailMocks(Role.OWNER)
      render(<SystemTemplateDetailPage orgName="test-org" templateName="reference-grant" />)
      expect(screen.getByRole('button', { name: /clone/i })).toBeInTheDocument()
    })

    it('shows clone button for org VIEWER (clone is read-only action)', () => {
      setupDetailMocks(Role.VIEWER)
      render(<SystemTemplateDetailPage orgName="test-org" templateName="reference-grant" />)
      expect(screen.getByRole('button', { name: /clone/i })).toBeInTheDocument()
    })

    it('clicking clone opens dialog', async () => {
      setupDetailMocks(Role.OWNER)
      const user = userEvent.setup()
      render(<SystemTemplateDetailPage orgName="test-org" templateName="reference-grant" />)
      await user.click(screen.getByRole('button', { name: /clone/i }))
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })

    it('clone dialog has name and display name fields', async () => {
      setupDetailMocks(Role.OWNER)
      const user = userEvent.setup()
      render(<SystemTemplateDetailPage orgName="test-org" templateName="reference-grant" />)
      await user.click(screen.getByRole('button', { name: /clone/i }))
      expect(screen.getByRole('textbox', { name: /^name$/i })).toBeInTheDocument()
      expect(screen.getByRole('textbox', { name: /display name/i })).toBeInTheDocument()
    })

    it('confirming clone calls cloneSystemTemplate', async () => {
      setupDetailMocks(Role.OWNER)
      const user = userEvent.setup()
      render(<SystemTemplateDetailPage orgName="test-org" templateName="reference-grant" />)
      await user.click(screen.getByRole('button', { name: /clone/i }))
      const nameInput = screen.getByRole('textbox', { name: /^name$/i })
      await user.clear(nameInput)
      await user.type(nameInput, 'new-template')
      const displayNameInput = screen.getByRole('textbox', { name: /display name/i })
      await user.clear(displayNameInput)
      await user.type(displayNameInput, 'New Template')
      await user.click(screen.getByRole('button', { name: /^clone$/i }))
      const mutateAsync = (useCloneSystemTemplate as Mock).mock.results[0].value.mutateAsync
      await waitFor(() => {
        expect(mutateAsync).toHaveBeenCalledWith(expect.objectContaining({
          sourceName: 'reference-grant',
          name: 'new-template',
          displayName: 'New Template',
        }))
      })
    })

    it('cancel closes clone dialog without saving', async () => {
      setupDetailMocks(Role.OWNER)
      const user = userEvent.setup()
      render(<SystemTemplateDetailPage orgName="test-org" templateName="reference-grant" />)
      await user.click(screen.getByRole('button', { name: /clone/i }))
      await user.click(screen.getByRole('button', { name: /cancel/i }))
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
      const mutateAsync = (useCloneSystemTemplate as Mock).mock.results[0].value.mutateAsync
      expect(mutateAsync).not.toHaveBeenCalled()
    })
  })
})

import { render, screen } from '@testing-library/react'
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
    gatewayNamespace: 'istio-ingress',
  },
  {
    name: 'optional-template',
    org: 'test-org',
    displayName: 'Optional Template',
    description: 'An optional system template',
    cueTemplate: '// optional template',
    mandatory: false,
    gatewayNamespace: 'istio-ingress',
  },
]

function setupListMocks() {
  ;(useListSystemTemplates as Mock).mockReturnValue({
    data: mockTemplates,
    isPending: false,
    error: null,
  })
}

function setupDetailMocks(userRole = Role.OWNER) {
  ;(useGetSystemTemplate as Mock).mockReturnValue({
    data: mockTemplates[0],
    isPending: false,
    error: null,
  })
  ;(useUpdateSystemTemplate as Mock).mockReturnValue({
    mutateAsync: vi.fn().mockResolvedValue({}),
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

  it('renders gateway namespace', () => {
    setupDetailMocks(Role.OWNER)
    render(<SystemTemplateDetailPage orgName="test-org" templateName="reference-grant" />)
    expect(screen.getByText('istio-ingress')).toBeInTheDocument()
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
    ;(useRenderSystemTemplate as Mock).mockReturnValue({ data: undefined, error: null, isFetching: false })
    ;(useGetOrganization as Mock).mockReturnValue({ data: { userRole: Role.OWNER }, isPending: false, error: null })
    render(<SystemTemplateDetailPage orgName="test-org" templateName="optional-template" />)
    expect(screen.queryByText('Mandatory')).not.toBeInTheDocument()
  })
})

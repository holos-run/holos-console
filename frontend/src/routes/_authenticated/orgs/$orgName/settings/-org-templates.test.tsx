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

vi.mock('@/queries/templates', () => ({
  useListTemplates: vi.fn(),
  useGetTemplate: vi.fn(),
  useCreateTemplate: vi.fn(),
  useUpdateTemplate: vi.fn(),
  useCloneTemplate: vi.fn(),
  useRenderTemplate: vi.fn(),
  useListReleases: vi.fn().mockReturnValue({ data: [], isPending: false, error: null }),
  useCreateRelease: vi.fn().mockReturnValue({ mutateAsync: vi.fn(), isPending: false }),
}))

vi.mock('@/queries/organizations', () => ({
  useGetOrganization: vi.fn(),
}))

vi.mock('@/hooks/use-debounced-value', () => ({
  useDebouncedValue: vi.fn((value: unknown) => value),
}))

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

import { useListTemplates } from '@/queries/templates'
import { useGetOrganization } from '@/queries/organizations'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { OrgTemplatesListPage } from './org-templates/index'

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
    description: 'An optional platform template',
    cueTemplate: '// optional template',
    mandatory: false,
    enabled: true,
  },
]

function setupListMocks(userRole = Role.OWNER) {
  ;(useListTemplates as Mock).mockReturnValue({
    data: mockTemplates,
    isPending: false,
    error: null,
  })
  ;(useGetOrganization as Mock).mockReturnValue({
    data: { name: 'test-org', userRole },
    isPending: false,
    error: null,
  })
}

describe('OrgTemplatesListPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders template names', () => {
    setupListMocks()
    render(<OrgTemplatesListPage orgName="test-org" />)
    expect(screen.getByText('reference-grant')).toBeInTheDocument()
    expect(screen.getByText('optional-template')).toBeInTheDocument()
  })

  it('renders template descriptions', () => {
    setupListMocks()
    render(<OrgTemplatesListPage orgName="test-org" />)
    expect(screen.getByText('Allows gateway HTTPRoutes to reference project Services')).toBeInTheDocument()
  })

  // HOL-555 removed the Mandatory badge from the list view. TemplatePolicy
  // REQUIRE rules (HOL-558) will re-introduce an "always applied" affordance.
  it('does not show Mandatory badge (removed in HOL-555)', () => {
    setupListMocks()
    render(<OrgTemplatesListPage orgName="test-org" />)
    expect(screen.queryByText('Mandatory')).not.toBeInTheDocument()
  })

  it('shows enabled badge for enabled templates', () => {
    setupListMocks()
    render(<OrgTemplatesListPage orgName="test-org" />)
    expect(screen.getByText('Enabled')).toBeInTheDocument()
  })

  it('shows disabled badge for disabled templates', () => {
    setupListMocks()
    render(<OrgTemplatesListPage orgName="test-org" />)
    expect(screen.getByText('Disabled')).toBeInTheDocument()
  })

  it('shows skeleton while loading', () => {
    ;(useListTemplates as Mock).mockReturnValue({
      data: undefined,
      isPending: true,
      error: null,
    })
    ;(useGetOrganization as Mock).mockReturnValue({ data: { userRole: Role.OWNER }, isPending: false, error: null })
    render(<OrgTemplatesListPage orgName="test-org" />)
    // Should not crash; skeleton elements rendered
    expect(screen.queryByText('reference-grant')).not.toBeInTheDocument()
  })

  it('shows error message when fetch fails', () => {
    ;(useListTemplates as Mock).mockReturnValue({
      data: undefined,
      isPending: false,
      error: new Error('Failed to load templates'),
    })
    ;(useGetOrganization as Mock).mockReturnValue({ data: { userRole: Role.OWNER }, isPending: false, error: null })
    render(<OrgTemplatesListPage orgName="test-org" />)
    expect(screen.getByText('Failed to load templates')).toBeInTheDocument()
  })

  describe('create template link', () => {
    it('shows Create Template link for org OWNER', () => {
      setupListMocks(Role.OWNER)
      render(<OrgTemplatesListPage orgName="test-org" />)
      expect(screen.getByRole('button', { name: /create template/i })).toBeInTheDocument()
    })

    it('does not show Create Template link for org VIEWER', () => {
      setupListMocks(Role.VIEWER)
      render(<OrgTemplatesListPage orgName="test-org" />)
      expect(screen.queryByRole('button', { name: /create template/i })).not.toBeInTheDocument()
    })

    it('Create Template button is wrapped in a link to the new page', () => {
      setupListMocks(Role.OWNER)
      render(<OrgTemplatesListPage orgName="test-org" />)
      const button = screen.getByRole('button', { name: /create template/i })
      const link = button.closest('a')
      expect(link).toBeInTheDocument()
    })
  })
})


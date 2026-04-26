// HOL-1022: Tests for the org-scoped template-grant create page.

import { render, screen } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

const mockNavigate = vi.fn()

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => ({ orgName: 'test-org' }),
      useSearch: () => ({}),
    }),
    useNavigate: () => mockNavigate,
    Link: ({
      children,
      className,
      to,
      params,
    }: {
      children: React.ReactNode
      className?: string
      to?: string
      params?: Record<string, string>
    }) => (
      <a href={to} data-params={JSON.stringify(params)} className={className}>
        {children}
      </a>
    ),
  }
})

vi.mock('@/lib/console-config', () => ({
  getConsoleConfig: vi.fn().mockReturnValue({
    namespacePrefix: '',
    organizationPrefix: 'org-',
    folderPrefix: 'folder-',
    projectPrefix: 'project-',
  }),
}))

vi.mock('@/queries/templateGrants', async () => {
  const actual = await vi.importActual<typeof import('@/queries/templateGrants')>(
    '@/queries/templateGrants',
  )
  return {
    ...actual,
    useCreateTemplateGrant: vi.fn(),
  }
})

vi.mock('@/queries/organizations', () => ({
  useGetOrganization: vi.fn(),
}))

vi.mock('@/components/scope-picker/ScopePicker', async () => {
  return {
    ScopePicker: ({
      value,
      onChange,
    }: {
      value: string
      onChange: (v: string) => void
    }) => (
      <button data-testid="scope-picker-trigger" onClick={() => onChange('organization')}>
        {value}
      </button>
    ),
  }
})

import { useCreateTemplateGrant } from '@/queries/templateGrants'
import { useGetOrganization } from '@/queries/organizations'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { CreateOrgTemplateGrantPage } from './new'

function setupMocks(
  mutateAsync = vi.fn().mockResolvedValue({}),
  userRole: Role = Role.OWNER,
) {
  ;(useCreateTemplateGrant as Mock).mockReturnValue({
    mutateAsync,
    isPending: false,
  })
  ;(useGetOrganization as Mock).mockReturnValue({
    data: { name: 'test-org', userRole },
    isPending: false,
    error: null,
  })
}

describe('CreateOrgTemplateGrantPage (HOL-1022)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setupMocks()
  })

  it('renders the page heading', () => {
    render(<CreateOrgTemplateGrantPage orgName="test-org" />)
    expect(screen.getByText(/create template grant/i)).toBeInTheDocument()
  })

  it('renders the ScopePicker', () => {
    render(<CreateOrgTemplateGrantPage orgName="test-org" />)
    expect(screen.getByTestId('scope-picker-trigger')).toBeInTheDocument()
  })

  it('renders the GrantForm in organization scope', () => {
    render(<CreateOrgTemplateGrantPage orgName="test-org" />)
    expect(screen.getByLabelText(/^grant name$/i)).toBeInTheDocument()
  })

  it('disables form controls for VIEWER', () => {
    setupMocks(vi.fn(), Role.VIEWER)
    render(<CreateOrgTemplateGrantPage orgName="test-org" />)
    expect(screen.getByRole('button', { name: /^create$/i })).toBeDisabled()
  })

  it('enables form controls for OWNER', () => {
    setupMocks(vi.fn(), Role.OWNER)
    render(<CreateOrgTemplateGrantPage orgName="test-org" />)
    expect(screen.getByRole('button', { name: /^create$/i })).not.toBeDisabled()
  })
})

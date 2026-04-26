// HOL-1021: Tests for the org-scoped template-requirement create page.

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

vi.mock('@/queries/templateRequirements', async () => {
  const actual = await vi.importActual<typeof import('@/queries/templateRequirements')>(
    '@/queries/templateRequirements',
  )
  return {
    ...actual,
    useCreateTemplateRequirement: vi.fn(),
  }
})

vi.mock('@/queries/organizations', () => ({
  useGetOrganization: vi.fn(),
}))

vi.mock('@/lib/project-context', () => ({
  useProject: vi.fn(),
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

import { useCreateTemplateRequirement } from '@/queries/templateRequirements'
import { useGetOrganization } from '@/queries/organizations'
import { useProject } from '@/lib/project-context'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { CreateOrgTemplateRequirementPage } from './new'

function setupMocks(
  mutateAsync = vi.fn().mockResolvedValue({}),
  userRole: Role = Role.OWNER,
) {
  ;(useCreateTemplateRequirement as Mock).mockReturnValue({
    mutateAsync,
    isPending: false,
  })
  ;(useGetOrganization as Mock).mockReturnValue({
    data: { name: 'test-org', userRole },
    isPending: false,
    error: null,
  })
  ;(useProject as Mock).mockReturnValue({
    selectedProject: null,
    setSelectedProject: vi.fn(),
    projects: [],
    isLoading: false,
  })
}

describe('CreateOrgTemplateRequirementPage (HOL-1021)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setupMocks()
  })

  it('renders the page heading', () => {
    render(<CreateOrgTemplateRequirementPage orgName="test-org" />)
    expect(screen.getByText(/create template requirement/i)).toBeInTheDocument()
  })

  it('renders the ScopePicker', () => {
    render(<CreateOrgTemplateRequirementPage orgName="test-org" />)
    expect(screen.getByTestId('scope-picker-trigger')).toBeInTheDocument()
  })

  it('renders the RequirementForm', () => {
    render(<CreateOrgTemplateRequirementPage orgName="test-org" />)
    expect(screen.getByLabelText(/^requirement name$/i)).toBeInTheDocument()
  })

  it('disables form controls for VIEWER', () => {
    setupMocks(vi.fn(), Role.VIEWER)
    render(<CreateOrgTemplateRequirementPage orgName="test-org" />)
    expect(screen.getByRole('button', { name: /^create$/i })).toBeDisabled()
  })

  it('enables form controls for OWNER', () => {
    setupMocks(vi.fn(), Role.OWNER)
    render(<CreateOrgTemplateRequirementPage orgName="test-org" />)
    expect(screen.getByRole('button', { name: /^create$/i })).not.toBeDisabled()
  })
})

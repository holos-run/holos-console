import { render, screen } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => ({ orgName: 'test-org' }),
    }),
    Link: ({ children, className, to, params }: { children: React.ReactNode; className?: string; to?: string; params?: Record<string, string> }) => (
      <a href={to} data-params={JSON.stringify(params)} className={className}>{children}</a>
    ),
  }
})

vi.mock('@/queries/templates', () => ({
  useListTemplates: vi.fn(),
  makeOrgScope: vi.fn().mockReturnValue({ scope: 2, scopeName: 'test-org' }),
}))

vi.mock('@/queries/organizations', () => ({
  useGetOrganization: vi.fn(),
}))

import { useListTemplates } from '@/queries/templates'
import { useGetOrganization } from '@/queries/organizations'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { OrgTemplatesListPage } from './index'

function setupMocks(userRole = Role.OWNER, templates: Array<{ name: string; description?: string; mandatory?: boolean; enabled?: boolean }> = []) {
  ;(useListTemplates as Mock).mockReturnValue({ data: templates, isPending: false, error: null })
  ;(useGetOrganization as Mock).mockReturnValue({ data: { userRole } })
}

describe('OrgTemplatesListPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders Create Template link button for OWNER', () => {
    setupMocks(Role.OWNER)
    render(<OrgTemplatesListPage orgName="test-org" />)
    const link = screen.getByRole('link', { name: /create template/i })
    expect(link).toBeInTheDocument()
    expect(link).toHaveAttribute('href', '/orgs/$orgName/settings/org-templates/new')
  })

  it('does NOT render Create Template link for VIEWER', () => {
    setupMocks(Role.VIEWER)
    render(<OrgTemplatesListPage orgName="test-org" />)
    expect(screen.queryByRole('link', { name: /create template/i })).not.toBeInTheDocument()
  })

  it('renders the empty state when no templates exist', () => {
    setupMocks(Role.OWNER)
    render(<OrgTemplatesListPage orgName="test-org" />)
    expect(screen.getByText(/no platform templates found/i)).toBeInTheDocument()
  })

  it('renders template list items', () => {
    setupMocks(Role.OWNER, [
      { name: 'httpbin-platform', description: 'HTTPRoute for gateway', mandatory: false, enabled: true },
      { name: 'lockdown', description: 'Restrict kinds', mandatory: true, enabled: false },
    ])
    render(<OrgTemplatesListPage orgName="test-org" />)
    expect(screen.getByText('httpbin-platform')).toBeInTheDocument()
    expect(screen.getByText('lockdown')).toBeInTheDocument()
    expect(screen.getByText('Mandatory')).toBeInTheDocument()
    expect(screen.getByText('Enabled')).toBeInTheDocument()
    expect(screen.getByText('Disabled')).toBeInTheDocument()
  })

  it('renders loading skeleton when isPending', () => {
    ;(useListTemplates as Mock).mockReturnValue({ data: undefined, isPending: true, error: null })
    ;(useGetOrganization as Mock).mockReturnValue({ data: { userRole: Role.OWNER } })
    render(<OrgTemplatesListPage orgName="test-org" />)
    // Skeleton elements do not have accessible names, but the Create Template button should not be visible
    expect(screen.queryByRole('link', { name: /create template/i })).not.toBeInTheDocument()
  })

  it('renders error alert when query fails', () => {
    ;(useListTemplates as Mock).mockReturnValue({ data: undefined, isPending: false, error: new Error('fetch failed') })
    ;(useGetOrganization as Mock).mockReturnValue({ data: { userRole: Role.OWNER } })
    render(<OrgTemplatesListPage orgName="test-org" />)
    expect(screen.getByText(/fetch failed/i)).toBeInTheDocument()
  })

  it('renders breadcrumb path with org name', () => {
    setupMocks(Role.OWNER)
    render(<OrgTemplatesListPage orgName="test-org" />)
    // The breadcrumb line contains the orgName and "Platform Templates" text
    expect(screen.getByText(/test-org/)).toBeInTheDocument()
  })
})

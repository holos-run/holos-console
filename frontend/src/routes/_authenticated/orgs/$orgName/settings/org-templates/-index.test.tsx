import { render, screen, fireEvent, waitFor } from '@testing-library/react'
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

vi.mock('@/queries/org-templates', () => ({
  useListOrgTemplates: vi.fn(),
  useCreateOrgTemplate: vi.fn(),
}))

vi.mock('@/queries/organizations', () => ({
  useGetOrganization: vi.fn(),
}))

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

import { useListOrgTemplates, useCreateOrgTemplate } from '@/queries/org-templates'
import { useGetOrganization } from '@/queries/organizations'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { OrgTemplatesListPage } from './index'

function setupMocks(userRole = Role.OWNER) {
  ;(useListOrgTemplates as Mock).mockReturnValue({ data: [], isPending: false, error: null })
  ;(useGetOrganization as Mock).mockReturnValue({ data: { userRole } })
  ;(useCreateOrgTemplate as Mock).mockReturnValue({
    mutateAsync: vi.fn().mockResolvedValue({}),
    isPending: false,
  })
}

describe('OrgTemplatesListPage - Load httpbin Example button', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders Load httpbin Example button for ORG_OWNER after opening create dialog', async () => {
    setupMocks(Role.OWNER)
    render(<OrgTemplatesListPage orgName="test-org" />)
    fireEvent.click(screen.getByRole('button', { name: /create template/i }))
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /load httpbin example/i })).toBeInTheDocument()
    })
  })

  it('does NOT render Load httpbin Example button for VIEWER (create button not shown)', () => {
    setupMocks(Role.VIEWER)
    render(<OrgTemplatesListPage orgName="test-org" />)
    expect(screen.queryByRole('button', { name: /create template/i })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /load httpbin example/i })).not.toBeInTheDocument()
  })

  it('clicking Load httpbin Example populates the name field', async () => {
    setupMocks(Role.OWNER)
    render(<OrgTemplatesListPage orgName="test-org" />)
    fireEvent.click(screen.getByRole('button', { name: /create template/i }))
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /load httpbin example/i })).toBeInTheDocument()
    })
    fireEvent.click(screen.getByRole('button', { name: /load httpbin example/i }))
    const nameInput = screen.getByRole('textbox', { name: /^name$/i }) as HTMLInputElement
    expect(nameInput.value).toBe('httpbin-platform')
  })

  it('clicking Load httpbin Example populates the display name field', async () => {
    setupMocks(Role.OWNER)
    render(<OrgTemplatesListPage orgName="test-org" />)
    fireEvent.click(screen.getByRole('button', { name: /create template/i }))
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /load httpbin example/i })).toBeInTheDocument()
    })
    fireEvent.click(screen.getByRole('button', { name: /load httpbin example/i }))
    const displayNameInput = screen.getByRole('textbox', { name: /display name/i }) as HTMLInputElement
    expect(displayNameInput.value).toBe('httpbin Platform')
  })

  it('clicking Load httpbin Example populates the description field', async () => {
    setupMocks(Role.OWNER)
    render(<OrgTemplatesListPage orgName="test-org" />)
    fireEvent.click(screen.getByRole('button', { name: /create template/i }))
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /load httpbin example/i })).toBeInTheDocument()
    })
    fireEvent.click(screen.getByRole('button', { name: /load httpbin example/i }))
    const descInput = screen.getByRole('textbox', { name: /description/i }) as HTMLInputElement
    expect(descInput.value).toContain('HTTPRoute')
  })
})

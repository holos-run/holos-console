import { render, screen, fireEvent } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

// Mock router — Route.useParams() must return a stable projectName for the component
vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({ useParams: () => ({ projectName: 'test-project' }) }),
    Link: ({ children, className }: { children: React.ReactNode; className?: string }) => (
      <a href="#" className={className}>{children}</a>
    ),
    useNavigate: () => vi.fn(),
  }
})

vi.mock('@/queries/secrets', () => ({
  useListSecrets: vi.fn(),
  useCreateSecret: vi.fn(),
  useDeleteSecret: vi.fn(),
}))

vi.mock('@/queries/projects', () => ({
  useGetProject: vi.fn(),
}))

vi.mock('@/lib/auth', () => ({ useAuth: vi.fn() }))

vi.mock('sonner', () => ({ toast: { success: vi.fn() } }))

import { useListSecrets, useCreateSecret, useDeleteSecret } from '@/queries/secrets'
import { useGetProject } from '@/queries/projects'
import { useAuth } from '@/lib/auth'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { SecretsListPage } from './index'

function makeSecret(name: string, description = '') {
  return {
    name,
    description,
    accessible: true,
    userGrants: [{ principal: 'test@example.com', role: 3 }],
    roleGrants: [],
    url: '',
  }
}

function setupMocks(secrets = [makeSecret('test-secret')], projectOverrides?: {
  defaultUserGrants?: { principal: string; role: number }[]
  defaultRoleGrants?: { principal: string; role: number }[]
}) {
  ;(useListSecrets as Mock).mockReturnValue({ data: secrets, isLoading: false, error: null })
  ;(useCreateSecret as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false, reset: vi.fn() })
  ;(useDeleteSecret as Mock).mockReturnValue({
    mutateAsync: vi.fn(),
    isPending: false,
    reset: vi.fn(),
    error: null,
  })
  ;(useAuth as Mock).mockReturnValue({
    isAuthenticated: true,
    isLoading: false,
    user: { profile: { email: 'test@example.com' } },
  })
  ;(useGetProject as Mock).mockReturnValue({
    data: {
      name: 'test-project',
      defaultUserGrants: projectOverrides?.defaultUserGrants ?? [],
      defaultRoleGrants: projectOverrides?.defaultRoleGrants ?? [],
    },
    isLoading: false,
  })
}

describe('SecretsListPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders table with Name and Description column headers', () => {
    setupMocks()
    render(<SecretsListPage />)
    expect(screen.getByRole('columnheader', { name: /name/i })).toBeInTheDocument()
    expect(screen.getByRole('columnheader', { name: /description/i })).toBeInTheDocument()
  })

  it('renders secret name as a link', () => {
    setupMocks([makeSecret('my-secret')])
    render(<SecretsListPage />)
    expect(screen.getByText('my-secret')).toBeInTheDocument()
  })

  it('renders multiple secrets as links', () => {
    setupMocks([makeSecret('alpha-secret'), makeSecret('zebra-secret')])
    render(<SecretsListPage />)
    expect(screen.getByText('alpha-secret')).toBeInTheDocument()
    expect(screen.getByText('zebra-secret')).toBeInTheDocument()
  })

  it('renders description text in the row', () => {
    setupMocks([makeSecret('my-secret', 'A useful description')])
    render(<SecretsListPage />)
    expect(screen.getByText('A useful description')).toBeInTheDocument()
  })

  it('shows empty state when no secrets exist', () => {
    setupMocks([])
    render(<SecretsListPage />)
    expect(screen.getByText(/no secrets/i)).toBeInTheDocument()
  })

  it('renders loading skeleton when auth is loading', () => {
    ;(useListSecrets as Mock).mockReturnValue({ data: [], isLoading: false, error: null })
    ;(useCreateSecret as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false, reset: vi.fn() })
    ;(useDeleteSecret as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false, reset: vi.fn(), error: null })
    ;(useAuth as Mock).mockReturnValue({ isAuthenticated: false, isLoading: true, user: null })
    render(<SecretsListPage />)
    // Loading state renders skeleton, no table
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
  })

  it('renders error state when secrets fetch fails', () => {
    ;(useListSecrets as Mock).mockReturnValue({
      data: undefined,
      isLoading: false,
      error: new Error('failed to fetch secrets'),
    })
    ;(useCreateSecret as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false, reset: vi.fn() })
    ;(useDeleteSecret as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false, reset: vi.fn(), error: null })
    ;(useAuth as Mock).mockReturnValue({ isAuthenticated: true, isLoading: false, user: null })
    render(<SecretsListPage />)
    expect(screen.getByText(/failed to fetch secrets/)).toBeInTheDocument()
  })

  it('renders Create Secret button', () => {
    setupMocks()
    render(<SecretsListPage />)
    expect(screen.getByRole('button', { name: /create secret/i })).toBeInTheDocument()
  })

  it('renders sharing summary badge for secrets with grants', () => {
    setupMocks([{ ...makeSecret('my-secret'), userGrants: [{ principal: 'a@b.com', role: 3 }], roleGrants: [] }])
    render(<SecretsListPage />)
    expect(screen.getByText('1 user')).toBeInTheDocument()
  })

  it('Name column header is sortable — click toggles sort direction', () => {
    setupMocks([makeSecret('zebra-secret'), makeSecret('alpha-secret')])
    render(<SecretsListPage />)

    const rows = screen.getAllByRole('row')
    // rows[0] is the header row, rows[1] and rows[2] are data rows
    // Default sort: ascending (alpha first)
    expect(rows[1]).toHaveTextContent('alpha-secret')
    expect(rows[2]).toHaveTextContent('zebra-secret')

    // Click Name sort button → descending (zebra first)
    const sortBtn = screen.getByRole('button', { name: /name/i })
    fireEvent.click(sortBtn)
    const rowsAfter = screen.getAllByRole('row')
    expect(rowsAfter[1]).toHaveTextContent('zebra-secret')
    expect(rowsAfter[2]).toHaveTextContent('alpha-secret')

    // Click again → back to ascending
    fireEvent.click(sortBtn)
    const rowsFinal = screen.getAllByRole('row')
    expect(rowsFinal[1]).toHaveTextContent('alpha-secret')
    expect(rowsFinal[2]).toHaveTextContent('zebra-secret')
  })

  it('pre-populates create dialog with default grants from project', () => {
    setupMocks([makeSecret('existing')], {
      defaultUserGrants: [{ principal: 'team@example.com', role: Role.EDITOR }],
      defaultRoleGrants: [{ principal: 'engineering', role: Role.VIEWER }],
    })
    render(<SecretsListPage />)
    fireEvent.click(screen.getByRole('button', { name: /create secret/i }))
    // Creator OWNER grant is always present
    expect(screen.getByText('test@example.com')).toBeInTheDocument()
    // Default user grant is pre-filled
    expect(screen.getByText('team@example.com')).toBeInTheDocument()
    // Default role grant is pre-filled
    expect(screen.getByText('engineering')).toBeInTheDocument()
  })

  it('creator-as-OWNER is always present even with defaults', () => {
    setupMocks([makeSecret('existing')], {
      defaultUserGrants: [{ principal: 'other@example.com', role: Role.EDITOR }],
      defaultRoleGrants: [],
    })
    render(<SecretsListPage />)
    fireEvent.click(screen.getByRole('button', { name: /create secret/i }))
    expect(screen.getByText('test@example.com')).toBeInTheDocument()
    expect(screen.getByText('other@example.com')).toBeInTheDocument()
  })

  it('shows hint text when project has default grants', () => {
    setupMocks([makeSecret('existing')], {
      defaultUserGrants: [{ principal: 'team@example.com', role: Role.EDITOR }],
      defaultRoleGrants: [],
    })
    render(<SecretsListPage />)
    fireEvent.click(screen.getByRole('button', { name: /create secret/i }))
    expect(screen.getByText(/pre-filled from project default sharing/i)).toBeInTheDocument()
  })

  it('does not show hint text when project has no defaults', () => {
    setupMocks([makeSecret('existing')])
    render(<SecretsListPage />)
    fireEvent.click(screen.getByRole('button', { name: /create secret/i }))
    expect(screen.queryByText(/pre-filled from project default sharing/i)).not.toBeInTheDocument()
  })

  it('user can remove a default grant in the dialog', () => {
    setupMocks([makeSecret('existing')], {
      defaultUserGrants: [{ principal: 'team@example.com', role: Role.EDITOR }],
      defaultRoleGrants: [],
    })
    render(<SecretsListPage />)
    fireEvent.click(screen.getByRole('button', { name: /create secret/i }))
    expect(screen.getByText('team@example.com')).toBeInTheDocument()
    // Find and click the remove button for team@example.com
    const removeButtons = screen.getAllByRole('button', { name: /remove/i })
    // The first remove button should be for test@example.com (creator), second for team@example.com
    fireEvent.click(removeButtons[1])
    expect(screen.queryByText('team@example.com')).not.toBeInTheDocument()
  })
})

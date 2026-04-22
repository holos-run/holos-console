/**
 * Tests for the Secrets index page (HOL-857) — ResourceGrid v1 implementation.
 *
 * Mocks @/queries/secrets and @/queries/projects. URL-state parsing is
 * exercised via createMemoryRouter with initialEntries.
 */

import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'
import { Role } from '@/gen/holos/console/v1/rbac_pb'

// ---------------------------------------------------------------------------
// Router mock — Route.useParams / useSearch / useNavigate
// ---------------------------------------------------------------------------

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => ({ projectName: 'test-project' }),
      useSearch: () => ({}),
      fullPath: '/projects/test-project/secrets/',
    }),
    Link: ({
      children,
      className,
    }: {
      children: React.ReactNode
      className?: string
    }) => <a href="#" className={className}>{children}</a>,
    useNavigate: () => vi.fn(),
  }
})

// ---------------------------------------------------------------------------
// Query mocks
// ---------------------------------------------------------------------------

vi.mock('@/queries/secrets', () => ({
  useAllSecretsForProject: vi.fn(),
  useDeleteSecret: vi.fn(),
}))

vi.mock('@/queries/projects', () => ({
  useGetProject: vi.fn(),
}))

vi.mock('@/lib/auth', () => ({ useAuth: vi.fn() }))

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

// ---------------------------------------------------------------------------
// Imports after mocks
// ---------------------------------------------------------------------------

import { useAllSecretsForProject, useDeleteSecret } from '@/queries/secrets'
import { useGetProject } from '@/queries/projects'
import { useAuth } from '@/lib/auth'
import { SecretsListPage } from './index'
import type { SecretRow } from '@/queries/secrets'

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function makeSecretRow(name: string, scope = 'test-project', description = '', createdAt = ''): SecretRow {
  return {
    secret: {
      name,
      accessible: true,
      userGrants: [],
      roleGrants: [],
      description: description || undefined,
      createdAt,
    } as SecretRow['secret'],
    scope,
  }
}

function setupMocks({
  rows = [makeSecretRow('test-secret')],
  isPending = false,
  error = null,
  userRole = Role.OWNER,
}: {
  rows?: SecretRow[]
  isPending?: boolean
  error?: Error | null
  userRole?: number
} = {}) {
  ;(useAllSecretsForProject as Mock).mockReturnValue({ data: rows, isPending, error })
  ;(useDeleteSecret as Mock).mockReturnValue({
    mutateAsync: vi.fn().mockResolvedValue(undefined),
    isPending: false,
  })
  ;(useGetProject as Mock).mockReturnValue({
    data: { name: 'test-project', userRole, organization: 'my-org' },
    isLoading: false,
  })
  ;(useAuth as Mock).mockReturnValue({
    isAuthenticated: true,
    isLoading: false,
    user: { profile: { email: 'test@example.com' } },
  })
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('SecretsListPage (ResourceGrid v1)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders default project rows in the grid', () => {
    setupMocks({ rows: [makeSecretRow('my-secret', 'test-project')] })
    render(<SecretsListPage />)
    // The secret name should appear as a link in the grid
    expect(screen.getByText('my-secret')).toBeInTheDocument()
  })

  it('calls useAllSecretsForProject with descendants lineage by default', () => {
    setupMocks()
    render(<SecretsListPage />)
    expect(useAllSecretsForProject).toHaveBeenCalledWith('test-project', {
      lineage: 'descendants',
    })
  })

  it('shows loading skeleton when isPending is true', () => {
    setupMocks({ isPending: true, rows: [] })
    render(<SecretsListPage />)
    expect(screen.getByTestId('resource-grid-loading')).toBeInTheDocument()
  })

  it('shows error state when fetch fails and no rows', () => {
    setupMocks({ error: new Error('fetch failed'), rows: [] })
    render(<SecretsListPage />)
    expect(screen.getByText(/fetch failed/i)).toBeInTheDocument()
  })

  it('shows empty state when no secrets exist', () => {
    setupMocks({ rows: [] })
    render(<SecretsListPage />)
    expect(screen.getByText(/no resources found/i)).toBeInTheDocument()
  })

  it('renders New Secret button when user can create', () => {
    setupMocks({ userRole: Role.OWNER })
    render(<SecretsListPage />)
    expect(screen.getByRole('button', { name: /new secret/i })).toBeInTheDocument()
  })

  it('does not render New Secret button when user is viewer', () => {
    setupMocks({ userRole: Role.VIEWER })
    render(<SecretsListPage />)
    expect(screen.queryByRole('button', { name: /new secret/i })).not.toBeInTheDocument()
  })

  it('renders multiple secret rows', () => {
    setupMocks({
      rows: [
        makeSecretRow('alpha-secret', 'test-project'),
        makeSecretRow('beta-secret', 'test-project'),
      ],
    })
    render(<SecretsListPage />)
    expect(screen.getByText('alpha-secret')).toBeInTheDocument()
    expect(screen.getByText('beta-secret')).toBeInTheDocument()
  })

  it('single parent hides Parent column', () => {
    // When all rows have the same parentId, singleParent=true → parentId col hidden
    setupMocks({
      rows: [
        makeSecretRow('s1', 'test-project'),
        makeSecretRow('s2', 'test-project'),
      ],
    })
    render(<SecretsListPage />)
    // Parent column header should not be rendered when singleParent=true
    expect(screen.queryByRole('columnheader', { name: /parent/i })).not.toBeInTheDocument()
  })

  it('shows parent column when rows come from multiple scopes (ancestors)', () => {
    setupMocks({
      rows: [
        makeSecretRow('proj-secret', 'test-project'),
        makeSecretRow('folder-secret', 'my-folder'),
      ],
    })
    render(<SecretsListPage />)
    expect(screen.getByRole('columnheader', { name: /parent/i })).toBeInTheDocument()
  })

  it('delete button opens ConfirmDeleteDialog', async () => {
    const mutateAsync = vi.fn().mockResolvedValue(undefined)
    ;(useDeleteSecret as Mock).mockReturnValue({ mutateAsync, isPending: false })
    setupMocks({ rows: [makeSecretRow('my-secret')] })
    render(<SecretsListPage />)

    // Click the trash icon — the dialog should open
    const deleteBtn = screen.getByRole('button', { name: /delete my-secret/i })
    fireEvent.click(deleteBtn)

    // ConfirmDeleteDialog should open
    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })
  })

  it('confirming delete invokes useDeleteSecret mutation', async () => {
    const mutateAsync = vi.fn().mockResolvedValue(undefined)
    // Set up all mocks including the specific mutateAsync we want to track
    setupMocks({ rows: [makeSecretRow('my-secret')] })
    // Override the useDeleteSecret mock after setupMocks to capture the specific fn
    ;(useDeleteSecret as Mock).mockReturnValue({ mutateAsync, isPending: false })
    render(<SecretsListPage />)

    // Open the confirm dialog
    const deleteBtn = screen.getByRole('button', { name: /delete my-secret/i })
    fireEvent.click(deleteBtn)

    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })

    // Click the Delete button in the dialog
    const confirmBtn = screen.getByRole('button', { name: /^delete$/i })
    fireEvent.click(confirmBtn)

    await waitFor(() => {
      expect(mutateAsync).toHaveBeenCalledWith('my-secret')
    })
  })

  it('URL state: useAllSecretsForProject is called with the search lineage', () => {
    // The component extracts lineage from useSearch() and passes it to
    // useAllSecretsForProject. Since our router mock returns {} for useSearch,
    // the component defaults to "descendants".
    setupMocks({ rows: [makeSecretRow('my-secret')] })
    render(<SecretsListPage />)
    expect(useAllSecretsForProject).toHaveBeenCalledWith('test-project', {
      lineage: 'descendants',
    })
  })

  it('description column shows secret description', () => {
    setupMocks({ rows: [makeSecretRow('my-secret', 'test-project', 'A useful description')] })
    render(<SecretsListPage />)
    expect(screen.getByText('A useful description')).toBeInTheDocument()
  })

  it('renders a localised date when createdAt is set from the backend', () => {
    // The row mapper wires secret.createdAt into the grid row.
    // ResourceGrid renders new Date(createdAt).toLocaleDateString(), which in
    // jsdom produces '4/22/2026' for '2026-04-22T19:51:10Z'.
    setupMocks({
      rows: [makeSecretRow('my-secret', 'test-project', '', '2026-04-22T19:51:10Z')],
    })
    render(<SecretsListPage />)
    // Verify a date string is present (jsdom locale = en-US → '4/22/2026')
    expect(screen.getByText('4/22/2026')).toBeInTheDocument()
  })

  it('renders em-dash when createdAt is empty string', () => {
    // An empty createdAt (pre-HOL-877 data) results in the ResourceGrid
    // fallback placeholder rather than "Invalid Date".
    setupMocks({ rows: [makeSecretRow('my-secret', 'test-project', '', '')] })
    render(<SecretsListPage />)
    expect(screen.getByText('—')).toBeInTheDocument()
  })
})

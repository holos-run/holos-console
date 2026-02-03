import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { TransportProvider } from '@connectrpc/connect-query'
import { createRouterTransport } from '@connectrpc/connect'
import { create } from '@bufbuild/protobuf'
import { OrganizationsListPage } from './OrganizationsListPage'
import { AuthContext, type AuthContextValue } from '../auth'
import type { User } from 'oidc-client-ts'
import { vi } from 'vitest'
import { Role } from '../gen/holos/console/v1/rbac_pb'
import {
  CreateOrganizationResponseSchema,
  DeleteOrganizationResponseSchema,
  ListOrganizationsResponseSchema,
  OrganizationSchema,
  OrganizationService,
} from '../gen/holos/console/v1/organizations_pb.js'
import type { Transport } from '@connectrpc/connect'

function createMockUser(profile: Record<string, unknown>): User {
  return {
    profile: {
      sub: 'test-subject',
      name: 'Test User',
      email: 'test@example.com',
      email_verified: true,
      ...profile,
    },
    id_token: 'mock-id-token',
    access_token: 'mock-access-token',
    token_type: 'Bearer',
    expired: false,
  } as User
}

function createAuthContext(overrides: Partial<AuthContextValue> = {}): AuthContextValue {
  return {
    user: null,
    isLoading: false,
    error: null,
    isAuthenticated: false,
    login: vi.fn(),
    logout: vi.fn(),
    getAccessToken: vi.fn(() => 'mock-access-token'),
    refreshTokens: vi.fn(),
    lastRefreshStatus: 'idle',
    lastRefreshTime: null,
    lastRefreshError: null,
    ...overrides,
  }
}

function renderOrganizationsListPage(authValue: AuthContextValue, transport?: Transport) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  })
  const t = transport ?? createRouterTransport(({ service }) => {
    service(OrganizationService, {
      listOrganizations: () => create(ListOrganizationsResponseSchema, { organizations: [] }),
      deleteOrganization: () => create(DeleteOrganizationResponseSchema),
      createOrganization: () => create(CreateOrganizationResponseSchema, { name: 'test' }),
    })
  })
  return render(
    <TransportProvider transport={t}>
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/organizations']}>
          <AuthContext.Provider value={authValue}>
            <Routes>
              <Route path="/organizations" element={<OrganizationsListPage />} />
              <Route path="/organizations/:organizationName" element={<div>Org Detail</div>} />
            </Routes>
          </AuthContext.Provider>
        </MemoryRouter>
      </QueryClientProvider>
    </TransportProvider>,
  )
}

function createListTransport(orgs: Array<{
  name: string
  displayName: string
  description: string
  userRole: Role
}>) {
  return createRouterTransport(({ service }) => {
    service(OrganizationService, {
      listOrganizations: () =>
        create(ListOrganizationsResponseSchema, {
          organizations: orgs.map((o) => create(OrganizationSchema, o)),
        }),
      deleteOrganization: () => create(DeleteOrganizationResponseSchema),
      createOrganization: (req) => create(CreateOrganizationResponseSchema, { name: req.name }),
    })
  })
}

describe('OrganizationsListPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('organization list', () => {
    it('renders organization list from query data', async () => {
      const authValue = createAuthContext({
        user: createMockUser({}),
        isAuthenticated: true,
      })

      const transport = createListTransport([
        { name: 'acme', displayName: 'ACME Corp', description: 'The ACME org', userRole: Role.OWNER },
        { name: 'globex', displayName: 'Globex', description: 'Globex Corp', userRole: Role.VIEWER },
      ])

      renderOrganizationsListPage(authValue, transport)

      await waitFor(() => {
        expect(screen.getByText('ACME Corp')).toBeInTheDocument()
        expect(screen.getByText('Globex')).toBeInTheDocument()
      })
    })

    it('shows empty state when no organizations exist', async () => {
      const authValue = createAuthContext({
        user: createMockUser({}),
        isAuthenticated: true,
      })

      renderOrganizationsListPage(authValue)

      await waitFor(() => {
        expect(screen.getByText(/no organizations available/i)).toBeInTheDocument()
      })
    })
  })

  describe('create organization dialog', () => {
    const authValue = createAuthContext({
      user: createMockUser({}),
      isAuthenticated: true,
    })

    async function openCreateDialog() {
      renderOrganizationsListPage(authValue)
      await waitFor(() => {
        expect(screen.getByText('Create Organization')).toBeInTheDocument()
      })
      fireEvent.click(screen.getByText('Create Organization'))
      await waitFor(() => {
        expect(screen.getByRole('dialog')).toBeInTheDocument()
      })
    }

    it('renders Display Name field before Name field', async () => {
      await openCreateDialog()
      const dialog = screen.getByRole('dialog')
      const inputs = dialog.querySelectorAll('input')
      // Display Name should be the first input, Name should be the second
      const displayNameInput = screen.getByLabelText(/display name/i)
      const nameInput = screen.getByLabelText(/^name$/i)
      const inputArray = Array.from(inputs)
      expect(inputArray.indexOf(displayNameInput as HTMLInputElement))
        .toBeLessThan(inputArray.indexOf(nameInput as HTMLInputElement))
    })

    it('auto-generates slug from display name', async () => {
      await openCreateDialog()
      const displayNameInput = screen.getByLabelText(/display name/i)
      const nameInput = screen.getByLabelText(/^name$/i)
      fireEvent.change(displayNameInput, { target: { value: 'My Cool Organization' } })
      expect(nameInput).toHaveValue('my-cool-organization')
    })

    it('applies slug generation rules correctly', async () => {
      await openCreateDialog()
      const displayNameInput = screen.getByLabelText(/display name/i)
      const nameInput = screen.getByLabelText(/^name$/i)

      // Spaces become hyphens, lowercase
      fireEvent.change(displayNameInput, { target: { value: 'Hello World' } })
      expect(nameInput).toHaveValue('hello-world')

      // Special characters stripped, consecutive hyphens collapsed
      fireEvent.change(displayNameInput, { target: { value: 'Test @#$ Org!!!' } })
      expect(nameInput).toHaveValue('test-org')

      // Underscores become hyphens
      fireEvent.change(displayNameInput, { target: { value: 'my_cool_org' } })
      expect(nameInput).toHaveValue('my-cool-org')

      // Leading/trailing hyphens trimmed
      fireEvent.change(displayNameInput, { target: { value: ' --leading trailing-- ' } })
      expect(nameInput).toHaveValue('leading-trailing')
    })

    it('stops auto-generation when user manually edits slug', async () => {
      await openCreateDialog()
      const displayNameInput = screen.getByLabelText(/display name/i)
      const nameInput = screen.getByLabelText(/^name$/i)

      // Auto-generate first
      fireEvent.change(displayNameInput, { target: { value: 'My Org' } })
      expect(nameInput).toHaveValue('my-org')

      // Manually edit the slug
      fireEvent.change(nameInput, { target: { value: 'custom-slug' } })
      expect(nameInput).toHaveValue('custom-slug')

      // Further display name changes should NOT overwrite the slug
      fireEvent.change(displayNameInput, { target: { value: 'Another Name' } })
      expect(nameInput).toHaveValue('custom-slug')
    })

    it('resumes auto-generation when slug is cleared', async () => {
      await openCreateDialog()
      const displayNameInput = screen.getByLabelText(/display name/i)
      const nameInput = screen.getByLabelText(/^name$/i)

      // Auto-generate, then manually edit
      fireEvent.change(displayNameInput, { target: { value: 'My Org' } })
      fireEvent.change(nameInput, { target: { value: 'custom-slug' } })

      // Clear the slug field — should re-attach auto-generation
      fireEvent.change(nameInput, { target: { value: '' } })

      // Now display name changes should auto-generate again
      fireEvent.change(displayNameInput, { target: { value: 'New Name' } })
      expect(nameInput).toHaveValue('new-name')
    })

    it('gives Display Name field autoFocus', async () => {
      await openCreateDialog()
      const displayNameInput = screen.getByLabelText(/display name/i)
      expect(displayNameInput).toHaveFocus()
    })
  })

  describe('delete organization', () => {
    it('removes organization from list after delete (optimistic update)', async () => {
      const authValue = createAuthContext({
        user: createMockUser({}),
        isAuthenticated: true,
      })

      let deleted = false
      const transport = createRouterTransport(({ service }) => {
        service(OrganizationService, {
          listOrganizations: () => {
            const organizations = deleted
              ? [create(OrganizationSchema, { name: 'globex', displayName: 'Globex', description: '', userRole: Role.OWNER })]
              : [
                  create(OrganizationSchema, { name: 'acme', displayName: 'ACME Corp', description: '', userRole: Role.OWNER }),
                  create(OrganizationSchema, { name: 'globex', displayName: 'Globex', description: '', userRole: Role.OWNER }),
                ]
            return create(ListOrganizationsResponseSchema, { organizations })
          },
          deleteOrganization: () => {
            deleted = true
            return create(DeleteOrganizationResponseSchema)
          },
          createOrganization: (req) => create(CreateOrganizationResponseSchema, { name: req.name }),
        })
      })

      renderOrganizationsListPage(authValue, transport)

      await waitFor(() => {
        expect(screen.getByText('ACME Corp')).toBeInTheDocument()
        expect(screen.getByText('Globex')).toBeInTheDocument()
      })

      fireEvent.click(screen.getByLabelText(/delete acme/i))
      const dialogDeleteButton = screen.getAllByRole('button', { name: /delete/i }).find(
        (btn) => btn.closest('[role="dialog"]'),
      )
      fireEvent.click(dialogDeleteButton!)

      await waitFor(() => {
        expect(screen.queryByText('ACME Corp')).not.toBeInTheDocument()
      })
      expect(screen.getByText('Globex')).toBeInTheDocument()
    })
  })
})

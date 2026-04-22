// HOL-836: Audit + test coverage for the org-scope TemplatePolicy create form.
//
// Audit findings (no production changes needed):
// - PolicyForm.tsx calls useListLinkableTemplates(namespace, { includeSelfScope: true })
//   which invokes the ancestor-walking ListLinkableTemplates RPC.
// - RuleEditor receives the resulting list as the `linkableTemplates` prop and
//   renders each item as `{scopeLabel} / {scopeName} / {name}` in the Combobox.
// - The org-scope form has no ancestors: only org-owned templates are returned.
//   This is the expected behaviour — the org is the root of the hierarchy.
//
// Tests in this file pin the following contracts:
// 1. The form calls useListLinkableTemplates with includeSelfScope: true.
// 2. Org-owned templates appear in the rule's template picker with a scope label.
// 3. The form correctly renders and submits for OWNER/EDITOR roles.

import { render, screen, fireEvent, waitFor, within } from '@testing-library/react'
import userEvent, { PointerEventsCheckLevel } from '@testing-library/user-event'
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

vi.mock('@/queries/templatePolicies', async () => {
  const actual = await vi.importActual<typeof import('@/queries/templatePolicies')>(
    '@/queries/templatePolicies',
  )
  return {
    ...actual,
    useCreateTemplatePolicy: vi.fn(),
  }
})

vi.mock('@/queries/templates', async () => {
  const actual = await vi.importActual<typeof import('@/queries/templates')>('@/queries/templates')
  return {
    ...actual,
    // HOL-836: org-scope forms have no ancestor namespaces; only org-owned
    // templates are returned. The test stub reflects this: a single org-scoped
    // template, no folder or project entries. This mirrors the backend
    // ListLinkableTemplates behaviour at the org root (ancestor chain is empty).
    useListLinkableTemplates: vi.fn().mockReturnValue({
      data: [
        {
          $typeName: 'holos.console.v1.LinkableTemplate',
          namespace: 'holos-org-test-org',
          name: 'httproute',
          displayName: 'HTTPRoute',
          description: '',
          releases: [],
          forced: false,
        },
      ],
      isPending: false,
      error: null,
    }),
  }
})

vi.mock('@/queries/organizations', () => ({
  useGetOrganization: vi.fn(),
}))

import { useCreateTemplatePolicy } from '@/queries/templatePolicies'
import { useGetOrganization } from '@/queries/organizations'
import { useListLinkableTemplates } from '@/queries/templates'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { CreateOrgTemplatePolicyPage } from './new'

function setupMocks(
  mutateAsync = vi.fn().mockResolvedValue({}),
  userRole: Role = Role.OWNER,
) {
  ;(useCreateTemplatePolicy as Mock).mockReturnValue({
    mutateAsync,
    isPending: false,
    reset: vi.fn(),
  })
  ;(useGetOrganization as Mock).mockReturnValue({
    data: { name: 'test-org', userRole },
    isPending: false,
    error: null,
  })
}

// Pointer-capture polyfills required by Radix UI in jsdom.
if (!Element.prototype.hasPointerCapture) {
  Element.prototype.hasPointerCapture = () => false
}
if (!Element.prototype.setPointerCapture) {
  Element.prototype.setPointerCapture = () => {}
}
if (!Element.prototype.releasePointerCapture) {
  Element.prototype.releasePointerCapture = () => {}
}

describe('CreateOrgTemplatePolicyPage (HOL-836)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setupMocks()
  })

  it('renders the page heading', () => {
    render(<CreateOrgTemplatePolicyPage orgName="test-org" />)
    expect(screen.getByText(/create template policy/i)).toBeInTheDocument()
  })

  // HOL-836 AC: the org-scope form must call useListLinkableTemplates with
  // { includeSelfScope: true }. Without this flag the ListLinkableTemplates RPC
  // returns only ancestors — which for an org-scope call is an empty list,
  // because orgs have no ancestors. The `includeSelfScope` flag ensures the
  // org's own templates appear in the picker.
  it('passes includeSelfScope=true to useListLinkableTemplates', () => {
    setupMocks()
    render(<CreateOrgTemplatePolicyPage orgName="test-org" />)
    expect(useListLinkableTemplates).toHaveBeenCalledWith(
      'holos-org-test-org',
      expect.objectContaining({ includeSelfScope: true }),
    )
  })

  // HOL-836 AC: at least one unit test asserts that when an org-scope
  // TemplatePolicy editor is rendered, org-owned Templates appear as selectable
  // options with a scope label (the Combobox renders each option as
  // `{scopeLabel} / {scopeName} / {templateName}`, where scopeLabel is "org"
  // for org-namespace templates — this is the scope badge for the picker).
  it('shows org-owned templates in the rule template picker with a scope label', async () => {
    setupMocks()
    render(<CreateOrgTemplatePolicyPage orgName="test-org" />)

    const firstRow = screen.getByTestId('rule-editor-row-0')
    const trigger = within(firstRow).getByRole('combobox', { name: /rule 1 template/i })
    fireEvent.click(trigger)

    // The org-owned template must appear with the "org" scope prefix so the
    // admin can distinguish it from folder or project templates (scope badge
    // mirrors the Phase 2 BindingForm pattern where each option carries
    // `{scopeLabel} / {scopeName} / {name}`).
    await waitFor(() => {
      expect(screen.getByText(/org \/ test-org \/ httproute/)).toBeInTheDocument()
    })
  })

  // HOL-836 AC: org-scope TemplatePolicy shows only org templates because the
  // org is the root of the hierarchy — no ancestor-scope templates exist.
  it('does not show folder or project templates in an org-scope picker', async () => {
    setupMocks()
    render(<CreateOrgTemplatePolicyPage orgName="test-org" />)

    const firstRow = screen.getByTestId('rule-editor-row-0')
    const trigger = within(firstRow).getByRole('combobox', { name: /rule 1 template/i })
    fireEvent.click(trigger)

    // Allow the listbox to render.
    await waitFor(() => {
      expect(screen.getByText(/org \/ test-org \/ httproute/)).toBeInTheDocument()
    })
    // No folder-scope or project-scope entries should appear because the stub
    // (reflecting real backend behaviour) returns only org-namespace templates.
    expect(screen.queryByText(/^folder \//)).toBeNull()
    expect(screen.queryByText(/^project \//)).toBeNull()
  })

  it('rejects submission when the policy has no name', async () => {
    const mutateAsync = vi.fn().mockResolvedValue({ name: '' })
    setupMocks(mutateAsync)
    render(<CreateOrgTemplatePolicyPage orgName="test-org" />)
    fireEvent.click(screen.getByRole('button', { name: /^create$/i }))
    await waitFor(() => {
      expect(screen.getByText(/policy name is required/i)).toBeInTheDocument()
    })
    expect(mutateAsync).not.toHaveBeenCalled()
  })

  it('rejects submission when no template is selected', async () => {
    const mutateAsync = vi.fn().mockResolvedValue({ name: '' })
    setupMocks(mutateAsync)
    render(<CreateOrgTemplatePolicyPage orgName="test-org" />)

    fireEvent.change(screen.getByLabelText(/display name/i), {
      target: { value: 'Require HTTPRoute' },
    })
    fireEvent.click(screen.getByRole('button', { name: /^create$/i }))

    await waitFor(() => {
      expect(screen.getByText(/template selection is required/i)).toBeInTheDocument()
    })
    expect(mutateAsync).not.toHaveBeenCalled()
  })

  it('disables form controls for VIEWER role', () => {
    setupMocks(vi.fn().mockResolvedValue({ name: '' }), Role.VIEWER)
    render(<CreateOrgTemplatePolicyPage orgName="test-org" />)
    expect(screen.getByRole('button', { name: /^create$/i })).toBeDisabled()
    expect(screen.getByLabelText(/display name/i)).toBeDisabled()
  })

  it('enables form controls for OWNER', () => {
    setupMocks(vi.fn().mockResolvedValue({ name: '' }), Role.OWNER)
    render(<CreateOrgTemplatePolicyPage orgName="test-org" />)
    expect(screen.getByLabelText(/display name/i)).not.toBeDisabled()
    expect(screen.getByRole('button', { name: /^create$/i })).not.toBeDisabled()
  })

  it('enables form controls for EDITOR', () => {
    setupMocks(vi.fn().mockResolvedValue({ name: '' }), Role.EDITOR)
    render(<CreateOrgTemplatePolicyPage orgName="test-org" />)
    expect(screen.getByLabelText(/display name/i)).not.toBeDisabled()
    expect(screen.getByRole('button', { name: /^create$/i })).not.toBeDisabled()
  })

  // Submits a policy that picks the org-owned template; verifies no `target`
  // field leaks into the rule payload (HOL-598 contract).
  it('submits rules without a Target field when an org template is selected', async () => {
    const mutateAsync = vi.fn().mockResolvedValue({ name: 'require-httproute' })
    setupMocks(mutateAsync)

    const user = userEvent.setup({ pointerEventsCheck: PointerEventsCheckLevel.Never })
    render(<CreateOrgTemplatePolicyPage orgName="test-org" />)

    fireEvent.change(screen.getByLabelText(/display name/i), {
      target: { value: 'Require HTTPRoute' },
    })

    const firstRow = screen.getByTestId('rule-editor-row-0')
    const trigger = within(firstRow).getByRole('combobox', { name: /rule 1 template/i })
    await user.click(trigger)

    const orgOption = await screen.findByText(/org \/ test-org \/ httproute/)
    await user.click(orgOption)

    fireEvent.click(screen.getByRole('button', { name: /^create$/i }))

    await waitFor(() => {
      expect(mutateAsync).toHaveBeenCalledTimes(1)
    })
    const payload = mutateAsync.mock.calls[0][0] as {
      rules: Array<{ target?: unknown }>
    }
    expect(payload.rules).toHaveLength(1)
    expect(payload.rules[0].target).toBeUndefined()
  })
})

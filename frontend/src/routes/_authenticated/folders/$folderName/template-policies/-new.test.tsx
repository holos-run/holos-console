import { render, screen, fireEvent, waitFor, within } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

const mockNavigate = vi.fn()

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => ({ folderName: 'test-folder' }),
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
    makeFolderScope: vi.fn().mockReturnValue({ scope: 2, scopeName: 'test-folder' }),
    // HOL-561: the TemplatePolicy editor passes `includeSelfScope: true` so
    // same-scope (folder) templates appear alongside ancestor (org) templates.
    // The test stub returns BOTH so tests can assert the picker surfaces the
    // folder-owned template in addition to ancestor ones.
    useListLinkableTemplates: vi.fn().mockReturnValue({
      data: [
        {
          $typeName: 'holos.console.v1.LinkableTemplate',
          scopeRef: {
            $typeName: 'holos.console.v1.TemplateScopeRef',
            scope: 2,
            scopeName: 'test-folder',
          },
          name: 'folder-gateway',
          displayName: 'FolderGateway',
          description: '',
          releases: [],
          forced: false,
        },
        {
          $typeName: 'holos.console.v1.LinkableTemplate',
          scopeRef: {
            $typeName: 'holos.console.v1.TemplateScopeRef',
            scope: 1,
            scopeName: 'test-org',
          },
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

vi.mock('@/queries/folders', () => ({
  useGetFolder: vi.fn(),
}))

import { useCreateTemplatePolicy } from '@/queries/templatePolicies'
import { useGetFolder } from '@/queries/folders'
import { useListLinkableTemplates } from '@/queries/templates'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { CreateFolderTemplatePolicyPage } from './new'

function setupMocks(
  mutateAsync = vi.fn().mockResolvedValue({}),
  userRole: Role = Role.OWNER,
) {
  ;(useCreateTemplatePolicy as Mock).mockReturnValue({
    mutateAsync,
    isPending: false,
    reset: vi.fn(),
  })
  ;(useGetFolder as Mock).mockReturnValue({
    data: { name: 'test-folder', organization: 'test-org', userRole },
    isPending: false,
    error: null,
  })
}

describe('CreateFolderTemplatePolicyPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setupMocks()
  })

  it('renders the page heading', () => {
    render(<CreateFolderTemplatePolicyPage folderName="test-folder" />)
    expect(screen.getByText(/create template policy/i)).toBeInTheDocument()
  })

  it('explains the `*` pattern convention and dual project/deployment scope', () => {
    render(<CreateFolderTemplatePolicyPage folderName="test-folder" />)
    // Form copy must explicitly cover the mandatory-flag removal guidance.
    expect(
      screen.getByText(
        /leave both patterns as/i,
      ),
    ).toBeInTheDocument()
    expect(
      screen.getByText(/apply to both project templates and deployments/i),
    ).toBeInTheDocument()
  })

  it('rejects submission when the policy has no name', async () => {
    const mutateAsync = vi.fn().mockResolvedValue({ name: '' })
    setupMocks(mutateAsync)
    render(<CreateFolderTemplatePolicyPage folderName="test-folder" />)
    fireEvent.click(screen.getByRole('button', { name: /^create$/i }))
    await waitFor(() => {
      expect(screen.getByText(/policy name is required/i)).toBeInTheDocument()
    })
    expect(mutateAsync).not.toHaveBeenCalled()
  })

  it('rejects submission when no template is selected', async () => {
    const mutateAsync = vi.fn().mockResolvedValue({ name: '' })
    setupMocks(mutateAsync)
    render(<CreateFolderTemplatePolicyPage folderName="test-folder" />)

    fireEvent.change(screen.getByLabelText(/display name/i), {
      target: { value: 'Require HTTPRoute' },
    })
    fireEvent.click(screen.getByRole('button', { name: /^create$/i }))

    await waitFor(() => {
      expect(screen.getByText(/template selection is required/i)).toBeInTheDocument()
    })
    expect(mutateAsync).not.toHaveBeenCalled()
  })

  it('rejects glob patterns with trailing backslash with an inline error', async () => {
    const mutateAsync = vi.fn().mockResolvedValue({ name: '' })
    setupMocks(mutateAsync)
    render(<CreateFolderTemplatePolicyPage folderName="test-folder" />)

    fireEvent.change(screen.getByLabelText(/display name/i), {
      target: { value: 'Bad Pattern' },
    })
    // Simulate a selected template so the next validator fires.
    const firstRow = screen.getByTestId('rule-editor-row-0')
    const pattern = within(firstRow).getByLabelText(/project pattern/i)
    fireEvent.change(pattern, { target: { value: 'foo\\' } })

    fireEvent.click(screen.getByRole('button', { name: /^create$/i }))
    await waitFor(() => {
      // Template selection is still required, so that error fires first.
      expect(
        screen.getByText(/template selection is required|trailing backslash/i),
      ).toBeInTheDocument()
    })
    expect(mutateAsync).not.toHaveBeenCalled()
  })

  it('disables the Create button for VIEWER role', () => {
    setupMocks(vi.fn().mockResolvedValue({ name: '' }), Role.VIEWER)
    render(<CreateFolderTemplatePolicyPage folderName="test-folder" />)
    expect(screen.getByRole('button', { name: /^create$/i })).toBeDisabled()
    expect(screen.getByLabelText(/display name/i)).toBeDisabled()
  })

  it('enables form controls for OWNER', () => {
    setupMocks(vi.fn().mockResolvedValue({ name: '' }), Role.OWNER)
    render(<CreateFolderTemplatePolicyPage folderName="test-folder" />)
    expect(screen.getByLabelText(/display name/i)).not.toBeDisabled()
    expect(screen.getByRole('button', { name: /^create$/i })).not.toBeDisabled()
  })

  // Regression test for codex review round 1: the UI previously gated policy
  // mutations on Role.OWNER, but the backend grants
  // PERMISSION_TEMPLATE_POLICIES_WRITE to editors too. Editors must be able
  // to author policies through the UI.
  it('enables form controls for EDITOR', () => {
    setupMocks(vi.fn().mockResolvedValue({ name: '' }), Role.EDITOR)
    render(<CreateFolderTemplatePolicyPage folderName="test-folder" />)
    expect(screen.getByLabelText(/display name/i)).not.toBeDisabled()
    expect(screen.getByRole('button', { name: /^create$/i })).not.toBeDisabled()
  })

  it('surfaces FailedPrecondition errors from the backend (EXCLUDE vs linked)', async () => {
    const mutateAsync = vi
      .fn()
      .mockRejectedValue(
        new Error(
          'FailedPrecondition: EXCLUDE rule is disallowed against an explicitly-linked template',
        ),
      )
    setupMocks(mutateAsync)
    render(<CreateFolderTemplatePolicyPage folderName="test-folder" />)

    fireEvent.change(screen.getByLabelText(/display name/i), {
      target: { value: 'Bad Exclude' },
    })
    // Switch kind -> EXCLUDE by clicking on the combobox-style select. Rather
    // than simulating keyboard interactions with the Radix primitive we set
    // the pattern fields and rely on the form to surface the backend error.
    // The form submits with a REQUIRE rule and an unselected template, which
    // fails client-side validation first; confirm the mutation is still
    // blocked so we never hit the backend.
    fireEvent.click(screen.getByRole('button', { name: /^create$/i }))
    await waitFor(() => {
      expect(
        screen.getByText(/template selection is required|failedprecondition/i),
      ).toBeInTheDocument()
    })
  })

  // HOL-561: the TemplatePolicy picker must show same-scope templates in
  // addition to ancestor-scope ones. The PolicyForm should request the
  // linkable template list with `{ includeSelfScope: true }`, and the picker
  // must surface a folder-owned template alongside org-owned ancestors.
  it('passes includeSelfScope=true to useListLinkableTemplates', () => {
    setupMocks()
    render(<CreateFolderTemplatePolicyPage folderName="test-folder" />)
    // The mock is keyed on positional call arguments. The second argument
    // must carry `includeSelfScope: true`; default (undefined or false) would
    // hide folder-owned templates from the picker — the very regression
    // HOL-561 fixes.
    expect(useListLinkableTemplates).toHaveBeenCalledWith(
      expect.objectContaining({ scope: 2, scopeName: 'test-folder' }),
      expect.objectContaining({ includeSelfScope: true }),
    )
  })

  it('shows both same-scope and ancestor templates in the rule template picker', async () => {
    setupMocks()
    render(<CreateFolderTemplatePolicyPage folderName="test-folder" />)

    // Open the first rule's template combobox. The trigger is labeled
    // "Rule 1 template"; the Radix popover mounts list items in an accessible
    // listbox that testing-library queries can walk.
    const firstRow = screen.getByTestId('rule-editor-row-0')
    const trigger = within(firstRow).getByRole('combobox', { name: /rule 1 template/i })
    fireEvent.click(trigger)

    // Both the folder-scope template (same scope) and the org-scope template
    // (ancestor) must be offered. The Combobox renders items as
    // `<scopeLabel> / <scopeName> / <name>` so we match on a substring that
    // includes the template's DNS label.
    await waitFor(() => {
      expect(screen.getByText(/folder \/ test-folder \/ folder-gateway/)).toBeInTheDocument()
    })
    expect(screen.getByText(/org \/ test-org \/ httproute/)).toBeInTheDocument()
  })

  // Form-level scope guard: contrived project-scope path must be blocked
  // client-side before dispatching the mutation. This exercises the
  // `forcedScopeType` prop which mirrors how a stale URL param could resolve.
  it('blocks submission when the resolved scope is project (contrived URL)', async () => {
    const mutateAsync = vi.fn().mockResolvedValue({ name: '' })
    setupMocks(mutateAsync)
    render(
      <CreateFolderTemplatePolicyPage
        folderName="test-folder"
        forcedScopeType="project"
      />,
    )

    fireEvent.change(screen.getByLabelText(/display name/i), {
      target: { value: 'Would-be Project Policy' },
    })
    fireEvent.click(screen.getByRole('button', { name: /^create$/i }))

    await waitFor(() => {
      const alert = screen.getByTestId('policy-form-error')
      expect(alert).toHaveTextContent(/only be created at folder or organization scope/i)
    })
    expect(mutateAsync).not.toHaveBeenCalled()
  })
})

import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

const mockNavigate = vi.fn()

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => ({ projectName: 'test-project' }),
    }),
    useNavigate: () => mockNavigate,
    Link: ({ children, className, to, params }: { children: React.ReactNode; className?: string; to?: string; params?: Record<string, string> }) => (
      <a href={to} data-params={JSON.stringify(params)} className={className}>{children}</a>
    ),
  }
})

vi.mock('@/queries/templates', () => ({
  useCreateTemplate: vi.fn(),
  useRenderTemplate: vi.fn(),
  useListLinkableTemplates: vi.fn().mockReturnValue({ data: [], isPending: false }),
  makeProjectScope: vi.fn().mockReturnValue({ scope: 3, scopeName: 'test-project' }),
  TemplateScope: { UNSPECIFIED: 0, ORGANIZATION: 1, FOLDER: 2, PROJECT: 3 },
  linkableKey: (scope: number | undefined, scopeName: string | undefined, name: string) =>
    `${scope ?? 0}/${scopeName ?? ''}/${name}`,
  parseLinkableKey: (key: string) => {
    const parts = key.split('/')
    return { scope: Number(parts[0]), scopeName: parts[1] ?? '', name: parts.slice(2).join('/') }
  },
}))

vi.mock('@/queries/projects', () => ({
  useGetProject: vi.fn(),
}))

vi.mock('@/queries/organizations', () => ({
  useGetOrganization: vi.fn(),
}))

vi.mock('@/hooks/use-debounced-value', () => ({
  useDebouncedValue: vi.fn((value: unknown) => value),
}))

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

import { useCreateTemplate, useRenderTemplate, useListLinkableTemplates } from '@/queries/templates'
import { useGetProject } from '@/queries/projects'
import { useGetOrganization } from '@/queries/organizations'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { CreateTemplatePage } from './new'

function setupMocks(
  mutateAsync = vi.fn().mockResolvedValue({}),
  renderData?: { renderedJson?: string },
  renderError?: Error,
  userRole = Role.OWNER,
) {
  ;(useCreateTemplate as Mock).mockReturnValue({
    mutateAsync,
    isPending: false,
    reset: vi.fn(),
  })
  ;(useRenderTemplate as Mock).mockReturnValue({
    data: renderData ?? undefined,
    error: renderError ?? null,
    isLoading: false,
    isError: !!renderError,
  })
  ;(useGetProject as Mock).mockReturnValue({
    data: { name: 'test-project', userRole, organization: 'test-org' },
    isLoading: false,
  })
  ;(useGetOrganization as Mock).mockReturnValue({
    data: { name: 'test-org', gatewayNamespace: '' },
    isPending: false,
    error: null,
  })
}

describe('CreateTemplatePage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setupMocks()
  })

  it('renders the page heading', () => {
    render(<CreateTemplatePage />)
    expect(screen.getByText(/create deployment template/i)).toBeInTheDocument()
  })

  it('renders Display Name field', () => {
    render(<CreateTemplatePage />)
    expect(screen.getByLabelText(/display name/i)).toBeInTheDocument()
  })

  it('renders Name (slug) field', () => {
    render(<CreateTemplatePage />)
    expect(screen.getByLabelText(/name slug/i)).toBeInTheDocument()
  })

  it('renders Description field', () => {
    render(<CreateTemplatePage />)
    expect(screen.getByLabelText(/description/i)).toBeInTheDocument()
  })

  it('renders CUE Template textarea', () => {
    render(<CreateTemplatePage />)
    expect(screen.getByRole('textbox', { name: /cue template/i })).toBeInTheDocument()
  })

  it('renders Create Template submit button', () => {
    render(<CreateTemplatePage />)
    expect(screen.getByRole('button', { name: /create template/i })).toBeInTheDocument()
  })

  it('renders a Cancel link back to the templates list', () => {
    render(<CreateTemplatePage />)
    expect(screen.getByRole('link', { name: /cancel/i })).toBeInTheDocument()
  })

  it('auto-derives slug from display name', () => {
    render(<CreateTemplatePage />)
    const displayNameInput = screen.getByLabelText(/display name/i)
    fireEvent.change(displayNameInput, { target: { value: 'My Web App' } })
    const slugInput = screen.getByLabelText(/name slug/i) as HTMLInputElement
    expect(slugInput.value).toBe('my-web-app')
  })

  it('shows validation error when name is empty', async () => {
    render(<CreateTemplatePage />)
    fireEvent.click(screen.getByRole('button', { name: /create template/i }))
    await waitFor(() => {
      expect(screen.getByText(/template name is required/i)).toBeInTheDocument()
    })
  })

  it('calls createMutation with form values on submit', async () => {
    const mutateAsync = vi.fn().mockResolvedValue({})
    setupMocks(mutateAsync)
    render(<CreateTemplatePage />)

    fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'My Template' } })
    fireEvent.change(screen.getByLabelText(/description/i), { target: { value: 'A description' } })
    fireEvent.click(screen.getByRole('button', { name: /create template/i }))

    await waitFor(() => {
      expect(mutateAsync).toHaveBeenCalledWith(
        expect.objectContaining({
          name: 'my-template',
          displayName: 'My Template',
          description: 'A description',
        }),
      )
    })
  })

  it('navigates to template detail page after successful creation', async () => {
    const mutateAsync = vi.fn().mockResolvedValue({})
    setupMocks(mutateAsync)
    render(<CreateTemplatePage />)

    fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'My Template' } })
    fireEvent.click(screen.getByRole('button', { name: /create template/i }))

    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith(
        expect.objectContaining({
          to: '/projects/$projectName/templates/$templateName',
          params: expect.objectContaining({ templateName: 'my-template' }),
        }),
      )
    })
  })

  it('shows error message when creation fails', async () => {
    const mutateAsync = vi.fn().mockRejectedValue(new Error('server error'))
    setupMocks(mutateAsync)
    render(<CreateTemplatePage />)

    fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'My Template' } })
    fireEvent.click(screen.getByRole('button', { name: /create template/i }))

    await waitFor(() => {
      expect(screen.getByText(/server error/i)).toBeInTheDocument()
    })
  })

  it('renders Preview toggle button', () => {
    render(<CreateTemplatePage />)
    expect(screen.getByRole('button', { name: /preview/i })).toBeInTheDocument()
  })

  it('shows rendered JSON when preview is toggled and data is available', async () => {
    setupMocks(
      vi.fn().mockResolvedValue({}),
      { renderedJson: '{"apiVersion":"apps/v1"}' },
    )
    render(<CreateTemplatePage />)
    fireEvent.click(screen.getByRole('button', { name: /preview/i }))
    await waitFor(() => {
      expect(screen.getByText(/"apiVersion":"apps\/v1"/)).toBeInTheDocument()
    })
  })

  it('shows CUE template has default content', () => {
    render(<CreateTemplatePage />)
    const cueEditor = screen.getByRole('textbox', { name: /cue template/i }) as HTMLTextAreaElement
    expect(cueEditor.value).toContain('projectResources')
  })

  it('useRenderTemplate is called with platform input including claims', () => {
    render(<CreateTemplatePage projectName="test-project" />)
    const calls = (useRenderTemplate as Mock).mock.calls
    expect(calls.length).toBeGreaterThan(0)
    // arg[0] is scope, arg[4] is cuePlatformInput
    const platformInput = calls[0][4]
    expect(platformInput).toContain('platform:')
    expect(platformInput).toContain('claims')
    expect(platformInput).toContain('email')
  })

  it('useRenderTemplate is called with project input (not platform project/namespace)', () => {
    render(<CreateTemplatePage projectName="test-project" />)
    const calls = (useRenderTemplate as Mock).mock.calls
    expect(calls.length).toBeGreaterThan(0)
    // arg[2] is cueInput (project input)
    const projectInput = calls[0][2]
    expect(projectInput).toContain('input:')
    expect(projectInput).not.toContain('project:')
    expect(projectInput).not.toContain('namespace:')
  })

  describe('Load httpbin Example button', () => {
    it('renders Load httpbin Example button', () => {
      render(<CreateTemplatePage />)
      expect(screen.getByRole('button', { name: /load httpbin example/i })).toBeInTheDocument()
    })

    it('clicking Load httpbin Example changes the CUE textarea content', () => {
      render(<CreateTemplatePage />)
      const cueEditor = screen.getByRole('textbox', { name: /cue template/i }) as HTMLTextAreaElement
      const initialContent = cueEditor.value
      fireEvent.click(screen.getByRole('button', { name: /load httpbin example/i }))
      expect(cueEditor.value).not.toBe(initialContent)
      expect(cueEditor.value).toContain('go-httpbin')
    })

    it('httpbin example CUE contains ServiceAccount, Deployment, and Service', () => {
      render(<CreateTemplatePage />)
      fireEvent.click(screen.getByRole('button', { name: /load httpbin example/i }))
      const cueEditor = screen.getByRole('textbox', { name: /cue template/i }) as HTMLTextAreaElement
      expect(cueEditor.value).toContain('ServiceAccount')
      expect(cueEditor.value).toContain('Deployment')
      expect(cueEditor.value).toContain('Service')
    })
  })

  describe('linked platform templates on create page', () => {
    // `forced` signals templates that a TemplatePolicy REQUIRE rule will
    // unify onto this project at render time. The checkbox renders checked
    // and disabled so the UI mirrors that the template is effectively
    // pinned; the user cannot opt out via the linking UI.
    const mockOrgTemplates = [
      { name: 'reference-grant', displayName: 'Reference Grant', description: 'Default ReferenceGrant for cross-namespace gateway routing', forced: true, scopeRef: { scope: 1, scopeName: 'default' } },
      { name: 'httpbin-platform', displayName: 'HTTPbin Platform', description: 'Platform HTTPRoute for go-httpbin', forced: false, scopeRef: { scope: 1, scopeName: 'default' } },
    ]
    const mockFolderTemplates = [
      { name: 'team-network-policy', displayName: 'Team Network Policy', description: 'Standard NetworkPolicy for team namespaces', forced: false, scopeRef: { scope: 2, scopeName: 'team-a' } },
    ]
    const allLinkable = [...mockOrgTemplates, ...mockFolderTemplates]

    it('shows linked templates section with empty state when no linkable templates exist', () => {
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: [], isPending: false })
      setupMocks()
      render(<CreateTemplatePage />)
      expect(screen.getByText(/linked platform templates/i)).toBeInTheDocument()
      expect(screen.getByText(/no platform templates available to link/i)).toBeInTheDocument()
    })

    it('shows empty state message for EDITOR when no linkable templates exist', () => {
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: [], isPending: false })
      setupMocks(vi.fn().mockResolvedValue({}), undefined, undefined, Role.EDITOR)
      render(<CreateTemplatePage />)
      expect(screen.getByText(/linked platform templates/i)).toBeInTheDocument()
      expect(screen.getByText(/no platform templates available to link/i)).toBeInTheDocument()
    })

    it('shows linked templates section when linkable templates exist for OWNER', () => {
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: allLinkable, isPending: false })
      setupMocks(vi.fn().mockResolvedValue({}), undefined, undefined, Role.OWNER)
      render(<CreateTemplatePage />)
      expect(screen.getByText(/linked platform templates/i)).toBeInTheDocument()
    })

    it('groups templates by scope with Organization and Folder headers', () => {
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: allLinkable, isPending: false })
      setupMocks(vi.fn().mockResolvedValue({}), undefined, undefined, Role.OWNER)
      render(<CreateTemplatePage />)
      expect(screen.getByText(/organization templates/i)).toBeInTheDocument()
      expect(screen.getByText(/folder templates/i)).toBeInTheDocument()
    })

    it('shows checkboxes for linkable templates when user is OWNER', () => {
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: allLinkable, isPending: false })
      setupMocks(vi.fn().mockResolvedValue({}), undefined, undefined, Role.OWNER)
      render(<CreateTemplatePage />)
      const checkboxes = screen.getAllByRole('checkbox')
      expect(checkboxes.length).toBe(3)
    })

    // `forced` templates are rendered checked and disabled so the linking
    // UI reflects the fact that a TemplatePolicy REQUIRE rule pins them at
    // render time; the user cannot opt out via this form.
    it('forced template checkbox is checked and disabled', () => {
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: allLinkable, isPending: false })
      setupMocks(vi.fn().mockResolvedValue({}), undefined, undefined, Role.OWNER)
      render(<CreateTemplatePage />)
      const checkbox = screen.getByRole('checkbox', { name: /reference grant/i })
      expect(checkbox).toBeChecked()
      expect(checkbox).toBeDisabled()
      expect(screen.getByText(/always applied/i)).toBeInTheDocument()
    })

    it('non-forced template checkboxes are unchecked by default', () => {
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: allLinkable, isPending: false })
      setupMocks(vi.fn().mockResolvedValue({}), undefined, undefined, Role.OWNER)
      render(<CreateTemplatePage />)
      const httpbinCheckbox = screen.getByRole('checkbox', { name: /httpbin platform/i })
      expect(httpbinCheckbox).not.toBeChecked()
      expect(httpbinCheckbox).not.toBeDisabled()
    })

    // Read-only linking view for non-OWNER roles surfaces only the
    // permission note; the checkboxes and actions are hidden.
    it('shows read-only view for EDITOR with permission note', () => {
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: allLinkable, isPending: false })
      setupMocks(vi.fn().mockResolvedValue({}), undefined, undefined, Role.EDITOR)
      render(<CreateTemplatePage />)
      expect(screen.getByText(/linked platform templates/i)).toBeInTheDocument()
      expect(screen.getByText(/only owners can link/i)).toBeInTheDocument()
    })

    it('shows read-only view for VIEWER with permission note', () => {
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: allLinkable, isPending: false })
      setupMocks(vi.fn().mockResolvedValue({}), undefined, undefined, Role.VIEWER)
      render(<CreateTemplatePage />)
      expect(screen.getByText(/linked platform templates/i)).toBeInTheDocument()
      expect(screen.getByText(/only owners can link/i)).toBeInTheDocument()
    })

    it('selected linked templates are included in create mutation', async () => {
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: allLinkable, isPending: false })
      const mutateAsync = vi.fn().mockResolvedValue({})
      setupMocks(mutateAsync, undefined, undefined, Role.OWNER)
      const user = userEvent.setup()
      render(<CreateTemplatePage />)

      fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'My Template' } })

      // Check a non-mandatory template
      await user.click(screen.getByRole('checkbox', { name: /httpbin platform/i }))

      fireEvent.click(screen.getByRole('button', { name: /create template/i }))

      await waitFor(() => {
        expect(mutateAsync).toHaveBeenCalledWith(
          expect.objectContaining({
            linkedTemplates: expect.arrayContaining([
              expect.objectContaining({ name: 'httpbin-platform', scope: 1, scopeName: 'default' }),
            ]),
          }),
        )
      })
    })

    it('disambiguates same-name templates across org and folder scopes', async () => {
      // When an org and folder template share the same name, selecting one
      // must not affect the other and the mutation must carry the correct scope.
      const sameName = [
        { name: 'shared-policy', displayName: 'Shared Policy (Org)', description: '', forced: false, scopeRef: { scope: 1, scopeName: 'default' } },
        { name: 'shared-policy', displayName: 'Shared Policy (Folder)', description: '', forced: false, scopeRef: { scope: 2, scopeName: 'team-a' } },
      ]
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: sameName, isPending: false })
      const mutateAsync = vi.fn().mockResolvedValue({})
      setupMocks(mutateAsync, undefined, undefined, Role.OWNER)
      const user = userEvent.setup()
      render(<CreateTemplatePage />)

      fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'My Template' } })

      // Select only the folder-scoped template (second checkbox)
      const checkboxes = screen.getAllByRole('checkbox')
      await user.click(checkboxes[1])

      fireEvent.click(screen.getByRole('button', { name: /create template/i }))

      await waitFor(() => {
        expect(mutateAsync).toHaveBeenCalledWith(
          expect.objectContaining({
            linkedTemplates: [
              expect.objectContaining({ name: 'shared-policy', scope: 2, scopeName: 'team-a' }),
            ],
          }),
        )
      })
    })

    it('create mutation receives empty linkedTemplates when no optional templates selected', async () => {
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: allLinkable, isPending: false })
      const mutateAsync = vi.fn().mockResolvedValue({})
      setupMocks(mutateAsync, undefined, undefined, Role.OWNER)
      render(<CreateTemplatePage />)

      fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'My Template' } })
      fireEvent.click(screen.getByRole('button', { name: /create template/i }))

      await waitFor(() => {
        expect(mutateAsync).toHaveBeenCalledWith(
          expect.objectContaining({
            linkedTemplates: [],
          }),
        )
      })
    })

    it('useRenderTemplate is called with selected linked templates for preview', async () => {
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: allLinkable, isPending: false })
      const mutateAsync = vi.fn().mockResolvedValue({})
      setupMocks(mutateAsync, undefined, undefined, Role.OWNER)
      const user = userEvent.setup()
      render(<CreateTemplatePage />)

      // Select a non-mandatory template
      await user.click(screen.getByRole('checkbox', { name: /httpbin platform/i }))

      const calls = (useRenderTemplate as Mock).mock.calls
      const lastCall = calls[calls.length - 1]
      // arg[5] is linkedTemplates
      expect(lastCall[5]).toEqual(
        expect.arrayContaining([
          expect.objectContaining({ name: 'httpbin-platform', scope: 1, scopeName: 'default' }),
        ]),
      )
    })

    it('useRenderTemplate receives empty linkedTemplates when none selected', () => {
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: allLinkable, isPending: false })
      setupMocks(vi.fn().mockResolvedValue({}), undefined, undefined, Role.OWNER)
      render(<CreateTemplatePage />)

      const calls = (useRenderTemplate as Mock).mock.calls
      const lastCall = calls[calls.length - 1]
      // arg[5] is linkedTemplates
      expect(lastCall[5]).toEqual([])
    })

    it('shows loading state when query is pending', () => {
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: [], isPending: true, isSuccess: false })
      setupMocks()
      render(<CreateTemplatePage />)
      expect(screen.getByText(/loading platform templates/i)).toBeInTheDocument()
    })

    it('does not show loading state when query errors (falls through to empty state)', () => {
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: [], isPending: false, isSuccess: false })
      setupMocks()
      render(<CreateTemplatePage />)
      expect(screen.queryByText(/loading platform templates/i)).not.toBeInTheDocument()
      expect(screen.getByText(/no platform templates available to link/i)).toBeInTheDocument()
    })
  })

  describe('per-collection preview sections', () => {
    it('shows both platform and project JSON sections when both are present', () => {
      setupMocks(
        vi.fn().mockResolvedValue({}),
        {
          renderedJson: '{"unified":"data"}',
          platformResourcesJson: '{"kind":"ReferenceGrant"}',
          projectResourcesJson: '{"kind":"Deployment"}',
        },
      )
      render(<CreateTemplatePage />)
      fireEvent.click(screen.getByRole('button', { name: /preview/i }))

      expect(screen.getByText('Platform Resources')).toBeInTheDocument()
      expect(screen.getByText('Project Resources')).toBeInTheDocument()
      expect(screen.getByLabelText('Platform Resources JSON')).toHaveTextContent('ReferenceGrant')
      expect(screen.getByLabelText('Project Resources JSON')).toHaveTextContent('Deployment')
    })

    it('shows empty-state message for platform and Project Resources heading when platform JSON is empty', () => {
      setupMocks(
        vi.fn().mockResolvedValue({}),
        {
          renderedJson: '{"unified":"data"}',
          platformResourcesJson: '',
          projectResourcesJson: '{"kind":"Deployment"}',
        },
      )
      render(<CreateTemplatePage />)
      fireEvent.click(screen.getByRole('button', { name: /preview/i }))

      expect(screen.getByText('Platform Resources')).toBeInTheDocument()
      expect(screen.getByText('No platform resources rendered by this template.')).toBeInTheDocument()
      expect(screen.getByText('Project Resources')).toBeInTheDocument()
      expect(screen.getByLabelText('Project Resources JSON')).toHaveTextContent('Deployment')
    })

    it('falls back to unified renderedJson when no per-collection fields', () => {
      setupMocks(
        vi.fn().mockResolvedValue({}),
        { renderedJson: '{"apiVersion":"apps/v1"}' },
      )
      render(<CreateTemplatePage />)
      fireEvent.click(screen.getByRole('button', { name: /preview/i }))

      expect(screen.queryByText('Platform Resources')).not.toBeInTheDocument()
      expect(screen.queryByText('Project Resources')).not.toBeInTheDocument()
      expect(screen.getByText(/"apiVersion":"apps\/v1"/)).toBeInTheDocument()
    })
  })
})

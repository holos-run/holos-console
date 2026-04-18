import { render, screen, fireEvent, waitFor, act, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => ({ projectName: 'test-project', templateName: 'web-app' }),
    }),
    useNavigate: () => vi.fn(),
  }
})

vi.mock('@/queries/templates', () => ({
  useGetTemplate: vi.fn(),
  useUpdateTemplate: vi.fn(),
  useDeleteTemplate: vi.fn(),
  useCloneTemplate: vi.fn(),
  useRenderTemplate: vi.fn(),
  useListLinkableTemplates: vi.fn().mockReturnValue({ data: [], isPending: false }),
  useCheckUpdates: vi.fn().mockReturnValue({ data: [], isPending: false, error: null }),
  useGetProjectTemplatePolicyState: vi.fn().mockReturnValue({ data: undefined, isPending: false, error: null }),
  makeProjectScope: vi.fn().mockReturnValue({ scope: 1, scopeName: 'test-project' }),
  TemplateScope: { UNSPECIFIED: 0, ORGANIZATION: 1, FOLDER: 2, PROJECT: 3 },
  linkableKey: (scope: number | undefined, scopeName: string | undefined, name: string) =>
    `${scope ?? 0}/${scopeName ?? ''}/${name}`,
  parseLinkableKey: (key: string) => {
    const parts = key.split('/')
    return { scope: Number(parts[0]), scopeName: parts[1] ?? '', name: parts.slice(2).join('/') }
  },
}))

vi.mock('@/components/template-updates', () => ({
  UpgradeDialog: () => null,
}))

vi.mock('@/queries/projects', () => ({
  useGetProject: vi.fn(),
}))

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

// Mock the debounce hook so tests don't have to manage timers by default.
// Individual tests that need to test stale behavior can override this mock.
vi.mock('@/hooks/use-debounced-value', () => ({
  useDebouncedValue: vi.fn((value: unknown) => value),
}))

import { useGetTemplate, useUpdateTemplate, useDeleteTemplate, useCloneTemplate, useRenderTemplate, useListLinkableTemplates, useCheckUpdates, useGetProjectTemplatePolicyState } from '@/queries/templates'
import { useGetProject } from '@/queries/projects'
import { useDebouncedValue } from '@/hooks/use-debounced-value'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { DeploymentTemplateDetailPage } from './$templateName'

const mockTemplate = {
  name: 'web-app',
  project: 'test-project',
  displayName: 'Web App',
  description: 'Standard web application',
  cueTemplate: '// cue template content',
  linkedTemplates: [] as Array<{ name: string; scope: number; scopeName: string }>,
}

function setupMocks(userRole = Role.OWNER, templateOverrides?: Partial<typeof mockTemplate>, renderYaml = '') {
  const template = { ...mockTemplate, ...templateOverrides }
  ;(useGetTemplate as Mock).mockReturnValue({ data: template, isPending: false, error: null })
  ;(useUpdateTemplate as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({}), isPending: false })
  ;(useDeleteTemplate as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({}), isPending: false, error: null, reset: vi.fn() })
  ;(useCloneTemplate as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({ name: 'new-template' }), isPending: false })
  ;(useGetProject as Mock).mockReturnValue({ data: { name: 'test-project', userRole }, isLoading: false })
  ;(useRenderTemplate as Mock).mockReturnValue({ data: { renderedYaml: renderYaml, renderedJson: '' }, error: null, isFetching: false })
}

describe('DeploymentTemplateDetailPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders template name', () => {
    setupMocks()
    render(<DeploymentTemplateDetailPage />)
    expect(screen.getByText('web-app')).toBeInTheDocument()
  })

  it('renders template description', () => {
    setupMocks()
    render(<DeploymentTemplateDetailPage />)
    expect(screen.getByText('Standard web application')).toBeInTheDocument()
  })

  it('renders CUE source editor with template content', () => {
    setupMocks()
    render(<DeploymentTemplateDetailPage />)
    const editor = screen.getByRole('textbox', { name: /cue template/i })
    expect(editor).toBeInTheDocument()
    expect((editor as HTMLTextAreaElement).value).toBe('// cue template content')
  })

  it('Save button is visible for owners', () => {
    setupMocks(Role.OWNER)
    render(<DeploymentTemplateDetailPage />)
    expect(screen.getByRole('button', { name: /save/i })).toBeInTheDocument()
  })

  it('Save button is visible for editors', () => {
    setupMocks(Role.EDITOR)
    render(<DeploymentTemplateDetailPage />)
    expect(screen.getByRole('button', { name: /save/i })).toBeInTheDocument()
  })

  it('Save button is not visible for viewers', () => {
    setupMocks(Role.VIEWER)
    render(<DeploymentTemplateDetailPage />)
    expect(screen.queryByRole('button', { name: /save/i })).not.toBeInTheDocument()
  })

  it('Delete button is visible for owners', () => {
    setupMocks(Role.OWNER)
    render(<DeploymentTemplateDetailPage />)
    expect(screen.getByRole('button', { name: /delete template/i })).toBeInTheDocument()
  })

  it('Delete button is not visible for editors', () => {
    setupMocks(Role.EDITOR)
    render(<DeploymentTemplateDetailPage />)
    expect(screen.queryByRole('button', { name: /delete template/i })).not.toBeInTheDocument()
  })

  it('Delete button is not visible for viewers', () => {
    setupMocks(Role.VIEWER)
    render(<DeploymentTemplateDetailPage />)
    expect(screen.queryByRole('button', { name: /delete template/i })).not.toBeInTheDocument()
  })

  it('Save calls useUpdateTemplate with changed CUE template', async () => {
    setupMocks(Role.OWNER)
    render(<DeploymentTemplateDetailPage />)
    const editor = screen.getByRole('textbox', { name: /cue template/i })
    fireEvent.change(editor, { target: { value: '// new cue content' } })
    fireEvent.click(screen.getByRole('button', { name: /save/i }))
    const mutateAsync = (useUpdateTemplate as Mock).mock.results[0].value.mutateAsync
    await waitFor(() => {
      expect(mutateAsync).toHaveBeenCalledWith(
        expect.objectContaining({ cueTemplate: '// new cue content' }),
      )
    })
  })

  it('clicking Delete opens confirmation dialog', () => {
    setupMocks(Role.OWNER)
    render(<DeploymentTemplateDetailPage />)
    fireEvent.click(screen.getByRole('button', { name: /delete template/i }))
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  it('confirming delete calls useDeleteTemplate', async () => {
    setupMocks(Role.OWNER)
    render(<DeploymentTemplateDetailPage />)
    fireEvent.click(screen.getByRole('button', { name: /delete template/i }))
    fireEvent.click(screen.getByRole('button', { name: /^delete$/i }))
    const mutateAsync = (useDeleteTemplate as Mock).mock.results[0].value.mutateAsync
    await waitFor(() => {
      expect(mutateAsync).toHaveBeenCalledWith({ name: 'web-app' })
    })
  })

  it('shows skeleton while loading', () => {
    ;(useGetTemplate as Mock).mockReturnValue({ data: undefined, isPending: true, error: null })
    ;(useUpdateTemplate as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false })
    ;(useDeleteTemplate as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false, error: null, reset: vi.fn() })
    ;(useGetProject as Mock).mockReturnValue({ data: undefined, isLoading: true })
    ;(useRenderTemplate as Mock).mockReturnValue({ data: { renderedYaml: '', renderedJson: '' }, error: null, isFetching: false })
    render(<DeploymentTemplateDetailPage />)
    const skeletons = document.querySelectorAll('[data-slot="skeleton"]')
    expect(skeletons.length).toBeGreaterThan(0)
  })

  it('shows error alert when fetch fails', () => {
    ;(useGetTemplate as Mock).mockReturnValue({ data: undefined, isPending: false, error: new Error('not found') })
    ;(useUpdateTemplate as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false })
    ;(useDeleteTemplate as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false, error: null, reset: vi.fn() })
    ;(useGetProject as Mock).mockReturnValue({ data: { name: 'test-project', userRole: Role.OWNER }, isLoading: false })
    ;(useRenderTemplate as Mock).mockReturnValue({ data: { renderedYaml: '', renderedJson: '' }, error: null, isFetching: false })
    render(<DeploymentTemplateDetailPage />)
    expect(screen.getByText('not found')).toBeInTheDocument()
  })

  it('Preview tab trigger is present in the template editor', () => {
    setupMocks(Role.OWNER, undefined, 'apiVersion: v1\nkind: ServiceAccount\n')
    render(<DeploymentTemplateDetailPage />)
    expect(screen.getByRole('tab', { name: /preview/i })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: /editor/i })).toBeInTheDocument()
  })

  it('Preview tab renders YAML on success', async () => {
    setupMocks(Role.OWNER, undefined, 'apiVersion: v1\nkind: ServiceAccount\n')
    const user = userEvent.setup()
    render(<DeploymentTemplateDetailPage />)
    await user.click(screen.getByRole('tab', { name: /preview/i }))
    const pre = screen.getByLabelText('Rendered YAML')
    expect(pre).toBeInTheDocument()
    expect(pre.textContent).toContain('ServiceAccount')
  })

  it('Preview tab shows error when render fails', async () => {
    setupMocks()
    ;(useRenderTemplate as Mock).mockReturnValue({ data: undefined, error: new Error('CUE syntax error'), isFetching: false })
    const user = userEvent.setup()
    render(<DeploymentTemplateDetailPage />)
    await user.click(screen.getByRole('tab', { name: /preview/i }))
    const errEl = screen.getByLabelText('Preview error')
    expect(errEl).toBeInTheDocument()
    expect(errEl.textContent).toContain('CUE syntax error')
  })

  it('CUE template textarea has fixed height and overflow classes', () => {
    setupMocks()
    render(<DeploymentTemplateDetailPage />)
    const editor = screen.getByRole('textbox', { name: /cue template/i })
    expect(editor.className).toContain('field-sizing-normal')
    expect(editor.className).toContain('max-h-96')
    expect(editor.className).toContain('overflow-y-auto')
  })

  it('useRenderTemplate is called with enabled: false when editor tab is active', () => {
    setupMocks()
    render(<DeploymentTemplateDetailPage />)
    // editor tab is active by default — enabled arg is false
    expect(useRenderTemplate as Mock).toHaveBeenCalledWith(
      expect.anything(), // scope
      expect.any(String),
      expect.any(String),
      false,
      expect.any(String),
      expect.any(Array), // linkedTemplates
    )
  })

  it('useRenderTemplate is called with enabled: true when preview tab is active', async () => {
    setupMocks(Role.OWNER, undefined, 'apiVersion: v1\n')
    const user = userEvent.setup()
    render(<DeploymentTemplateDetailPage />)
    await user.click(screen.getByRole('tab', { name: /preview/i }))
    expect(useRenderTemplate as Mock).toHaveBeenCalledWith(
      expect.anything(), // scope
      expect.any(String),
      expect.any(String),
      true,
      expect.any(String),
      expect.any(Array), // linkedTemplates
    )
  })

  it('Platform Input textarea is rendered in the preview tab', async () => {
    setupMocks(Role.OWNER, undefined, 'apiVersion: v1\n')
    const user = userEvent.setup()
    render(<DeploymentTemplateDetailPage />)
    await user.click(screen.getByRole('tab', { name: /preview/i }))
    expect(screen.getByRole('textbox', { name: /platform input/i })).toBeInTheDocument()
  })

  it('Project Input textarea is rendered in the preview tab', async () => {
    setupMocks(Role.OWNER, undefined, 'apiVersion: v1\n')
    const user = userEvent.setup()
    render(<DeploymentTemplateDetailPage />)
    await user.click(screen.getByRole('tab', { name: /preview/i }))
    expect(screen.getByRole('textbox', { name: /project input/i })).toBeInTheDocument()
  })

  it('Platform Input textarea contains project, namespace, and claims with email', async () => {
    setupMocks(Role.OWNER, undefined, 'apiVersion: v1\n')
    const user = userEvent.setup()
    render(<DeploymentTemplateDetailPage projectName="test-project" templateName="web-app" />)
    await user.click(screen.getByRole('tab', { name: /preview/i }))
    const platformInput = screen.getByRole('textbox', { name: /platform input/i }) as HTMLTextAreaElement
    expect(platformInput.value).toContain('test-project')
    expect(platformInput.value).toContain('claims')
    expect(platformInput.value).toContain('email')
  })

  it('Project Input textarea contains name, image, tag, and port', async () => {
    setupMocks(Role.OWNER, undefined, 'apiVersion: v1\n')
    const user = userEvent.setup()
    render(<DeploymentTemplateDetailPage projectName="test-project" templateName="web-app" />)
    await user.click(screen.getByRole('tab', { name: /preview/i }))
    const projectInput = screen.getByRole('textbox', { name: /project input/i }) as HTMLTextAreaElement
    expect(projectInput.value).toContain('name')
    expect(projectInput.value).toContain('image')
    expect(projectInput.value).toContain('tag')
    expect(projectInput.value).toContain('port')
  })

  it('useRenderTemplate receives separate platform and project inputs', async () => {
    setupMocks(Role.OWNER, undefined, 'apiVersion: v1\n')
    const user = userEvent.setup()
    render(<DeploymentTemplateDetailPage />)
    await user.click(screen.getByRole('tab', { name: /preview/i }))
    // scope is arg[0], cueTemplate is arg[1], cueInput is arg[2], enabled is arg[3], cuePlatformInput is arg[4]
    const calls = (useRenderTemplate as Mock).mock.calls
    const lastCall = calls[calls.length - 1]
    expect(lastCall[2]).toContain('input:')
    expect(lastCall[4]).toContain('platform:')
  })

  it('modifying Project Input calls useRenderTemplate with updated value', async () => {
    setupMocks(Role.OWNER, undefined, 'apiVersion: v1\n')
    const user = userEvent.setup()
    render(<DeploymentTemplateDetailPage />)
    await user.click(screen.getByRole('tab', { name: /preview/i }))
    const inputEditor = screen.getByRole('textbox', { name: /project input/i })
    fireEvent.change(inputEditor, { target: { value: 'input: { name: "custom" }' } })
    // With the identity mock for useDebouncedValue, debounced value equals raw value immediately
    expect(useRenderTemplate as Mock).toHaveBeenCalledWith(
      expect.anything(), // scope
      expect.any(String),
      'input: { name: "custom" }',
      true,
      expect.any(String),
      expect.any(Array), // linkedTemplates
    )
  })

  describe('edit description dialog', () => {
    it('shows edit description button for owner', () => {
      setupMocks(Role.OWNER)
      render(<DeploymentTemplateDetailPage />)
      expect(screen.getByRole('button', { name: /edit description/i })).toBeInTheDocument()
    })

    it('shows edit description button for editor', () => {
      setupMocks(Role.EDITOR)
      render(<DeploymentTemplateDetailPage />)
      expect(screen.getByRole('button', { name: /edit description/i })).toBeInTheDocument()
    })

    it('does not show edit description button for viewer', () => {
      setupMocks(Role.VIEWER)
      render(<DeploymentTemplateDetailPage />)
      expect(screen.queryByRole('button', { name: /edit description/i })).not.toBeInTheDocument()
    })

    it('opens edit description dialog on pencil click', async () => {
      setupMocks(Role.OWNER)
      const user = userEvent.setup()
      render(<DeploymentTemplateDetailPage />)
      await user.click(screen.getByRole('button', { name: /edit description/i }))
      expect(screen.getByRole('dialog')).toBeInTheDocument()
      expect(screen.getByRole('textbox', { name: /description/i })).toBeInTheDocument()
    })

    it('pre-fills dialog textarea with current description', async () => {
      setupMocks(Role.OWNER)
      const user = userEvent.setup()
      render(<DeploymentTemplateDetailPage />)
      await user.click(screen.getByRole('button', { name: /edit description/i }))
      const textarea = screen.getByRole('textbox', { name: /description/i }) as HTMLTextAreaElement
      expect(textarea.value).toBe('Standard web application')
    })

    it('save description calls updateMutation with new description', async () => {
      setupMocks(Role.OWNER)
      const user = userEvent.setup()
      render(<DeploymentTemplateDetailPage />)
      await user.click(screen.getByRole('button', { name: /edit description/i }))
      const textarea = screen.getByRole('textbox', { name: /description/i })
      await user.clear(textarea)
      await user.type(textarea, 'Updated description')
      await user.click(screen.getByRole('button', { name: /^save$/i }))
      const mutateAsync = (useUpdateTemplate as Mock).mock.results[0].value.mutateAsync
      await waitFor(() => {
        expect(mutateAsync).toHaveBeenCalledWith(
          expect.objectContaining({ description: 'Updated description' }),
        )
      })
    })

    it('cancel closes dialog without saving', async () => {
      setupMocks(Role.OWNER)
      const user = userEvent.setup()
      render(<DeploymentTemplateDetailPage />)
      await user.click(screen.getByRole('button', { name: /edit description/i }))
      await user.click(screen.getByRole('button', { name: /cancel/i }))
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
      const mutateAsync = (useUpdateTemplate as Mock).mock.results[0].value.mutateAsync
      expect(mutateAsync).not.toHaveBeenCalled()
    })

    it('renders description URLs as links', () => {
      setupMocks(Role.OWNER, { description: 'See https://example.com for details' })
      render(<DeploymentTemplateDetailPage />)
      const link = screen.getByRole('link', { name: /https:\/\/example\.com/ })
      expect(link).toBeInTheDocument()
      expect(link).toHaveAttribute('href', 'https://example.com')
    })

    it('shows No description when description is empty', () => {
      setupMocks(Role.OWNER, { description: '' })
      render(<DeploymentTemplateDetailPage />)
      expect(screen.getByText('No description')).toBeInTheDocument()
    })
  })

  describe('render status indicator', () => {
    it('shows fresh indicator when not stale, not rendering, and no error', async () => {
      setupMocks(Role.OWNER, undefined, 'apiVersion: v1\n')
      // useDebouncedValue returns the same value as input (identity) → not stale
      const user = userEvent.setup()
      render(<DeploymentTemplateDetailPage />)
      await user.click(screen.getByRole('tab', { name: /preview/i }))
      expect(screen.getByLabelText('Render status: fresh')).toBeInTheDocument()
    })

    it('shows rendering indicator when isFetching is true', async () => {
      setupMocks(Role.OWNER, undefined, 'apiVersion: v1\n')
      ;(useRenderTemplate as Mock).mockReturnValue({
        data: { renderedYaml: 'apiVersion: v1\n', renderedJson: '' },
        error: null,
        isFetching: true,
      })
      const user = userEvent.setup()
      render(<DeploymentTemplateDetailPage />)
      await user.click(screen.getByRole('tab', { name: /preview/i }))
      expect(screen.getByLabelText('Render status: rendering')).toBeInTheDocument()
    })

    it('shows error indicator when render fails', async () => {
      setupMocks(Role.OWNER)
      ;(useRenderTemplate as Mock).mockReturnValue({
        data: undefined,
        error: new Error('CUE syntax error'),
        isFetching: false,
      })
      const user = userEvent.setup()
      render(<DeploymentTemplateDetailPage />)
      await user.click(screen.getByRole('tab', { name: /preview/i }))
      expect(screen.getByLabelText('Render status: error')).toBeInTheDocument()
    })

    it('shows stale indicator when input changed but debounce timer is still running', async () => {
      setupMocks(Role.OWNER, undefined, 'apiVersion: v1\n')
      // Make useDebouncedValue return a value that differs from current state to simulate stale
      ;(useDebouncedValue as Mock).mockReturnValue('old-value')
      const user = userEvent.setup()
      render(<DeploymentTemplateDetailPage />)
      await user.click(screen.getByRole('tab', { name: /preview/i }))
      // Change the project input — raw state will differ from debounced value
      const inputEditor = screen.getByRole('textbox', { name: /project input/i })
      await act(async () => {
        fireEvent.change(inputEditor, { target: { value: 'new-value' } })
      })
      expect(screen.getByLabelText('Render status: stale')).toBeInTheDocument()
    })

    it('previously rendered YAML stays visible while stale (no blank flash)', async () => {
      setupMocks(Role.OWNER, undefined, 'apiVersion: v1\nkind: ServiceAccount\n')
      ;(useDebouncedValue as Mock).mockReturnValue('old-value')
      const user = userEvent.setup()
      render(<DeploymentTemplateDetailPage />)
      await user.click(screen.getByRole('tab', { name: /preview/i }))
      const inputEditor = screen.getByRole('textbox', { name: /project input/i })
      await act(async () => {
        fireEvent.change(inputEditor, { target: { value: 'new-value' } })
      })
      // Stale YAML should still be visible
      const pre = screen.getByLabelText('Rendered YAML')
      expect(pre.textContent).toContain('ServiceAccount')
    })
  })

  describe('clone button', () => {
    it('shows clone button for owner', () => {
      setupMocks(Role.OWNER)
      render(<DeploymentTemplateDetailPage />)
      expect(screen.getByRole('button', { name: /clone/i })).toBeInTheDocument()
    })

    it('shows clone button for editor', () => {
      setupMocks(Role.EDITOR)
      render(<DeploymentTemplateDetailPage />)
      expect(screen.getByRole('button', { name: /clone/i })).toBeInTheDocument()
    })

    it('shows clone button for viewer', () => {
      setupMocks(Role.VIEWER)
      render(<DeploymentTemplateDetailPage />)
      expect(screen.getByRole('button', { name: /clone/i })).toBeInTheDocument()
    })

    it('clicking clone opens dialog', async () => {
      setupMocks(Role.OWNER)
      const user = userEvent.setup()
      render(<DeploymentTemplateDetailPage />)
      await user.click(screen.getByRole('button', { name: /clone/i }))
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })

    it('clone dialog has name and display name fields', async () => {
      setupMocks(Role.OWNER)
      const user = userEvent.setup()
      render(<DeploymentTemplateDetailPage />)
      await user.click(screen.getByRole('button', { name: /clone/i }))
      expect(screen.getByRole('textbox', { name: /^name$/i })).toBeInTheDocument()
      expect(screen.getByRole('textbox', { name: /display name/i })).toBeInTheDocument()
    })

    it('confirming clone calls cloneTemplate', async () => {
      setupMocks(Role.OWNER)
      const user = userEvent.setup()
      render(<DeploymentTemplateDetailPage />)
      await user.click(screen.getByRole('button', { name: /clone/i }))
      const nameInput = screen.getByRole('textbox', { name: /^name$/i })
      await user.clear(nameInput)
      await user.type(nameInput, 'web-app-copy')
      const displayNameInput = screen.getByRole('textbox', { name: /display name/i })
      await user.clear(displayNameInput)
      await user.type(displayNameInput, 'Web App Copy')
      await user.click(screen.getByRole('button', { name: /^clone$/i }))
      const mutateAsync = (useCloneTemplate as Mock).mock.results[0].value.mutateAsync
      await waitFor(() => {
        expect(mutateAsync).toHaveBeenCalledWith(expect.objectContaining({
          sourceName: 'web-app',
          name: 'web-app-copy',
          displayName: 'Web App Copy',
        }))
      })
    })

    it('cancel closes clone dialog without saving', async () => {
      setupMocks(Role.OWNER)
      const user = userEvent.setup()
      render(<DeploymentTemplateDetailPage />)
      await user.click(screen.getByRole('button', { name: /clone/i }))
      await user.click(screen.getByRole('button', { name: /cancel/i }))
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
      const mutateAsync = (useCloneTemplate as Mock).mock.results[0].value.mutateAsync
      expect(mutateAsync).not.toHaveBeenCalled()
    })
  })

  describe('linked platform templates', () => {
    // HOL-555: LinkableTemplate.mandatory was removed in favor of `forced`,
    // which the linking UI uses to render "always applied" templates as
    // checked + disabled until HOL-557 migrates the backend auto-inclusion
    // to TemplatePolicy REQUIRE evaluation.
    const mockLinkable = [
      { name: 'reference-grant', displayName: 'Reference Grant', description: 'Adds a ReferenceGrant', forced: true, scopeRef: { scope: 1, scopeName: 'acme' } },
      { name: 'httproute', displayName: 'HTTPRoute Gateway', description: 'Adds an HTTPRoute', forced: false, scopeRef: { scope: 1, scopeName: 'acme' } },
      { name: 'team-network-policy', displayName: 'Team Network Policy', description: 'Adds network policy', forced: false, scopeRef: { scope: 2, scopeName: 'platform' } },
    ]

    it('shows linked templates section with empty state when no linkable templates exist', () => {
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: [], isPending: false })
      setupMocks()
      render(<DeploymentTemplateDetailPage />)
      expect(screen.getByText(/linked platform templates/i)).toBeInTheDocument()
      expect(screen.getByText(/none linked/i)).toBeInTheDocument()
      expect(screen.getByText(/no platform templates available to link/i)).toBeInTheDocument()
    })

    it('shows linked templates row when linkable templates exist', () => {
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: mockLinkable, isPending: false })
      setupMocks()
      render(<DeploymentTemplateDetailPage />)
      expect(screen.getByText(/linked platform templates/i)).toBeInTheDocument()
    })

    it('shows None linked when no templates are linked or forced', () => {
      const noForced = mockLinkable.map((t) => ({ ...t, forced: false }))
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: noForced, isPending: false })
      setupMocks(Role.OWNER, { ...mockTemplate, linkedTemplates: [] })
      render(<DeploymentTemplateDetailPage />)
      expect(screen.getAllByText(/None linked/i).length).toBeGreaterThan(0)
    })

    // HOL-555 -> HOL-557 transition: during this window the backend
    // resolver still auto-unifies ancestor templates carrying the
    // legacy mandatory annotation (surfaced via `forced=true`). The
    // detail page MUST reflect that by rendering those templates in the
    // read-only listing so the effective template set is accurate.
    it('shows forced ancestor template in read-only listing even when not explicitly linked', () => {
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: mockLinkable, isPending: false })
      setupMocks(Role.OWNER, { ...mockTemplate, linkedTemplates: [] })
      render(<DeploymentTemplateDetailPage />)
      // reference-grant has forced=true; its pill should be visible.
      expect(screen.getByText('Reference Grant')).toBeInTheDocument()
      // And None-linked should NOT be shown, because the forced one counts.
      expect(screen.queryByText(/None linked/i)).not.toBeInTheDocument()
    })

    it('renders Always applied badge on forced ancestor template pill', () => {
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: mockLinkable, isPending: false })
      setupMocks(Role.OWNER, { ...mockTemplate, linkedTemplates: [] })
      render(<DeploymentTemplateDetailPage />)
      // The read-only pill for the forced template is labeled
      // "Always applied" (matching the new/edit dialog treatment).
      expect(screen.getAllByText(/always applied/i).length).toBeGreaterThan(0)
    })

    it('does not render Always applied badge on non-forced linked templates', () => {
      const nonForced = mockLinkable.map((t) => ({ ...t, forced: false }))
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: nonForced, isPending: false })
      setupMocks(Role.OWNER, { ...mockTemplate, linkedTemplates: [{ name: 'httproute', scope: 1, scopeName: 'acme' }] })
      render(<DeploymentTemplateDetailPage />)
      // No forced templates, so the Always applied badge is not shown.
      expect(screen.queryByText(/always applied/i)).not.toBeInTheDocument()
    })

    it('shows linked template names as badges', () => {
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: mockLinkable, isPending: false })
      setupMocks(Role.OWNER, { ...mockTemplate, linkedTemplates: [{ name: 'httproute', scope: 1, scopeName: 'acme' }] })
      render(<DeploymentTemplateDetailPage />)
      expect(screen.getByText('HTTPRoute Gateway')).toBeInTheDocument()
    })

    it('shows edit linked templates button for owners', () => {
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: mockLinkable, isPending: false })
      setupMocks(Role.OWNER)
      render(<DeploymentTemplateDetailPage />)
      expect(screen.getByRole('button', { name: /edit linked platform templates/i })).toBeInTheDocument()
    })

    it('shows edit linked templates button for editors', () => {
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: mockLinkable, isPending: false })
      setupMocks(Role.EDITOR)
      render(<DeploymentTemplateDetailPage />)
      expect(screen.getByRole('button', { name: /edit linked platform templates/i })).toBeInTheDocument()
    })

    it('does not show edit linked templates button for viewers', () => {
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: mockLinkable, isPending: false })
      setupMocks(Role.VIEWER)
      render(<DeploymentTemplateDetailPage />)
      expect(screen.queryByRole('button', { name: /edit linked platform templates/i })).not.toBeInTheDocument()
    })

    it('clicking edit linked templates button opens dialog', async () => {
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: mockLinkable, isPending: false })
      setupMocks(Role.OWNER, { ...mockTemplate, linkedTemplates: [{ name: 'httproute', scope: 1, scopeName: 'acme' }] })
      const user = userEvent.setup()
      render(<DeploymentTemplateDetailPage />)
      await user.click(screen.getByRole('button', { name: /edit linked platform templates/i }))
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })

    it('dialog shows checkboxes for each linkable template', async () => {
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: mockLinkable, isPending: false })
      setupMocks(Role.OWNER, { ...mockTemplate, linkedTemplates: [] })
      const user = userEvent.setup()
      render(<DeploymentTemplateDetailPage />)
      await user.click(screen.getByRole('button', { name: /edit linked platform templates/i }))
      const checkboxes = screen.getAllByRole('checkbox')
      expect(checkboxes.length).toBeGreaterThanOrEqual(3)
    })

    // HOL-555: `forced` templates render checked and disabled so the UI
    // reflects the backend's annotation-driven auto-inclusion until HOL-557
    // migrates this behavior to TemplatePolicy REQUIRE.
    it('forced template checkbox in dialog is checked and disabled', async () => {
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: mockLinkable, isPending: false })
      setupMocks(Role.OWNER, { ...mockTemplate, linkedTemplates: [] })
      const user = userEvent.setup()
      render(<DeploymentTemplateDetailPage />)
      await user.click(screen.getByRole('button', { name: /edit linked platform templates/i }))
      const checkbox = screen.getByRole('checkbox', { name: /reference grant/i })
      expect(checkbox).toBeChecked()
      expect(checkbox).toBeDisabled()
    })

    it('saving calls updateMutation with selected linkedTemplates and updateLinkedTemplates: true', async () => {
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: mockLinkable, isPending: false })
      setupMocks(Role.OWNER, { ...mockTemplate, linkedTemplates: [] })
      const user = userEvent.setup()
      render(<DeploymentTemplateDetailPage />)
      await user.click(screen.getByRole('button', { name: /edit linked platform templates/i }))
      const dialog = screen.getByRole('dialog')
      // Check the non-mandatory template
      await user.click(within(dialog).getByRole('checkbox', { name: /httproute gateway/i }))
      await user.click(within(dialog).getByRole('button', { name: /^save$/i }))
      const mutateAsync = (useUpdateTemplate as Mock).mock.results[0].value.mutateAsync
      await waitFor(() => {
        expect(mutateAsync).toHaveBeenCalledWith(
          expect.objectContaining({
            linkedTemplates: expect.arrayContaining([
              expect.objectContaining({ scope: 1, scopeName: 'acme', name: 'httproute' }),
            ]),
            updateLinkedTemplates: true,
          }),
        )
      })
    })

    it('saving CUE template does NOT include updateLinkedTemplates: true', async () => {
      setupMocks(Role.OWNER)
      render(<DeploymentTemplateDetailPage />)
      const editor = screen.getByRole('textbox', { name: /cue template/i })
      fireEvent.change(editor, { target: { value: '// updated cue' } })
      fireEvent.click(screen.getByRole('button', { name: /save/i }))
      const mutateAsync = (useUpdateTemplate as Mock).mock.results[0].value.mutateAsync
      await waitFor(() => {
        expect(mutateAsync).toHaveBeenCalledWith(
          expect.not.objectContaining({ updateLinkedTemplates: true }),
        )
      })
    })

    it('dialog groups templates by scope with Organization and Folder headers', async () => {
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: mockLinkable, isPending: false })
      setupMocks(Role.OWNER, { ...mockTemplate, linkedTemplates: [] })
      const user = userEvent.setup()
      render(<DeploymentTemplateDetailPage />)
      await user.click(screen.getByRole('button', { name: /edit linked platform templates/i }))
      expect(screen.getByText('Organization Templates')).toBeInTheDocument()
      expect(screen.getByText('Folder Templates')).toBeInTheDocument()
    })

    it('OWNER can toggle non-forced checkboxes in dialog', async () => {
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: mockLinkable, isPending: false })
      setupMocks(Role.OWNER, { ...mockTemplate, linkedTemplates: [] })
      const user = userEvent.setup()
      render(<DeploymentTemplateDetailPage />)
      await user.click(screen.getByRole('button', { name: /edit linked platform templates/i }))
      const httpCheckbox = screen.getByRole('checkbox', { name: /httproute gateway/i })
      expect(httpCheckbox).not.toBeDisabled()
      const folderCheckbox = screen.getByRole('checkbox', { name: /team network policy/i })
      expect(folderCheckbox).not.toBeDisabled()
    })

    it('EDITOR sees all checkboxes disabled with permission message', async () => {
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: mockLinkable, isPending: false })
      setupMocks(Role.EDITOR, { ...mockTemplate, linkedTemplates: [] })
      const user = userEvent.setup()
      render(<DeploymentTemplateDetailPage />)
      await user.click(screen.getByRole('button', { name: /edit linked platform templates/i }))
      // All checkboxes should be disabled for EDITOR
      const checkboxes = screen.getAllByRole('checkbox')
      checkboxes.forEach((cb) => {
        expect(cb).toBeDisabled()
      })
      // Permission message should be visible
      expect(screen.getByText(/owner permission is required/i)).toBeInTheDocument()
    })

    it('EDITOR does not see Save button in linked templates dialog', async () => {
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: mockLinkable, isPending: false })
      setupMocks(Role.EDITOR, { ...mockTemplate, linkedTemplates: [] })
      const user = userEvent.setup()
      render(<DeploymentTemplateDetailPage />)
      await user.click(screen.getByRole('button', { name: /edit linked platform templates/i }))
      const dialog = screen.getByRole('dialog')
      expect(within(dialog).queryByRole('button', { name: /^save$/i })).not.toBeInTheDocument()
    })

    it('useRenderTemplate is called with template linked templates', () => {
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: mockLinkable, isPending: false })
      setupMocks(Role.OWNER, { ...mockTemplate, linkedTemplates: [
        { name: 'httproute', scope: 1, scopeName: 'acme' },
      ] })
      render(<DeploymentTemplateDetailPage />)
      const calls = (useRenderTemplate as Mock).mock.calls
      const lastCall = calls[calls.length - 1]
      // arg[5] is linkedTemplates
      expect(lastCall[5]).toEqual(
        expect.arrayContaining([
          expect.objectContaining({ name: 'httproute', scope: 1, scopeName: 'acme' }),
        ]),
      )
    })

    it('useRenderTemplate receives empty linkedTemplates when template has none', () => {
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: mockLinkable, isPending: false })
      setupMocks(Role.OWNER, { ...mockTemplate, linkedTemplates: [] })
      render(<DeploymentTemplateDetailPage />)
      const calls = (useRenderTemplate as Mock).mock.calls
      const lastCall = calls[calls.length - 1]
      // arg[5] is linkedTemplates
      expect(lastCall[5]).toEqual([])
    })

    it('shows scope badge per template in read-only display', () => {
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: mockLinkable, isPending: false })
      setupMocks(Role.OWNER, { ...mockTemplate, linkedTemplates: [
        { name: 'httproute', scope: 1, scopeName: 'acme' },
        { name: 'team-network-policy', scope: 2, scopeName: 'platform' },
      ] })
      render(<DeploymentTemplateDetailPage />)
      // Org badge for httproute and reference-grant (mandatory)
      const orgBadges = screen.getAllByText('Org')
      expect(orgBadges.length).toBeGreaterThanOrEqual(1)
      // Folder badge for team-network-policy
      const folderBadges = screen.getAllByText('Folder')
      expect(folderBadges.length).toBeGreaterThanOrEqual(1)
    })

    it('shows loading state when query is pending', () => {
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: [], isPending: true, isSuccess: false })
      setupMocks()
      render(<DeploymentTemplateDetailPage />)
      expect(screen.getByText('Loading...')).toBeInTheDocument()
    })

    it('does not show loading state when query errors (falls through to empty state)', () => {
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: [], isPending: false, isSuccess: false })
      setupMocks()
      render(<DeploymentTemplateDetailPage />)
      expect(screen.queryByText('Loading...')).not.toBeInTheDocument()
      expect(screen.getByText(/none linked/i)).toBeInTheDocument()
    })
  })

  describe('version status indicator', () => {
    const mockLinkable = [
      { name: 'reference-grant', displayName: 'Reference Grant', description: 'Adds a ReferenceGrant', forced: true, scopeRef: { scope: 1, scopeName: 'acme' }, releases: [{ version: '1.0.0' }, { version: '1.1.0' }] },
      { name: 'httproute', displayName: 'HTTPRoute Gateway', description: 'Adds an HTTPRoute', forced: false, scopeRef: { scope: 1, scopeName: 'acme' }, releases: [{ version: '2.0.0' }, { version: '2.1.0' }] },
      { name: 'no-releases', displayName: 'No Releases Template', description: 'No releases', forced: false, scopeRef: { scope: 2, scopeName: 'platform' }, releases: [] },
    ]

    it('shows green check icon when current version equals latest version', () => {
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: mockLinkable, isPending: false })
      ;(useCheckUpdates as Mock).mockReturnValue({
        data: [
          { ref: { scope: 1, scopeName: 'acme', name: 'httproute' }, currentVersion: '2.1.0', latestVersion: '2.1.0', latestCompatibleVersion: '2.1.0', breakingUpdateAvailable: false },
        ],
        isPending: false,
        error: null,
      })
      setupMocks(Role.OWNER, { ...mockTemplate, linkedTemplates: [{ name: 'httproute', scope: 1, scopeName: 'acme', versionConstraint: '^2.0.0' }] })
      render(<DeploymentTemplateDetailPage />)
      expect(screen.getByLabelText('Up to date')).toBeInTheDocument()
      expect(screen.getByText('v2.1.0')).toBeInTheDocument()
    })

    it('shows amber update icon when current version is behind latest', () => {
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: mockLinkable, isPending: false })
      ;(useCheckUpdates as Mock).mockReturnValue({
        data: [
          { ref: { scope: 1, scopeName: 'acme', name: 'httproute' }, currentVersion: '2.0.0', latestVersion: '2.1.0', latestCompatibleVersion: '2.1.0', breakingUpdateAvailable: false },
        ],
        isPending: false,
        error: null,
      })
      setupMocks(Role.OWNER, { ...mockTemplate, linkedTemplates: [{ name: 'httproute', scope: 1, scopeName: 'acme', versionConstraint: '^2.0.0' }] })
      render(<DeploymentTemplateDetailPage />)
      expect(screen.getByLabelText('Update available')).toBeInTheDocument()
      expect(screen.getByText('v2.0.0')).toBeInTheDocument()
    })

    it('shows unversioned indicator for templates with no releases', () => {
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: mockLinkable, isPending: false })
      ;(useCheckUpdates as Mock).mockReturnValue({
        data: [
          { ref: { scope: 2, scopeName: 'platform', name: 'no-releases' }, currentVersion: '', latestVersion: '', latestCompatibleVersion: '', breakingUpdateAvailable: false },
        ],
        isPending: false,
        error: null,
      })
      setupMocks(Role.OWNER, { ...mockTemplate, linkedTemplates: [{ name: 'no-releases', scope: 2, scopeName: 'platform' }] })
      render(<DeploymentTemplateDetailPage />)
      expect(screen.getByText('unversioned')).toBeInTheDocument()
    })

    it('passes includeCurrent: true to useCheckUpdates', () => {
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: mockLinkable, isPending: false })
      setupMocks(Role.OWNER, { ...mockTemplate, linkedTemplates: [{ name: 'httproute', scope: 1, scopeName: 'acme' }] })
      render(<DeploymentTemplateDetailPage />)
      // useCheckUpdates should be called with scope, templateName, and options including includeCurrent
      expect(useCheckUpdates as Mock).toHaveBeenCalledWith(
        expect.anything(), // scope
        expect.any(String), // templateName
        expect.objectContaining({ includeCurrent: true }),
      )
    })
  })

  // HOL-559: the project-template detail page renders the shared
  // PolicySection with a Reconcile action gated on write permission. The
  // Reconcile mutation preserves every existing field (displayName,
  // description, cueTemplate, enabled, linkedTemplates) so the backend
  // re-renders against the current TemplatePolicy chain without changing
  // functional behavior.
  describe('policy drift section', () => {
    function setupDriftState(drift: boolean) {
      ;(useGetProjectTemplatePolicyState as Mock).mockReturnValue({
        data: {
          $typeName: 'holos.console.v1.PolicyState',
          appliedSet: [{ $typeName: 'holos.console.v1.LinkedTemplateRef', scope: 1, scopeName: 'acme', name: 'base', versionConstraint: '' }],
          currentSet: [{ $typeName: 'holos.console.v1.LinkedTemplateRef', scope: 1, scopeName: 'acme', name: 'base', versionConstraint: '' }],
          addedRefs: drift ? [{ $typeName: 'holos.console.v1.LinkedTemplateRef', scope: 1, scopeName: 'acme', name: 'sidecar', versionConstraint: '' }] : [],
          removedRefs: [],
          drift,
          hasAppliedState: true,
        },
        isPending: false,
        error: null,
      })
    }

    it('renders the Policy Drift badge when drift is true', () => {
      setupMocks(Role.OWNER)
      setupDriftState(true)
      render(<DeploymentTemplateDetailPage />)
      expect(screen.getByTestId('policy-drift-badge')).toBeInTheDocument()
    })

    it('does not render the Policy Drift badge when drift is false', () => {
      setupMocks(Role.OWNER)
      setupDriftState(false)
      render(<DeploymentTemplateDetailPage />)
      expect(screen.queryByTestId('policy-drift-badge')).not.toBeInTheDocument()
      expect(screen.getByTestId('policy-in-sync')).toBeInTheDocument()
    })

    it('renders the Reconcile button for owners when drift is true', () => {
      setupMocks(Role.OWNER)
      setupDriftState(true)
      render(<DeploymentTemplateDetailPage />)
      expect(screen.getByRole('button', { name: /reconcile policy drift/i })).toBeInTheDocument()
    })

    it('renders the Reconcile button for editors when drift is true', () => {
      setupMocks(Role.EDITOR)
      setupDriftState(true)
      render(<DeploymentTemplateDetailPage />)
      expect(screen.getByRole('button', { name: /reconcile policy drift/i })).toBeInTheDocument()
    })

    it('does not render the Reconcile button for viewers', () => {
      setupMocks(Role.VIEWER)
      setupDriftState(true)
      render(<DeploymentTemplateDetailPage />)
      expect(screen.getByTestId('policy-drift-badge')).toBeInTheDocument()
      expect(screen.queryByRole('button', { name: /reconcile policy drift/i })).not.toBeInTheDocument()
    })

    it('clicking Reconcile calls useUpdateTemplate with preserved fields', async () => {
      setupMocks(Role.OWNER)
      setupDriftState(true)
      render(<DeploymentTemplateDetailPage />)
      fireEvent.click(screen.getByRole('button', { name: /reconcile policy drift/i }))
      const mutateAsync = (useUpdateTemplate as Mock).mock.results[0].value.mutateAsync
      await waitFor(() => {
        expect(mutateAsync).toHaveBeenCalledWith(
          expect.objectContaining({
            cueTemplate: expect.any(String),
            updateLinkedTemplates: true,
          }),
        )
      })
    })

    it('Reconcile success shows a success toast', async () => {
      setupMocks(Role.OWNER)
      setupDriftState(true)
      const { toast } = await import('sonner')
      render(<DeploymentTemplateDetailPage />)
      fireEvent.click(screen.getByRole('button', { name: /reconcile policy drift/i }))
      await waitFor(() => {
        expect(toast.success).toHaveBeenCalledWith('Reconcile requested')
      })
    })

    it('Reconcile failure shows an error toast', async () => {
      setupMocks(Role.OWNER)
      setupDriftState(true)
      ;(useUpdateTemplate as Mock).mockReturnValue({
        mutateAsync: vi.fn().mockRejectedValue(new Error('conflict')),
        isPending: false,
      })
      const { toast } = await import('sonner')
      render(<DeploymentTemplateDetailPage />)
      fireEvent.click(screen.getByRole('button', { name: /reconcile policy drift/i }))
      await waitFor(() => {
        expect(toast.error).toHaveBeenCalledWith('conflict')
      })
    })

    it('Reconcile button is disabled while update mutation is pending', () => {
      setupMocks(Role.OWNER)
      setupDriftState(true)
      ;(useUpdateTemplate as Mock).mockReturnValue({
        mutateAsync: vi.fn(),
        isPending: true,
      })
      render(<DeploymentTemplateDetailPage />)
      const btn = screen.getByRole('button', { name: /reconcile policy drift/i })
      expect(btn).toBeDisabled()
    })
  })
})

import { render, screen, fireEvent, waitFor, act } from '@testing-library/react'
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

vi.mock('@/queries/deployment-templates', () => ({
  useGetDeploymentTemplate: vi.fn(),
  useUpdateDeploymentTemplate: vi.fn(),
  useDeleteDeploymentTemplate: vi.fn(),
  useCloneDeploymentTemplate: vi.fn(),
  useRenderDeploymentTemplate: vi.fn(),
  useListLinkableOrgTemplates: vi.fn().mockReturnValue({ data: [] }),
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

import { useGetDeploymentTemplate, useUpdateDeploymentTemplate, useDeleteDeploymentTemplate, useCloneDeploymentTemplate, useRenderDeploymentTemplate, useListLinkableOrgTemplates } from '@/queries/deployment-templates'
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
}

function setupMocks(userRole = Role.OWNER, templateOverrides?: Partial<typeof mockTemplate>, renderYaml = '') {
  const template = { ...mockTemplate, ...templateOverrides }
  ;(useGetDeploymentTemplate as Mock).mockReturnValue({ data: template, isPending: false, error: null })
  ;(useUpdateDeploymentTemplate as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({}), isPending: false })
  ;(useDeleteDeploymentTemplate as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({}), isPending: false, error: null, reset: vi.fn() })
  ;(useCloneDeploymentTemplate as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({ name: 'new-template' }), isPending: false })
  ;(useGetProject as Mock).mockReturnValue({ data: { name: 'test-project', userRole }, isLoading: false })
  ;(useRenderDeploymentTemplate as Mock).mockReturnValue({ data: { renderedYaml: renderYaml, renderedJson: '' }, error: null, isFetching: false })
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

  it('Save calls useUpdateDeploymentTemplate with changed CUE template', async () => {
    setupMocks(Role.OWNER)
    render(<DeploymentTemplateDetailPage />)
    const editor = screen.getByRole('textbox', { name: /cue template/i })
    fireEvent.change(editor, { target: { value: '// new cue content' } })
    fireEvent.click(screen.getByRole('button', { name: /save/i }))
    const mutateAsync = (useUpdateDeploymentTemplate as Mock).mock.results[0].value.mutateAsync
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

  it('confirming delete calls useDeleteDeploymentTemplate', async () => {
    setupMocks(Role.OWNER)
    render(<DeploymentTemplateDetailPage />)
    fireEvent.click(screen.getByRole('button', { name: /delete template/i }))
    fireEvent.click(screen.getByRole('button', { name: /^delete$/i }))
    const mutateAsync = (useDeleteDeploymentTemplate as Mock).mock.results[0].value.mutateAsync
    await waitFor(() => {
      expect(mutateAsync).toHaveBeenCalledWith({ name: 'web-app' })
    })
  })

  it('shows skeleton while loading', () => {
    ;(useGetDeploymentTemplate as Mock).mockReturnValue({ data: undefined, isPending: true, error: null })
    ;(useUpdateDeploymentTemplate as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false })
    ;(useDeleteDeploymentTemplate as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false, error: null, reset: vi.fn() })
    ;(useGetProject as Mock).mockReturnValue({ data: undefined, isLoading: true })
    ;(useRenderDeploymentTemplate as Mock).mockReturnValue({ data: { renderedYaml: '', renderedJson: '' }, error: null, isFetching: false })
    render(<DeploymentTemplateDetailPage />)
    const skeletons = document.querySelectorAll('[data-slot="skeleton"]')
    expect(skeletons.length).toBeGreaterThan(0)
  })

  it('shows error alert when fetch fails', () => {
    ;(useGetDeploymentTemplate as Mock).mockReturnValue({ data: undefined, isPending: false, error: new Error('not found') })
    ;(useUpdateDeploymentTemplate as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false })
    ;(useDeleteDeploymentTemplate as Mock).mockReturnValue({ mutateAsync: vi.fn(), isPending: false, error: null, reset: vi.fn() })
    ;(useGetProject as Mock).mockReturnValue({ data: { name: 'test-project', userRole: Role.OWNER }, isLoading: false })
    ;(useRenderDeploymentTemplate as Mock).mockReturnValue({ data: { renderedYaml: '', renderedJson: '' }, error: null, isFetching: false })
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
    ;(useRenderDeploymentTemplate as Mock).mockReturnValue({ data: undefined, error: new Error('CUE syntax error'), isFetching: false })
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

  it('useRenderDeploymentTemplate is called with enabled: false when editor tab is active', () => {
    setupMocks()
    render(<DeploymentTemplateDetailPage />)
    // editor tab is active by default
    expect(useRenderDeploymentTemplate as Mock).toHaveBeenCalledWith(
      expect.any(String),
      expect.any(String),
      false,
      expect.any(String),
    )
  })

  it('useRenderDeploymentTemplate is called with enabled: true when preview tab is active', async () => {
    setupMocks(Role.OWNER, undefined, 'apiVersion: v1\n')
    const user = userEvent.setup()
    render(<DeploymentTemplateDetailPage />)
    await user.click(screen.getByRole('tab', { name: /preview/i }))
    expect(useRenderDeploymentTemplate as Mock).toHaveBeenCalledWith(
      expect.any(String),
      expect.any(String),
      true,
      expect.any(String),
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

  it('useRenderDeploymentTemplate receives separate platform and project inputs', async () => {
    setupMocks(Role.OWNER, undefined, 'apiVersion: v1\n')
    const user = userEvent.setup()
    render(<DeploymentTemplateDetailPage />)
    await user.click(screen.getByRole('tab', { name: /preview/i }))
    // The 4th arg is the platform input string
    const calls = (useRenderDeploymentTemplate as Mock).mock.calls
    const lastCall = calls[calls.length - 1]
    expect(lastCall[1]).toContain('input:')
    expect(lastCall[3]).toContain('platform:')
  })

  it('modifying Project Input calls useRenderDeploymentTemplate with updated value', async () => {
    setupMocks(Role.OWNER, undefined, 'apiVersion: v1\n')
    const user = userEvent.setup()
    render(<DeploymentTemplateDetailPage />)
    await user.click(screen.getByRole('tab', { name: /preview/i }))
    const inputEditor = screen.getByRole('textbox', { name: /project input/i })
    fireEvent.change(inputEditor, { target: { value: 'input: { name: "custom" }' } })
    // With the identity mock for useDebouncedValue, debounced value equals raw value immediately
    expect(useRenderDeploymentTemplate as Mock).toHaveBeenCalledWith(
      expect.any(String),
      'input: { name: "custom" }',
      true,
      expect.any(String),
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
      const mutateAsync = (useUpdateDeploymentTemplate as Mock).mock.results[0].value.mutateAsync
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
      const mutateAsync = (useUpdateDeploymentTemplate as Mock).mock.results[0].value.mutateAsync
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
      ;(useRenderDeploymentTemplate as Mock).mockReturnValue({
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
      ;(useRenderDeploymentTemplate as Mock).mockReturnValue({
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

    it('confirming clone calls cloneDeploymentTemplate', async () => {
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
      const mutateAsync = (useCloneDeploymentTemplate as Mock).mock.results[0].value.mutateAsync
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
      const mutateAsync = (useCloneDeploymentTemplate as Mock).mock.results[0].value.mutateAsync
      expect(mutateAsync).not.toHaveBeenCalled()
    })
  })

  describe('linked platform templates', () => {
    const mockLinkable = [
      { name: 'httproute', displayName: 'HTTPRoute Gateway', description: 'Adds an HTTPRoute', mandatory: false },
      { name: 'mandatory-labels', displayName: 'Mandatory Labels', description: 'Enforces labels', mandatory: true },
    ]

    it('does not show linked templates row when no linkable templates exist', () => {
      ;(useListLinkableOrgTemplates as Mock).mockReturnValue({ data: [] })
      setupMocks()
      render(<DeploymentTemplateDetailPage />)
      expect(screen.queryByText(/linked platform templates/i)).not.toBeInTheDocument()
    })

    it('shows linked templates row when linkable templates exist', () => {
      ;(useListLinkableOrgTemplates as Mock).mockReturnValue({ data: mockLinkable })
      setupMocks()
      render(<DeploymentTemplateDetailPage />)
      expect(screen.getByText(/linked platform templates/i)).toBeInTheDocument()
    })

    it('shows None linked when no templates are linked', () => {
      ;(useListLinkableOrgTemplates as Mock).mockReturnValue({ data: mockLinkable })
      // v1alpha2: linkedTemplates replaces linkedOrgTemplates (array of LinkedTemplateRef)
      setupMocks(Role.OWNER, { ...mockTemplate, linkedTemplates: [] })
      render(<DeploymentTemplateDetailPage />)
      // mandatory template is always shown even when linkedTemplates is empty
      expect(screen.getByText('Mandatory Labels')).toBeInTheDocument()
    })

    it('shows linked template names as badges', () => {
      ;(useListLinkableOrgTemplates as Mock).mockReturnValue({ data: mockLinkable })
      // v1alpha2: linkedTemplates replaces linkedOrgTemplates (array of LinkedTemplateRef with name field)
      setupMocks(Role.OWNER, { ...mockTemplate, linkedTemplates: [{ name: 'httproute', scope: 1, scopeName: 'acme' }] })
      render(<DeploymentTemplateDetailPage />)
      expect(screen.getByText('HTTPRoute Gateway')).toBeInTheDocument()
    })

    it('shows edit linked templates button for owners', () => {
      ;(useListLinkableOrgTemplates as Mock).mockReturnValue({ data: mockLinkable })
      setupMocks(Role.OWNER)
      render(<DeploymentTemplateDetailPage />)
      expect(screen.getByRole('button', { name: /edit linked platform templates/i })).toBeInTheDocument()
    })

    it('does not show edit linked templates button for viewers', () => {
      ;(useListLinkableOrgTemplates as Mock).mockReturnValue({ data: mockLinkable })
      setupMocks(Role.VIEWER)
      render(<DeploymentTemplateDetailPage />)
      expect(screen.queryByRole('button', { name: /edit linked platform templates/i })).not.toBeInTheDocument()
    })

    it('clicking edit linked templates button opens dialog', async () => {
      ;(useListLinkableOrgTemplates as Mock).mockReturnValue({ data: mockLinkable })
      setupMocks(Role.OWNER, { ...mockTemplate, linkedOrgTemplates: ['httproute'] })
      const user = userEvent.setup()
      render(<DeploymentTemplateDetailPage />)
      await user.click(screen.getByRole('button', { name: /edit linked platform templates/i }))
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })

    it('dialog shows checkboxes for each linkable template', async () => {
      ;(useListLinkableOrgTemplates as Mock).mockReturnValue({ data: mockLinkable })
      setupMocks(Role.OWNER, { ...mockTemplate, linkedOrgTemplates: ['httproute'] })
      const user = userEvent.setup()
      render(<DeploymentTemplateDetailPage />)
      await user.click(screen.getByRole('button', { name: /edit linked platform templates/i }))
      const checkboxes = screen.getAllByRole('checkbox')
      expect(checkboxes.length).toBeGreaterThanOrEqual(2)
    })

    it('mandatory template checkbox is checked and disabled in dialog', async () => {
      ;(useListLinkableOrgTemplates as Mock).mockReturnValue({ data: mockLinkable })
      setupMocks(Role.OWNER, { ...mockTemplate, linkedOrgTemplates: [] })
      const user = userEvent.setup()
      render(<DeploymentTemplateDetailPage />)
      await user.click(screen.getByRole('button', { name: /edit linked platform templates/i }))
      const mandatoryCheckbox = screen.getByRole('checkbox', { name: /mandatory labels/i })
      expect(mandatoryCheckbox).toBeChecked()
      expect(mandatoryCheckbox).toBeDisabled()
    })

    it('saving calls updateMutation with selected linkedOrgTemplates', async () => {
      ;(useListLinkableOrgTemplates as Mock).mockReturnValue({ data: mockLinkable })
      setupMocks(Role.OWNER, { ...mockTemplate, linkedOrgTemplates: [] })
      const user = userEvent.setup()
      render(<DeploymentTemplateDetailPage />)
      await user.click(screen.getByRole('button', { name: /edit linked platform templates/i }))
      // Check the non-mandatory template
      await user.click(screen.getByRole('checkbox', { name: /httproute gateway/i }))
      await user.click(screen.getByRole('button', { name: /^save$/i }))
      const mutateAsync = (useUpdateDeploymentTemplate as Mock).mock.results[0].value.mutateAsync
      await waitFor(() => {
        expect(mutateAsync).toHaveBeenCalledWith(
          expect.objectContaining({ linkedOrgTemplates: ['httproute'] }),
        )
      })
    })
  })
})

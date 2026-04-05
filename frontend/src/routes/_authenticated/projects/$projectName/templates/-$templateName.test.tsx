import { render, screen, fireEvent, waitFor } from '@testing-library/react'
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
  useRenderDeploymentTemplate: vi.fn(),
}))

vi.mock('@/queries/projects', () => ({
  useGetProject: vi.fn(),
}))

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

import { useGetDeploymentTemplate, useUpdateDeploymentTemplate, useDeleteDeploymentTemplate, useRenderDeploymentTemplate } from '@/queries/deployment-templates'
import { useGetProject } from '@/queries/projects'
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
})

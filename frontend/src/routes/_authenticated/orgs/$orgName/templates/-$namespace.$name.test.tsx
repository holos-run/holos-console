import { render, screen, waitFor, fireEvent } from '@testing-library/react'
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
      useParams: () => ({
        orgName: 'test-org',
        namespace: 'prj-billing',
        name: 'web',
      }),
    }),
    useNavigate: () => mockNavigate,
  }
})

vi.mock('@/queries/templates', () => ({
  useGetTemplate: vi.fn(),
  useUpdateTemplate: vi.fn(),
  useDeleteTemplate: vi.fn(),
  useListTemplateExamples: vi.fn(),
  useRenderTemplate: vi.fn().mockReturnValue({
    data: undefined,
    error: null,
    isFetching: false,
  }),
}))

vi.mock('@/hooks/use-debounced-value', () => ({
  useDebouncedValue: vi.fn((value: unknown) => value),
}))

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}))

import {
  useGetTemplate,
  useUpdateTemplate,
  useDeleteTemplate,
  useListTemplateExamples,
} from '@/queries/templates'
import { toast } from 'sonner'
import { ConsolidatedTemplateEditorPage } from './$namespace.$name'

const EXAMPLE_HTTPROUTE = {
  name: 'httproute-v1',
  displayName: 'HTTPRoute Ingress',
  description: 'Provides an HTTPRoute for the org-configured ingress gateway.',
  cueTemplate: '// example CUE\nplatformResources: {}\n',
}

const EXAMPLE_SECOND = {
  name: 'configmap-v1',
  displayName: 'ConfigMap Starter',
  description: 'A minimal ConfigMap scaffold.',
  cueTemplate: '// another example\nprojectResources: {}\n',
}

function setupMocks(
  template: {
    name: string
    namespace: string
    displayName: string
    description?: string
    cueTemplate: string
    enabled?: boolean
    linkedTemplates?: unknown[]
  } = {
    name: 'web',
    namespace: 'prj-billing',
    displayName: 'Web Service',
    description: '',
    cueTemplate: '// cue body',
    enabled: true,
    linkedTemplates: [],
  },
  examples: typeof EXAMPLE_HTTPROUTE[] = [EXAMPLE_HTTPROUTE, EXAMPLE_SECOND],
) {
  ;(useGetTemplate as Mock).mockReturnValue({
    data: template,
    isPending: false,
    error: null,
  })
  ;(useUpdateTemplate as Mock).mockReturnValue({
    mutateAsync: vi.fn().mockResolvedValue({}),
    isPending: false,
  })
  ;(useDeleteTemplate as Mock).mockReturnValue({
    mutateAsync: vi.fn().mockResolvedValue({}),
    isPending: false,
    error: null,
  })
  ;(useListTemplateExamples as Mock).mockReturnValue({
    data: examples,
    isPending: false,
    error: null,
  })
}

describe('ConsolidatedTemplateEditorPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders skeletons while loading', () => {
    ;(useGetTemplate as Mock).mockReturnValue({
      data: undefined,
      isPending: true,
      error: null,
    })
    ;(useUpdateTemplate as Mock).mockReturnValue({
      mutateAsync: vi.fn(),
      isPending: false,
    })
    ;(useDeleteTemplate as Mock).mockReturnValue({
      mutateAsync: vi.fn(),
      isPending: false,
      error: null,
    })
    ;(useListTemplateExamples as Mock).mockReturnValue({
      data: [],
      isPending: false,
      error: null,
    })
    const { container } = render(<ConsolidatedTemplateEditorPage />)
    // The skeletons render with data-slot="skeleton" (shadcn primitive).
    expect(container.querySelector('[data-slot="skeleton"]')).toBeInTheDocument()
  })

  it('renders the error alert when the fetch fails', () => {
    ;(useGetTemplate as Mock).mockReturnValue({
      data: undefined,
      isPending: false,
      error: new Error('template not found'),
    })
    ;(useUpdateTemplate as Mock).mockReturnValue({
      mutateAsync: vi.fn(),
      isPending: false,
    })
    ;(useDeleteTemplate as Mock).mockReturnValue({
      mutateAsync: vi.fn(),
      isPending: false,
      error: null,
    })
    ;(useListTemplateExamples as Mock).mockReturnValue({
      data: [],
      isPending: false,
      error: null,
    })
    render(<ConsolidatedTemplateEditorPage />)
    expect(screen.getByText('template not found')).toBeInTheDocument()
  })

  it('loads a template by namespace+name and renders the display name', () => {
    setupMocks()
    render(<ConsolidatedTemplateEditorPage />)

    expect(useGetTemplate).toHaveBeenCalledWith('prj-billing', 'web')
    expect(screen.getByText('Web Service')).toBeInTheDocument()
  })

  it('renders the namespace and name in the General section', () => {
    setupMocks()
    render(<ConsolidatedTemplateEditorPage />)

    expect(screen.getByText('Namespace')).toBeInTheDocument()
    expect(screen.getByText('Name')).toBeInTheDocument()
    // Both values rendered with the mono style so assert presence of the
    // raw strings in the document.
    expect(screen.getAllByText('prj-billing').length).toBeGreaterThan(0)
    expect(screen.getAllByText('web').length).toBeGreaterThan(0)
  })

  it('falls back to the slug when displayName is empty', () => {
    setupMocks({
      name: 'web',
      namespace: 'prj-billing',
      displayName: '',
      cueTemplate: '// cue',
    })
    render(<ConsolidatedTemplateEditorPage />)
    // Slug used as the heading when displayName is empty.
    expect(screen.getByRole('heading', { name: 'web' })).toBeInTheDocument()
  })

  it('saves via the update RPC, passing the current cueTemplate', async () => {
    const mutateAsync = vi.fn().mockResolvedValue({})
    ;(useGetTemplate as Mock).mockReturnValue({
      data: {
        name: 'web',
        namespace: 'prj-billing',
        displayName: 'Web Service',
        description: 'desc',
        cueTemplate: '// original',
        enabled: true,
        linkedTemplates: [],
      },
      isPending: false,
      error: null,
    })
    ;(useUpdateTemplate as Mock).mockReturnValue({ mutateAsync, isPending: false })
    ;(useDeleteTemplate as Mock).mockReturnValue({
      mutateAsync: vi.fn(),
      isPending: false,
      error: null,
    })
    ;(useListTemplateExamples as Mock).mockReturnValue({
      data: [EXAMPLE_HTTPROUTE],
      isPending: false,
      error: null,
    })

    const user = userEvent.setup()
    render(<ConsolidatedTemplateEditorPage />)

    expect(useUpdateTemplate).toHaveBeenCalledWith('prj-billing', 'web')

    await user.click(screen.getByRole('button', { name: /^save$/i }))
    await waitFor(() => {
      expect(mutateAsync).toHaveBeenCalledWith(
        expect.objectContaining({
          displayName: 'Web Service',
          description: 'desc',
          cueTemplate: '// original',
          enabled: true,
        }),
      )
    })
  })

  // HOL-799: TemplateExamplePicker is wired into the consolidated editor so
  // authors can browse and load examples. Because the editor always starts with
  // an existing CUE body, selecting an example prompts for confirmation first.
  describe('TemplateExamplePicker integration', () => {
    it('renders the Load Example picker trigger', () => {
      setupMocks()
      render(<ConsolidatedTemplateEditorPage />)
      expect(screen.getByRole('combobox', { name: /load example/i })).toBeInTheDocument()
    })

    it('selecting an example with confirmation replaces the CUE template', async () => {
      setupMocks({
        name: 'web',
        namespace: 'prj-billing',
        displayName: 'Web Service',
        cueTemplate: '// original cue body',
        enabled: true,
        linkedTemplates: [],
      })
      vi.spyOn(window, 'confirm').mockReturnValue(true)

      render(<ConsolidatedTemplateEditorPage />)

      fireEvent.click(screen.getByRole('combobox', { name: /load example/i }))
      const item = await screen.findByText(EXAMPLE_HTTPROUTE.displayName)
      fireEvent.click(item)

      await waitFor(() => {
        // The CUE editor in CueTemplateEditor renders a textarea. Find it by
        // its accessible label.
        const textarea = screen.getByRole('textbox', { name: /cue template/i }) as HTMLTextAreaElement
        expect(textarea.value).toBe(EXAMPLE_HTTPROUTE.cueTemplate)
      })
    })

    it('cancelling the confirm dialog leaves the CUE template unchanged', async () => {
      const originalCue = '// original cue body'
      setupMocks({
        name: 'web',
        namespace: 'prj-billing',
        displayName: 'Web Service',
        cueTemplate: originalCue,
        enabled: true,
        linkedTemplates: [],
      })
      vi.spyOn(window, 'confirm').mockReturnValue(false)

      render(<ConsolidatedTemplateEditorPage />)

      fireEvent.click(screen.getByRole('combobox', { name: /load example/i }))
      const item = await screen.findByText(EXAMPLE_HTTPROUTE.displayName)
      fireEvent.click(item)

      await waitFor(() => {
        const textarea = screen.getByRole('textbox', { name: /cue template/i }) as HTMLTextAreaElement
        expect(textarea.value).toBe(originalCue)
      })
    })
  })

  describe('delete flow', () => {
    it('renders the Delete button in the header row', () => {
      setupMocks()
      render(
        <ConsolidatedTemplateEditorPage
          orgName="test-org"
          namespace="prj-billing"
          name="web"
        />,
      )
      // There should be a Delete button visible before the dialog opens.
      expect(screen.getByRole('button', { name: 'Delete' })).toBeInTheDocument()
      // useDeleteTemplate should be wired with the same namespace as
      // useGetTemplate / useUpdateTemplate.
      expect(useDeleteTemplate).toHaveBeenCalledWith('prj-billing')
    })

    it('opens the confirmation dialog when Delete is clicked', async () => {
      setupMocks()
      const user = userEvent.setup()
      render(
        <ConsolidatedTemplateEditorPage
          orgName="test-org"
          namespace="prj-billing"
          name="web"
        />,
      )

      await user.click(screen.getByRole('button', { name: 'Delete' }))

      expect(
        screen.getByRole('heading', { name: 'Delete Template' }),
      ).toBeInTheDocument()
      // Body names the template as namespace/name.
      expect(screen.getByText(/prj-billing\/web/)).toBeInTheDocument()
      // Warning copy.
      expect(
        screen.getByText(/cannot be undone/i),
      ).toBeInTheDocument()
    })

    it('closes the dialog without mutating when Cancel is clicked', async () => {
      const deleteMutateAsync = vi.fn()
      setupMocks()
      ;(useDeleteTemplate as Mock).mockReturnValue({
        mutateAsync: deleteMutateAsync,
        isPending: false,
        error: null,
      })

      const user = userEvent.setup()
      render(
        <ConsolidatedTemplateEditorPage
          orgName="test-org"
          namespace="prj-billing"
          name="web"
        />,
      )

      await user.click(screen.getByRole('button', { name: 'Delete' }))
      expect(
        screen.getByRole('heading', { name: 'Delete Template' }),
      ).toBeInTheDocument()

      await user.click(screen.getByRole('button', { name: 'Cancel' }))

      await waitFor(() => {
        expect(
          screen.queryByRole('heading', { name: 'Delete Template' }),
        ).not.toBeInTheDocument()
      })
      expect(deleteMutateAsync).not.toHaveBeenCalled()
      expect(mockNavigate).not.toHaveBeenCalled()
    })

    it('calls the delete mutation with the template name on confirm', async () => {
      const deleteMutateAsync = vi.fn().mockResolvedValue({})
      setupMocks()
      ;(useDeleteTemplate as Mock).mockReturnValue({
        mutateAsync: deleteMutateAsync,
        isPending: false,
        error: null,
      })

      const user = userEvent.setup()
      render(
        <ConsolidatedTemplateEditorPage
          orgName="test-org"
          namespace="prj-billing"
          name="web"
        />,
      )

      await user.click(screen.getByRole('button', { name: 'Delete' }))
      // Confirm inside the dialog — dialog has its own Delete button.
      const confirmButton = screen
        .getAllByRole('button', { name: 'Delete' })
        .find((btn) => btn.closest('[role="dialog"]'))
      expect(confirmButton).toBeDefined()
      await user.click(confirmButton!)

      await waitFor(() => {
        expect(deleteMutateAsync).toHaveBeenCalledWith({ name: 'web' })
      })
    })

    it('navigates to the org templates index and toasts on success', async () => {
      const deleteMutateAsync = vi.fn().mockResolvedValue({})
      setupMocks()
      ;(useDeleteTemplate as Mock).mockReturnValue({
        mutateAsync: deleteMutateAsync,
        isPending: false,
        error: null,
      })

      const user = userEvent.setup()
      render(
        <ConsolidatedTemplateEditorPage
          orgName="test-org"
          namespace="prj-billing"
          name="web"
        />,
      )

      await user.click(screen.getByRole('button', { name: 'Delete' }))
      const confirmButton = screen
        .getAllByRole('button', { name: 'Delete' })
        .find((btn) => btn.closest('[role="dialog"]'))
      await user.click(confirmButton!)

      await waitFor(() => {
        expect(mockNavigate).toHaveBeenCalledWith({
          to: '/orgs/$orgName/templates',
          params: { orgName: 'test-org' },
        })
      })
      expect(toast.success).toHaveBeenCalledWith('Template deleted')
    })

    it('surfaces an inline error and does not navigate on failure', async () => {
      const deleteMutateAsync = vi
        .fn()
        .mockRejectedValue(new Error('delete forbidden'))
      setupMocks()
      ;(useDeleteTemplate as Mock).mockReturnValue({
        mutateAsync: deleteMutateAsync,
        isPending: false,
        error: new Error('delete forbidden'),
      })

      const user = userEvent.setup()
      render(
        <ConsolidatedTemplateEditorPage
          orgName="test-org"
          namespace="prj-billing"
          name="web"
        />,
      )

      await user.click(screen.getByRole('button', { name: 'Delete' }))
      const confirmButton = screen
        .getAllByRole('button', { name: 'Delete' })
        .find((btn) => btn.closest('[role="dialog"]'))
      await user.click(confirmButton!)

      await waitFor(() => {
        expect(deleteMutateAsync).toHaveBeenCalledWith({ name: 'web' })
      })

      // Inline error inside the dialog, no navigation, no success toast.
      expect(screen.getByText('delete forbidden')).toBeInTheDocument()
      expect(
        screen.getByRole('heading', { name: 'Delete Template' }),
      ).toBeInTheDocument()
      expect(mockNavigate).not.toHaveBeenCalled()
      expect(toast.success).not.toHaveBeenCalled()
    })

    it('shows "Deleting..." and disables the confirm button while pending', async () => {
      setupMocks()
      ;(useDeleteTemplate as Mock).mockReturnValue({
        mutateAsync: vi.fn(),
        isPending: true,
        error: null,
      })

      const user = userEvent.setup()
      render(
        <ConsolidatedTemplateEditorPage
          orgName="test-org"
          namespace="prj-billing"
          name="web"
        />,
      )

      await user.click(screen.getByRole('button', { name: 'Delete' }))

      const deletingButton = await screen.findByRole('button', {
        name: /deleting/i,
      })
      expect(deletingButton).toBeDisabled()
    })
  })
})

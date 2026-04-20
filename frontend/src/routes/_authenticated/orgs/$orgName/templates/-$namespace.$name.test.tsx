import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

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
  }
})

vi.mock('@/queries/templates', () => ({
  useGetTemplate: vi.fn(),
  useUpdateTemplate: vi.fn(),
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

import { useGetTemplate, useUpdateTemplate } from '@/queries/templates'
import { ConsolidatedTemplateEditorPage } from './$namespace.$name'

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
})

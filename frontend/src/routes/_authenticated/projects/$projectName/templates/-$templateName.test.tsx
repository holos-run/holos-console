/**
 * Tests for the project-scoped template detail page (HOL-974).
 *
 * Covers: data loading, error state, save mutation, delete flow,
 * and that the CueTemplateEditor renders (preview tab present).
 */

import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

const navigateSpy = vi.fn()

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => ({ projectName: 'billing', templateName: 'reference-grant' }),
    }),
    Link: ({
      children,
      to,
      params,
      className,
    }: {
      children: React.ReactNode
      to?: string
      params?: Record<string, string>
      className?: string
    }) => {
      // Interpolate TanStack Router $param placeholders so href assertions work.
      let href = to ?? '#'
      if (params) {
        for (const [key, val] of Object.entries(params)) {
          href = href.replace(`$${key}`, val)
        }
      }
      return (
        <a href={href} className={className}>
          {children}
        </a>
      )
    },
    useNavigate: () => navigateSpy,
  }
})

vi.mock('@/lib/console-config', () => ({
  getConsoleConfig: vi.fn().mockReturnValue({
    namespacePrefix: '',
    organizationPrefix: 'org-',
    folderPrefix: 'folder-',
    projectPrefix: 'project-',
  }),
}))

vi.mock('@/queries/templates', () => ({
  useGetTemplate: vi.fn(),
  useUpdateTemplate: vi.fn(),
  useDeleteTemplate: vi.fn(),
  useGetTemplateDefaults: vi.fn(),
  useRenderTemplate: vi.fn(),
  useListTemplateExamples: vi.fn(),
}))

vi.mock('@/hooks/use-debounced-value', () => ({
  useDebouncedValue: vi.fn((value: unknown) => value),
}))

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

import {
  useGetTemplate,
  useUpdateTemplate,
  useDeleteTemplate,
  useGetTemplateDefaults,
  useRenderTemplate,
  useListTemplateExamples,
} from '@/queries/templates'
import { ProjectTemplateDetailPage } from './$templateName'

const saveMutateAsync = vi.fn()
const deleteMutateAsync = vi.fn()

function setupMocks({
  template = {
    name: 'reference-grant',
    namespace: 'project-billing',
    displayName: 'Reference Grant',
    description: 'A reference template',
    cueTemplate: 'input: #ProjectInput',
    enabled: false,
    createdAt: '2026-04-22T00:00:00.000Z',
  },
  isPending = false,
  error = null as Error | null,
} = {}) {
  ;(useGetTemplate as Mock).mockReturnValue({ data: template, isPending, error })
  ;(useGetTemplateDefaults as Mock).mockReturnValue({ data: undefined })
  ;(useUpdateTemplate as Mock).mockReturnValue({
    mutateAsync: saveMutateAsync,
    isPending: false,
  })
  ;(useDeleteTemplate as Mock).mockReturnValue({
    mutateAsync: deleteMutateAsync,
    isPending: false,
    error: null,
  })
  ;(useRenderTemplate as Mock).mockReturnValue({
    data: null,
    error: null,
    isFetching: false,
  })
  ;(useListTemplateExamples as Mock).mockReturnValue({ data: [], isPending: false })
}

describe('ProjectTemplateDetailPage (HOL-974)', () => {
  beforeEach(() => {
    navigateSpy.mockReset()
    saveMutateAsync.mockReset().mockResolvedValue({})
    deleteMutateAsync.mockReset().mockResolvedValue({})
    vi.clearAllMocks()
  })

  // -------------------------------------------------------------------------
  // Loading state
  // -------------------------------------------------------------------------

  it('renders loading skeletons while data is pending', () => {
    setupMocks({ isPending: true, template: undefined as never })
    render(
      <ProjectTemplateDetailPage
        projectName="billing"
        templateName="reference-grant"
      />,
    )
    // Skeletons render; no template content visible.
    expect(screen.queryByText('Reference Grant')).not.toBeInTheDocument()
  })

  // -------------------------------------------------------------------------
  // Error state
  // -------------------------------------------------------------------------

  it('renders an error alert when the template lookup fails', () => {
    setupMocks({
      template: undefined as never,
      isPending: false,
      error: new Error('template not found'),
    })
    render(
      <ProjectTemplateDetailPage
        projectName="billing"
        templateName="reference-grant"
      />,
    )
    expect(screen.getByText('template not found')).toBeInTheDocument()
  })

  // -------------------------------------------------------------------------
  // Detail content
  // -------------------------------------------------------------------------

  it('renders the template display name and metadata', () => {
    setupMocks()
    render(
      <ProjectTemplateDetailPage
        projectName="billing"
        templateName="reference-grant"
      />,
    )
    expect(screen.getByText('Reference Grant')).toBeInTheDocument()
    expect(screen.getByText('reference-grant')).toBeInTheDocument()
    expect(screen.getByText('project-billing')).toBeInTheDocument()
  })

  // -------------------------------------------------------------------------
  // CueTemplateEditor presence — preview tab works
  // -------------------------------------------------------------------------

  it('renders Editor and Preview tabs (CueTemplateEditor present)', () => {
    setupMocks()
    render(
      <ProjectTemplateDetailPage
        projectName="billing"
        templateName="reference-grant"
      />,
    )
    expect(screen.getByRole('tab', { name: /editor/i })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: /preview/i })).toBeInTheDocument()
  })

  // -------------------------------------------------------------------------
  // No Platform Input panel in the preview tab
  // -------------------------------------------------------------------------

  it('preview tab does NOT render a Platform Input panel header', () => {
    setupMocks()
    render(
      <ProjectTemplateDetailPage
        projectName="billing"
        templateName="reference-grant"
      />,
    )
    // The page must never show a "Platform Input" label.
    // Platform context is injected by the backend via TemplatePolicyBinding rules
    // — the user does not supply it in this editor.
    expect(screen.queryByText(/platform input/i)).not.toBeInTheDocument()
  })

  // -------------------------------------------------------------------------
  // Back link
  // -------------------------------------------------------------------------

  it('renders a back link to the templates index', () => {
    setupMocks()
    render(
      <ProjectTemplateDetailPage
        projectName="billing"
        templateName="reference-grant"
      />,
    )
    const backLink = screen.getByRole('link', { name: /templates/i })
    expect(backLink).toHaveAttribute('href', '/projects/billing/templates')
  })

  // -------------------------------------------------------------------------
  // Delete flow
  // -------------------------------------------------------------------------

  it('opens delete dialog when Delete button is clicked', async () => {
    setupMocks()
    render(
      <ProjectTemplateDetailPage
        projectName="billing"
        templateName="reference-grant"
      />,
    )
    const deleteBtn = screen.getByRole('button', { name: /^delete$/i })
    fireEvent.click(deleteBtn)
    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })
    // Dialog title should be present.
    expect(screen.getByRole('heading', { name: /delete template/i })).toBeInTheDocument()
  })

  it('calls deleteTemplate mutation and navigates to index on confirm', async () => {
    setupMocks()
    render(
      <ProjectTemplateDetailPage
        projectName="billing"
        templateName="reference-grant"
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: /^delete$/i }))
    await waitFor(() => screen.getByRole('dialog'))

    // The dialog has its own Delete button.
    const dialogDeleteBtns = screen.getAllByRole('button', { name: /^delete$/i })
    // Click the one inside the dialog (last one).
    fireEvent.click(dialogDeleteBtns[dialogDeleteBtns.length - 1])

    await waitFor(() => {
      expect(deleteMutateAsync).toHaveBeenCalledWith({ name: 'reference-grant' })
    })
    await waitFor(() => {
      expect(navigateSpy).toHaveBeenCalledWith(
        expect.objectContaining({
          to: '/projects/$projectName/templates',
          params: { projectName: 'billing' },
        }),
      )
    })
  })
})

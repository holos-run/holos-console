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
      useParams: () => ({ folderName: 'test-folder', templateName: 'httproute-ingress' }),
    }),
    useNavigate: () => vi.fn(),
    Link: ({
      children,
      ...props
    }: React.AnchorHTMLAttributes<HTMLAnchorElement> & { children: React.ReactNode }) => (
      <a {...props}>{children}</a>
    ),
  }
})

vi.mock('@/queries/templates', () => ({
  useGetTemplate: vi.fn(),
  useUpdateTemplate: vi.fn(),
  useDeleteTemplate: vi.fn(),
  useCloneTemplate: vi.fn(),
  useRenderTemplate: vi.fn(),
  makeFolderScope: vi.fn().mockReturnValue({ scope: 3, scopeName: 'test-folder' }),
}))

vi.mock('@/queries/folders', () => ({
  useGetFolder: vi.fn(),
}))

vi.mock('@/hooks/use-debounced-value', () => ({
  useDebouncedValue: vi.fn((value: unknown) => value),
}))

vi.mock('@/components/template-releases', () => ({
  TemplateReleases: () => <div data-testid="template-releases">Releases</div>,
}))

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

import {
  useGetTemplate,
  useUpdateTemplate,
  useDeleteTemplate,
  useCloneTemplate,
  useRenderTemplate,
} from '@/queries/templates'
import { useGetFolder } from '@/queries/folders'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { FolderTemplateDetailPage } from './$templateName'

const mockTemplate = {
  name: 'httproute-ingress',
  displayName: 'HTTPRoute Ingress',
  description: 'Provides an HTTPRoute for the istio-ingress gateway',
  cueTemplate: '// httproute template content',
  mandatory: true,
  enabled: false,
}

function setupMocks(
  userRole = Role.OWNER,
  templateOverride?: Partial<typeof mockTemplate>,
) {
  const template = { ...mockTemplate, ...templateOverride }
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
    reset: vi.fn(),
  })
  ;(useCloneTemplate as Mock).mockReturnValue({
    mutateAsync: vi.fn().mockResolvedValue({ name: 'new-template' }),
    isPending: false,
  })
  ;(useRenderTemplate as Mock).mockReturnValue({
    data: { renderedYaml: '', renderedJson: '' },
    error: null,
    isFetching: false,
  })
  ;(useGetFolder as Mock).mockReturnValue({
    data: { name: 'test-folder', organization: 'test-org', userRole },
    isPending: false,
    error: null,
  })
}

describe('FolderTemplateDetailPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  // HOL-555 removed the Mandatory badge from the detail view. TemplatePolicy
  // REQUIRE rules (HOL-558) will re-introduce an "always applied" affordance.
  it('renders template name (Mandatory badge removed in HOL-555)', () => {
    setupMocks(Role.OWNER)
    render(
      <FolderTemplateDetailPage
        folderName="test-folder"
        templateName="httproute-ingress"
      />,
    )
    expect(screen.getByText('httproute-ingress')).toBeInTheDocument()
    expect(screen.queryByText('Mandatory')).not.toBeInTheDocument()
  })

  it('renders template display name', () => {
    setupMocks(Role.OWNER)
    render(
      <FolderTemplateDetailPage
        folderName="test-folder"
        templateName="httproute-ingress"
      />,
    )
    expect(screen.getByText('HTTPRoute Ingress')).toBeInTheDocument()
  })

  it('shows Save button for folder OWNER', () => {
    setupMocks(Role.OWNER)
    render(
      <FolderTemplateDetailPage
        folderName="test-folder"
        templateName="httproute-ingress"
      />,
    )
    expect(screen.getByRole('button', { name: /save/i })).toBeInTheDocument()
  })

  it('hides Save button for folder VIEWER', () => {
    setupMocks(Role.VIEWER)
    render(
      <FolderTemplateDetailPage
        folderName="test-folder"
        templateName="httproute-ingress"
      />,
    )
    expect(
      screen.queryByRole('button', { name: /save/i }),
    ).not.toBeInTheDocument()
  })

  it('shows read-only message for non-owner', () => {
    setupMocks(Role.VIEWER)
    render(
      <FolderTemplateDetailPage
        folderName="test-folder"
        templateName="httproute-ingress"
      />,
    )
    expect(screen.getByText(/folder Owner permissions/i)).toBeInTheDocument()
  })

  it('does not show mandatory badge for non-mandatory template', () => {
    setupMocks(Role.OWNER, { mandatory: false })
    render(
      <FolderTemplateDetailPage
        folderName="test-folder"
        templateName="httproute-ingress"
      />,
    )
    expect(screen.queryByText('Mandatory')).not.toBeInTheDocument()
  })

  describe('enabled toggle', () => {
    it('shows enabled toggle', () => {
      setupMocks(Role.OWNER)
      render(
        <FolderTemplateDetailPage
          folderName="test-folder"
          templateName="httproute-ingress"
        />,
      )
      expect(
        screen.getByRole('switch', { name: /enabled/i }),
      ).toBeInTheDocument()
    })

    it('toggle is checked when template is enabled', () => {
      setupMocks(Role.OWNER, { enabled: true })
      render(
        <FolderTemplateDetailPage
          folderName="test-folder"
          templateName="httproute-ingress"
        />,
      )
      const toggle = screen.getByRole('switch', { name: /enabled/i })
      expect(toggle).toHaveAttribute('data-state', 'checked')
    })

    it('toggle is unchecked when template is disabled', () => {
      setupMocks(Role.OWNER, { enabled: false })
      render(
        <FolderTemplateDetailPage
          folderName="test-folder"
          templateName="httproute-ingress"
        />,
      )
      const toggle = screen.getByRole('switch', { name: /enabled/i })
      expect(toggle).toHaveAttribute('data-state', 'unchecked')
    })

    it('clicking toggle calls updateTemplate with new enabled state and preserves all fields', async () => {
      setupMocks(Role.OWNER, { enabled: false })
      const user = userEvent.setup()
      render(
        <FolderTemplateDetailPage
          folderName="test-folder"
          templateName="httproute-ingress"
        />,
      )
      const toggle = screen.getByRole('switch', { name: /enabled/i })
      await user.click(toggle)
      const mutateAsync = (useUpdateTemplate as Mock).mock.results[0].value
        .mutateAsync
      await waitFor(() => {
        // HOL-555 removed `mandatory` from the update payload. TemplatePolicy
        // REQUIRE rules (HOL-557) will take over this concept.
        expect(mutateAsync).toHaveBeenCalledWith(
          expect.objectContaining({
            enabled: true,
            displayName: mockTemplate.displayName,
            description: mockTemplate.description,
            cueTemplate: mockTemplate.cueTemplate,
          }),
        )
      })
    })

    it('toggle is disabled for folder VIEWER', () => {
      setupMocks(Role.VIEWER)
      render(
        <FolderTemplateDetailPage
          folderName="test-folder"
          templateName="httproute-ingress"
        />,
      )
      const toggle = screen.getByRole('switch', { name: /enabled/i })
      expect(toggle).toBeDisabled()
    })
  })

  describe('delete template', () => {
    it('shows Delete Template button for folder OWNER', () => {
      setupMocks(Role.OWNER)
      render(
        <FolderTemplateDetailPage
          folderName="test-folder"
          templateName="httproute-ingress"
        />,
      )
      expect(
        screen.getByRole('button', { name: /delete template/i }),
      ).toBeInTheDocument()
    })

    it('does not show Delete Template button for folder VIEWER', () => {
      setupMocks(Role.VIEWER)
      render(
        <FolderTemplateDetailPage
          folderName="test-folder"
          templateName="httproute-ingress"
        />,
      )
      expect(
        screen.queryByRole('button', { name: /delete template/i }),
      ).not.toBeInTheDocument()
    })

    it('clicking Delete Template opens confirmation dialog', () => {
      setupMocks(Role.OWNER)
      render(
        <FolderTemplateDetailPage
          folderName="test-folder"
          templateName="httproute-ingress"
        />,
      )
      fireEvent.click(screen.getByRole('button', { name: /delete template/i }))
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })

    it('confirming delete calls useDeleteTemplate', async () => {
      setupMocks(Role.OWNER)
      render(
        <FolderTemplateDetailPage
          folderName="test-folder"
          templateName="httproute-ingress"
        />,
      )
      fireEvent.click(screen.getByRole('button', { name: /delete template/i }))
      fireEvent.click(screen.getByRole('button', { name: /^delete$/i }))
      const mutateAsync = (useDeleteTemplate as Mock).mock.results[0].value
        .mutateAsync
      await waitFor(() => {
        expect(mutateAsync).toHaveBeenCalledWith({
          name: 'httproute-ingress',
        })
      })
    })

    it('cancel closes delete dialog without calling delete', async () => {
      setupMocks(Role.OWNER)
      const user = userEvent.setup()
      render(
        <FolderTemplateDetailPage
          folderName="test-folder"
          templateName="httproute-ingress"
        />,
      )
      await user.click(screen.getByRole('button', { name: /delete template/i }))
      await user.click(screen.getByRole('button', { name: /cancel/i }))
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
      const mutateAsync = (useDeleteTemplate as Mock).mock.results[0].value
        .mutateAsync
      expect(mutateAsync).not.toHaveBeenCalled()
    })
  })

  describe('clone button', () => {
    it('shows clone button', () => {
      setupMocks(Role.OWNER)
      render(
        <FolderTemplateDetailPage
          folderName="test-folder"
          templateName="httproute-ingress"
        />,
      )
      expect(
        screen.getByRole('button', { name: /clone/i }),
      ).toBeInTheDocument()
    })

    it('does not show clone button for viewer', () => {
      setupMocks(Role.VIEWER)
      render(
        <FolderTemplateDetailPage
          folderName="test-folder"
          templateName="httproute-ingress"
        />,
      )
      expect(
        screen.queryByRole('button', { name: /clone/i }),
      ).not.toBeInTheDocument()
    })

    it('clicking clone opens dialog', async () => {
      setupMocks(Role.OWNER)
      const user = userEvent.setup()
      render(
        <FolderTemplateDetailPage
          folderName="test-folder"
          templateName="httproute-ingress"
        />,
      )
      await user.click(screen.getByRole('button', { name: /clone/i }))
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })

    it('clone dialog has name and display name fields', async () => {
      setupMocks(Role.OWNER)
      const user = userEvent.setup()
      render(
        <FolderTemplateDetailPage
          folderName="test-folder"
          templateName="httproute-ingress"
        />,
      )
      await user.click(screen.getByRole('button', { name: /clone/i }))
      expect(
        screen.getByRole('textbox', { name: /^name$/i }),
      ).toBeInTheDocument()
      expect(
        screen.getByRole('textbox', { name: /display name/i }),
      ).toBeInTheDocument()
    })

    it('confirming clone calls cloneTemplate', async () => {
      setupMocks(Role.OWNER)
      const user = userEvent.setup()
      render(
        <FolderTemplateDetailPage
          folderName="test-folder"
          templateName="httproute-ingress"
        />,
      )
      await user.click(screen.getByRole('button', { name: /clone/i }))
      const nameInput = screen.getByRole('textbox', { name: /^name$/i })
      await user.clear(nameInput)
      await user.type(nameInput, 'new-template')
      const displayNameInput = screen.getByRole('textbox', {
        name: /display name/i,
      })
      await user.clear(displayNameInput)
      await user.type(displayNameInput, 'New Template')
      await user.click(screen.getByRole('button', { name: /^clone$/i }))
      const mutateAsync = (useCloneTemplate as Mock).mock.results[0].value
        .mutateAsync
      await waitFor(() => {
        expect(mutateAsync).toHaveBeenCalledWith(
          expect.objectContaining({
            sourceName: 'httproute-ingress',
            name: 'new-template',
            displayName: 'New Template',
          }),
        )
      })
    })

    it('cancel closes clone dialog without saving', async () => {
      setupMocks(Role.OWNER)
      const user = userEvent.setup()
      render(
        <FolderTemplateDetailPage
          folderName="test-folder"
          templateName="httproute-ingress"
        />,
      )
      await user.click(screen.getByRole('button', { name: /clone/i }))
      await user.click(screen.getByRole('button', { name: /cancel/i }))
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
      const mutateAsync = (useCloneTemplate as Mock).mock.results[0].value
        .mutateAsync
      expect(mutateAsync).not.toHaveBeenCalled()
    })
  })

  it('shows skeleton while loading', () => {
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
      reset: vi.fn(),
    })
    ;(useCloneTemplate as Mock).mockReturnValue({
      mutateAsync: vi.fn().mockResolvedValue({ name: 'new' }),
      isPending: false,
    })
    ;(useRenderTemplate as Mock).mockReturnValue({
      data: { renderedYaml: '', renderedJson: '' },
      error: null,
      isFetching: false,
    })
    ;(useGetFolder as Mock).mockReturnValue({
      data: { name: 'test-folder', organization: 'test-org', userRole: Role.OWNER },
      isPending: false,
      error: null,
    })
    render(
      <FolderTemplateDetailPage
        folderName="test-folder"
        templateName="httproute-ingress"
      />,
    )
    const skeletons = document.querySelectorAll('[data-slot="skeleton"]')
    expect(skeletons.length).toBeGreaterThan(0)
  })

  it('shows error alert when fetch fails', () => {
    ;(useGetTemplate as Mock).mockReturnValue({
      data: undefined,
      isPending: false,
      error: new Error('not found'),
    })
    ;(useUpdateTemplate as Mock).mockReturnValue({
      mutateAsync: vi.fn(),
      isPending: false,
    })
    ;(useDeleteTemplate as Mock).mockReturnValue({
      mutateAsync: vi.fn(),
      isPending: false,
      error: null,
      reset: vi.fn(),
    })
    ;(useCloneTemplate as Mock).mockReturnValue({
      mutateAsync: vi.fn().mockResolvedValue({ name: 'new' }),
      isPending: false,
    })
    ;(useRenderTemplate as Mock).mockReturnValue({
      data: { renderedYaml: '', renderedJson: '' },
      error: null,
      isFetching: false,
    })
    ;(useGetFolder as Mock).mockReturnValue({
      data: { name: 'test-folder', organization: 'test-org', userRole: Role.OWNER },
      isPending: false,
      error: null,
    })
    render(
      <FolderTemplateDetailPage
        folderName="test-folder"
        templateName="httproute-ingress"
      />,
    )
    expect(screen.getByText('not found')).toBeInTheDocument()
  })

  // HOL-555 removed the `mandatory` field from the Template proto; this test
  // only preserves `enabled` now. TemplatePolicy REQUIRE rules (HOL-557) will
  // take over the concept.
  it('Save calls useUpdateTemplate with changed CUE template and preserves enabled', async () => {
    setupMocks(Role.OWNER, { enabled: true })
    render(
      <FolderTemplateDetailPage
        folderName="test-folder"
        templateName="httproute-ingress"
      />,
    )
    const editor = screen.getByRole('textbox', { name: /cue template/i })
    fireEvent.change(editor, { target: { value: '// new cue content' } })
    fireEvent.click(screen.getByRole('button', { name: /save/i }))
    const mutateAsync = (useUpdateTemplate as Mock).mock.results[0].value
      .mutateAsync
    await waitFor(() => {
      expect(mutateAsync).toHaveBeenCalledWith(
        expect.objectContaining({
          cueTemplate: '// new cue content',
          enabled: true,
        }),
      )
    })
  })
})

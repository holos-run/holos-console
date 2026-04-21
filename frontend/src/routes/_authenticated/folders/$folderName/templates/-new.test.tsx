import { render, screen, fireEvent, waitFor } from '@testing-library/react'
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
    Link: ({ children, className, to, params }: { children: React.ReactNode; className?: string; to?: string; params?: Record<string, string> }) => (
      <a href={to} data-params={JSON.stringify(params)} className={className}>{children}</a>
    ),
  }
})

// Flatten Tooltip so TooltipContent renders inline in jsdom. The hover
// interaction itself belongs to Radix Tooltip and is not exercised here;
// this keeps content-level assertions (tooltip copy) reachable via
// getByText without faking pointer events.
vi.mock('@/components/ui/tooltip', () => ({
  Tooltip: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipTrigger: ({
    children,
    asChild,
  }: {
    children: React.ReactNode
    asChild?: boolean
  }) => (asChild ? <>{children}</> : <span>{children}</span>),
  TooltipContent: ({
    children,
    ...rest
  }: React.HTMLAttributes<HTMLDivElement> & { children: React.ReactNode }) => (
    <div {...rest}>{children}</div>
  ),
}))

vi.mock('@/queries/templates', () => ({
  useCreateTemplate: vi.fn(),
  useListTemplateExamples: vi.fn(),
}))

vi.mock('@/queries/folders', () => ({
  useGetFolder: vi.fn(),
}))

import { useCreateTemplate, useListTemplateExamples } from '@/queries/templates'
import { useGetFolder } from '@/queries/folders'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { CreateFolderTemplatePage } from './new'

const EXAMPLE_HTTPROUTE = {
  name: 'httproute-v1',
  displayName: 'HTTPRoute Ingress',
  description: 'Provides an HTTPRoute for the org-configured ingress gateway.',
  cueTemplate: '// example CUE\nplatformResources: {}\n',
}

const EXAMPLE_SECOND = {
  name: 'configmap-v1',
  displayName: 'ConfigMap Starter',
  description: 'A minimal ConfigMap scaffold for project-scope templates.',
  cueTemplate: '// another example\nprojectResources: {}\n',
}

function setupMocks(
  mutateAsync = vi.fn().mockResolvedValue({}),
  userRole = Role.OWNER,
  examples: typeof EXAMPLE_HTTPROUTE[] = [EXAMPLE_HTTPROUTE, EXAMPLE_SECOND],
) {
  ;(useCreateTemplate as Mock).mockReturnValue({
    mutateAsync,
    isPending: false,
    reset: vi.fn(),
  })
  ;(useGetFolder as Mock).mockReturnValue({
    data: { name: 'test-folder', organization: 'test-org', userRole },
    isPending: false,
    error: null,
  })
  ;(useListTemplateExamples as Mock).mockReturnValue({
    data: examples,
    isPending: false,
    error: null,
  })
}

describe('CreateFolderTemplatePage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setupMocks()
  })

  it('renders the page heading', () => {
    render(<CreateFolderTemplatePage folderName="test-folder" />)
    expect(screen.getByText(/create platform template/i)).toBeInTheDocument()
  })

  it('renders Display Name field', () => {
    render(<CreateFolderTemplatePage folderName="test-folder" />)
    expect(screen.getByLabelText(/display name/i)).toBeInTheDocument()
  })

  it('renders Name (slug) field', () => {
    render(<CreateFolderTemplatePage folderName="test-folder" />)
    expect(screen.getByLabelText(/name slug/i)).toBeInTheDocument()
  })

  it('renders Description field', () => {
    render(<CreateFolderTemplatePage folderName="test-folder" />)
    expect(screen.getByLabelText(/description/i)).toBeInTheDocument()
  })

  it('renders CUE Template textarea', () => {
    render(<CreateFolderTemplatePage folderName="test-folder" />)
    expect(screen.getByRole('textbox', { name: /cue template/i })).toBeInTheDocument()
  })

  it('renders Enabled switch defaulting to checked', () => {
    render(<CreateFolderTemplatePage folderName="test-folder" />)
    const toggle = screen.getByRole('switch', { name: /enabled/i })
    expect(toggle).toBeInTheDocument()
    expect(toggle).toHaveAttribute('data-state', 'checked')
  })

  it('renders Enabled label without the old parenthetical', () => {
    render(<CreateFolderTemplatePage folderName="test-folder" />)
    const label = screen.getByText(/^Enabled$/)
    expect(label).toBeInTheDocument()
    expect(
      screen.queryByText(/apply to projects in this folder/i),
    ).not.toBeInTheDocument()
  })

  it('renders the TemplatePolicyBinding tooltip copy', () => {
    render(<CreateFolderTemplatePage folderName="test-folder" />)
    // Use a custom matcher on the <p> element (matched by tagName) so
    // whitespace from JSX line breaks inside the sentence does not defeat
    // an exact-text match. The acceptance criterion specifies the tooltip
    // reads exactly this sentence.
    const expected =
      'Unified with resources bound to this Template by Policy when enabled. See TemplatePolicyBinding.'
    const node = screen.getByText((_content, element) => {
      if (!element || element.tagName !== 'P') return false
      const text = element.textContent?.replace(/\s+/g, ' ').trim()
      return text === expected
    })
    expect(node).toBeInTheDocument()
  })

  it('renders Create submit button', () => {
    render(<CreateFolderTemplatePage folderName="test-folder" />)
    expect(screen.getByRole('button', { name: /^create$/i })).toBeInTheDocument()
  })

  it('renders a Cancel link back to the templates list', () => {
    render(<CreateFolderTemplatePage folderName="test-folder" />)
    expect(screen.getByRole('link', { name: /cancel/i })).toBeInTheDocument()
  })

  it('auto-derives slug from display name', () => {
    render(<CreateFolderTemplatePage folderName="test-folder" />)
    const displayNameInput = screen.getByLabelText(/display name/i)
    fireEvent.change(displayNameInput, { target: { value: 'My Web App' } })
    const slugInput = screen.getByLabelText(/name slug/i) as HTMLInputElement
    expect(slugInput.value).toBe('my-web-app')
  })

  it('shows validation error when name is empty', async () => {
    render(<CreateFolderTemplatePage folderName="test-folder" />)
    fireEvent.click(screen.getByRole('button', { name: /^create$/i }))
    await waitFor(() => {
      expect(screen.getByText(/template name is required/i)).toBeInTheDocument()
    })
  })

  it('calls createMutation with form values on submit', async () => {
    const mutateAsync = vi.fn().mockResolvedValue({})
    setupMocks(mutateAsync)
    render(<CreateFolderTemplatePage folderName="test-folder" />)

    fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'My Template' } })
    fireEvent.change(screen.getByLabelText(/description/i), { target: { value: 'A description' } })
    fireEvent.click(screen.getByRole('button', { name: /^create$/i }))

    await waitFor(() => {
      expect(mutateAsync).toHaveBeenCalledWith(
        expect.objectContaining({
          name: 'my-template',
          displayName: 'My Template',
          description: 'A description',
          // Enabled now defaults to true; the switch is rendered checked on
          // mount and the mutation carries that value unless the user flips
          // it off.
          enabled: true,
        }),
      )
    })
  })

  // HOL-555 removed the Mandatory proto field; HOL-558 shifts the concept to
  // TemplatePolicy REQUIRE rules. The Mandatory toggle must not render on the
  // template create form and the mutation payload must not carry a mandatory
  // field.
  it('does not render a Mandatory toggle (removed in HOL-555)', () => {
    render(<CreateFolderTemplatePage folderName="test-folder" />)
    expect(screen.queryByRole('switch', { name: /mandatory/i })).not.toBeInTheDocument()
    expect(screen.queryByLabelText(/mandatory/i)).not.toBeInTheDocument()
  })

  it('does not pass mandatory in the mutation payload', async () => {
    const mutateAsync = vi.fn().mockResolvedValue({})
    setupMocks(mutateAsync)
    render(<CreateFolderTemplatePage folderName="test-folder" />)

    fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'My Template' } })
    fireEvent.click(screen.getByRole('button', { name: /^create$/i }))

    await waitFor(() => {
      expect(mutateAsync).toHaveBeenCalled()
    })
    const payload = mutateAsync.mock.calls[0][0]
    expect(payload).not.toHaveProperty('mandatory')
  })

  it('navigates to template detail page after successful creation', async () => {
    const mutateAsync = vi.fn().mockResolvedValue({})
    setupMocks(mutateAsync)
    render(<CreateFolderTemplatePage folderName="test-folder" />)

    fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'My Template' } })
    fireEvent.click(screen.getByRole('button', { name: /^create$/i }))

    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith(
        expect.objectContaining({
          to: '/folders/$folderName/templates/$templateName',
          params: expect.objectContaining({ folderName: 'test-folder', templateName: 'my-template' }),
        }),
      )
    })
  })

  it('shows error message when creation fails', async () => {
    const mutateAsync = vi.fn().mockRejectedValue(new Error('server error'))
    setupMocks(mutateAsync)
    render(<CreateFolderTemplatePage folderName="test-folder" />)

    fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'My Template' } })
    fireEvent.click(screen.getByRole('button', { name: /^create$/i }))

    await waitFor(() => {
      expect(screen.getByText(/server error/i)).toBeInTheDocument()
    })
  })

  it('renders breadcrumb navigation', () => {
    render(<CreateFolderTemplatePage folderName="test-folder" />)
    expect(screen.getByText('Platform Templates')).toBeInTheDocument()
    expect(screen.getByText('test-folder')).toBeInTheDocument()
  })

  // HOL-798: the inline "Load Example" button and hard-coded CUE body were
  // replaced by the reusable TemplateExamplePicker backed by
  // ListTemplateExamples. The picker is the single source of example content
  // from now on.
  describe('TemplateExamplePicker integration', () => {
    it('renders the Load Example picker trigger', () => {
      render(<CreateFolderTemplatePage folderName="test-folder" />)
      expect(screen.getByRole('combobox', { name: /load example/i })).toBeInTheDocument()
    })

    it('no longer renders a plain "Load Example" push button', () => {
      render(<CreateFolderTemplatePage folderName="test-folder" />)
      // Picker trigger is exposed as role=combobox. A plain role=button with
      // that accessible name would indicate the old inline button survived.
      expect(
        screen.queryByRole('button', { name: /load example/i }),
      ).not.toBeInTheDocument()
    })

    it('selecting an example populates all four form fields in one action', async () => {
      render(<CreateFolderTemplatePage folderName="test-folder" />)
      fireEvent.click(screen.getByRole('combobox', { name: /load example/i }))

      const item = await screen.findByText(EXAMPLE_HTTPROUTE.displayName)
      fireEvent.click(item)

      await waitFor(() => {
        const displayNameInput = screen.getByLabelText(/display name/i) as HTMLInputElement
        expect(displayNameInput.value).toBe(EXAMPLE_HTTPROUTE.displayName)
      })
      const nameInput = screen.getByLabelText(/name slug/i) as HTMLInputElement
      expect(nameInput.value).toBe(EXAMPLE_HTTPROUTE.name)
      const descriptionInput = screen.getByLabelText(/description/i) as HTMLInputElement
      expect(descriptionInput.value).toBe(EXAMPLE_HTTPROUTE.description)
      const cueEditor = screen.getByRole('textbox', { name: /cue template/i }) as HTMLTextAreaElement
      expect(cueEditor.value).toBe(EXAMPLE_HTTPROUTE.cueTemplate)
    })
  })

  describe('Enabled switch', () => {
    it('passes enabled: false when toggle is switched off', async () => {
      const mutateAsync = vi.fn().mockResolvedValue({})
      setupMocks(mutateAsync)
      render(<CreateFolderTemplatePage folderName="test-folder" />)

      fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'My Template' } })
      // Toggle starts checked; one click flips it off.
      fireEvent.click(screen.getByRole('switch', { name: /enabled/i }))
      fireEvent.click(screen.getByRole('button', { name: /^create$/i }))

      await waitFor(() => {
        expect(mutateAsync).toHaveBeenCalledWith(
          expect.objectContaining({
            enabled: false,
          }),
        )
      })
    })
  })

  describe('permissions', () => {
    it('disables form fields for non-OWNER users', () => {
      setupMocks(vi.fn().mockResolvedValue({}), Role.VIEWER)
      render(<CreateFolderTemplatePage folderName="test-folder" />)

      expect(screen.getByLabelText(/display name/i)).toBeDisabled()
      expect(screen.getByLabelText(/name slug/i)).toBeDisabled()
      expect(screen.getByLabelText(/description/i)).toBeDisabled()
      expect(screen.getByRole('textbox', { name: /cue template/i })).toBeDisabled()
      expect(screen.getByRole('switch', { name: /enabled/i })).toBeDisabled()
      expect(screen.getByRole('button', { name: /^create$/i })).toBeDisabled()
      expect(screen.getByRole('combobox', { name: /load example/i })).toBeDisabled()
    })

    it('enables form fields for OWNER users', () => {
      setupMocks(vi.fn().mockResolvedValue({}), Role.OWNER)
      render(<CreateFolderTemplatePage folderName="test-folder" />)

      expect(screen.getByLabelText(/display name/i)).not.toBeDisabled()
      expect(screen.getByLabelText(/name slug/i)).not.toBeDisabled()
      expect(screen.getByLabelText(/description/i)).not.toBeDisabled()
      expect(screen.getByRole('textbox', { name: /cue template/i })).not.toBeDisabled()
      expect(screen.getByRole('button', { name: /^create$/i })).not.toBeDisabled()
    })
  })
})

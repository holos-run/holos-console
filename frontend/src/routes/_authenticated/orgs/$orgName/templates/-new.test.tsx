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
      useParams: () => ({ orgName: 'test-org' }),
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

vi.mock('@/queries/templates', () => ({
  useCreateTemplate: vi.fn(),
  useListTemplateExamples: vi.fn(),
}))

vi.mock('@/queries/organizations', () => ({
  useGetOrganization: vi.fn(),
}))

import { useCreateTemplate, useListTemplateExamples } from '@/queries/templates'
import { useGetOrganization } from '@/queries/organizations'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { CreateOrgTemplatePage } from './new'

const EXAMPLE_HTTPROUTE = {
  name: 'httproute-v1',
  displayName: 'HTTPRoute (v1)',
  description: 'Exposes project services via an HTTPRoute into the org-configured ingress gateway.',
  cueTemplate: '// httproute CUE\nplatformResources: {}\n',
}

const EXAMPLE_SECOND = {
  name: 'allowed-project-resource-kinds-v1',
  displayName: 'Allowed Project Resource Kinds (v1)',
  description: 'Closes projectResources.namespacedResources.',
  cueTemplate: '// allowed CUE\nprojectResources: {}\n',
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
  ;(useGetOrganization as Mock).mockReturnValue({
    data: { name: 'test-org', userRole },
    isPending: false,
    error: null,
  })
  ;(useListTemplateExamples as Mock).mockReturnValue({
    data: examples,
    isPending: false,
    error: null,
  })
}

describe('CreateOrgTemplatePage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setupMocks()
  })

  it('renders the page heading', () => {
    render(<CreateOrgTemplatePage orgName="test-org" />)
    expect(screen.getByText(/create platform template/i)).toBeInTheDocument()
  })

  it('renders Display Name field', () => {
    render(<CreateOrgTemplatePage orgName="test-org" />)
    expect(screen.getByLabelText(/display name/i)).toBeInTheDocument()
  })

  it('renders Name (slug) field', () => {
    render(<CreateOrgTemplatePage orgName="test-org" />)
    expect(screen.getByLabelText(/name slug/i)).toBeInTheDocument()
  })

  it('renders Description field', () => {
    render(<CreateOrgTemplatePage orgName="test-org" />)
    expect(screen.getByLabelText(/description/i)).toBeInTheDocument()
  })

  it('renders CUE Template textarea', () => {
    render(<CreateOrgTemplatePage orgName="test-org" />)
    expect(
      screen.getByRole('textbox', { name: /cue template/i }),
    ).toBeInTheDocument()
  })

  it('renders Enabled switch defaulting to unchecked', () => {
    render(<CreateOrgTemplatePage orgName="test-org" />)
    const toggle = screen.getByRole('switch', { name: /enabled/i })
    expect(toggle).toBeInTheDocument()
    expect(toggle).toHaveAttribute('data-state', 'unchecked')
  })

  it('renders Create submit button', () => {
    render(<CreateOrgTemplatePage orgName="test-org" />)
    expect(
      screen.getByRole('button', { name: /^create$/i }),
    ).toBeInTheDocument()
  })

  it('renders a Cancel link back to the templates list', () => {
    render(<CreateOrgTemplatePage orgName="test-org" />)
    expect(screen.getByRole('link', { name: /cancel/i })).toBeInTheDocument()
  })

  it('auto-derives slug from display name', () => {
    render(<CreateOrgTemplatePage orgName="test-org" />)
    const displayNameInput = screen.getByLabelText(/display name/i)
    fireEvent.change(displayNameInput, { target: { value: 'My Web App' } })
    const slugInput = screen.getByLabelText(/name slug/i) as HTMLInputElement
    expect(slugInput.value).toBe('my-web-app')
  })

  it('shows validation error when name is empty', async () => {
    render(<CreateOrgTemplatePage orgName="test-org" />)
    fireEvent.click(screen.getByRole('button', { name: /^create$/i }))
    await waitFor(() => {
      expect(
        screen.getByText(/template name is required/i),
      ).toBeInTheDocument()
    })
  })

  it('calls createMutation with form values on submit', async () => {
    const mutateAsync = vi.fn().mockResolvedValue({})
    setupMocks(mutateAsync)
    render(<CreateOrgTemplatePage orgName="test-org" />)

    fireEvent.change(screen.getByLabelText(/display name/i), {
      target: { value: 'My Template' },
    })
    fireEvent.change(screen.getByLabelText(/description/i), {
      target: { value: 'A description' },
    })
    fireEvent.click(screen.getByRole('button', { name: /^create$/i }))

    await waitFor(() => {
      expect(mutateAsync).toHaveBeenCalledWith(
        expect.objectContaining({
          name: 'my-template',
          displayName: 'My Template',
          description: 'A description',
          enabled: false,
        }),
      )
    })
  })

  it('navigates to the consolidated editor after successful creation', async () => {
    const mutateAsync = vi.fn().mockResolvedValue({})
    setupMocks(mutateAsync)
    render(<CreateOrgTemplatePage orgName="test-org" />)

    fireEvent.change(screen.getByLabelText(/display name/i), {
      target: { value: 'My Template' },
    })
    fireEvent.click(screen.getByRole('button', { name: /^create$/i }))

    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith(
        expect.objectContaining({
          to: '/orgs/$orgName/templates/$namespace/$name',
          params: expect.objectContaining({
            orgName: 'test-org',
            name: 'my-template',
          }),
        }),
      )
    })
  })

  it('shows error message when creation fails', async () => {
    const mutateAsync = vi.fn().mockRejectedValue(new Error('server error'))
    setupMocks(mutateAsync)
    render(<CreateOrgTemplatePage orgName="test-org" />)

    fireEvent.change(screen.getByLabelText(/display name/i), {
      target: { value: 'My Template' },
    })
    fireEvent.click(screen.getByRole('button', { name: /^create$/i }))

    await waitFor(() => {
      expect(screen.getByText(/server error/i)).toBeInTheDocument()
    })
  })

  it('renders breadcrumb navigation', () => {
    render(<CreateOrgTemplatePage orgName="test-org" />)
    expect(screen.getByText('Templates')).toBeInTheDocument()
    expect(screen.getByText('test-org')).toBeInTheDocument()
  })

  // HOL-800: the inline "Load Example" button and hard-coded CUE body were
  // replaced by the TemplateExamplePicker (same picker used on folder/project
  // new-template pages). Verify the transition.
  describe('example picker', () => {
    it('renders the Load Example picker trigger', () => {
      render(<CreateOrgTemplatePage orgName="test-org" />)
      // TemplateExamplePicker exposes role=combobox (shadcn Button with role override).
      expect(screen.getByRole('combobox', { name: /load example/i })).toBeInTheDocument()
    })

    it('no longer renders a plain hard-coded "Load Example" push button', () => {
      render(<CreateOrgTemplatePage orgName="test-org" />)
      // The old inline Load Example button filled a fixed httpbin-platform template.
      // Now all load-example logic lives in the picker popover — the trigger is
      // role=combobox, not role=button, so a plain button query returns null.
      expect(
        screen.queryByRole('button', { name: /load example/i }),
      ).toBeNull()
    })

    it('selecting an example from the picker fills all form fields', async () => {
      const user = userEvent.setup()
      render(<CreateOrgTemplatePage orgName="test-org" />)

      // Open the picker.
      await user.click(screen.getByRole('combobox', { name: /load example/i }))

      // Pick the first example.
      const item = await screen.findByText(EXAMPLE_HTTPROUTE.displayName)
      await user.click(item)

      const displayNameInput = screen.getByLabelText(/display name/i) as HTMLInputElement
      expect(displayNameInput.value).toBe(EXAMPLE_HTTPROUTE.displayName)

      const nameInput = screen.getByLabelText(/name slug/i) as HTMLInputElement
      expect(nameInput.value).toBe(EXAMPLE_HTTPROUTE.name)

      const descriptionInput = screen.getByLabelText(/description/i) as HTMLInputElement
      expect(descriptionInput.value).toBe(EXAMPLE_HTTPROUTE.description)

      const cueEditor = screen.getByRole('textbox', { name: /cue template/i }) as HTMLTextAreaElement
      expect(cueEditor.value).toBe(EXAMPLE_HTTPROUTE.cueTemplate)
    })
  })

  describe('permissions', () => {
    it('disables form fields for non-OWNER users', () => {
      setupMocks(vi.fn().mockResolvedValue({}), Role.VIEWER)
      render(<CreateOrgTemplatePage orgName="test-org" />)

      expect(screen.getByLabelText(/display name/i)).toBeDisabled()
      expect(screen.getByLabelText(/name slug/i)).toBeDisabled()
      expect(screen.getByLabelText(/description/i)).toBeDisabled()
      expect(
        screen.getByRole('textbox', { name: /cue template/i }),
      ).toBeDisabled()
      expect(screen.getByRole('switch', { name: /enabled/i })).toBeDisabled()
      expect(screen.getByRole('button', { name: /^create$/i })).toBeDisabled()
    })

    it('enables form fields for OWNER users', () => {
      setupMocks(vi.fn().mockResolvedValue({}), Role.OWNER)
      render(<CreateOrgTemplatePage orgName="test-org" />)

      expect(screen.getByLabelText(/display name/i)).not.toBeDisabled()
      expect(screen.getByLabelText(/name slug/i)).not.toBeDisabled()
      expect(screen.getByLabelText(/description/i)).not.toBeDisabled()
      expect(
        screen.getByRole('textbox', { name: /cue template/i }),
      ).not.toBeDisabled()
      expect(
        screen.getByRole('button', { name: /^create$/i }),
      ).not.toBeDisabled()
    })
  })
})

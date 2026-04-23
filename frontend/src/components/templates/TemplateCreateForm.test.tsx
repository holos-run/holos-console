import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

// Radix pointer-capture polyfills for jsdom — same pattern used by other
// Combobox/Select-based tests in this codebase.
if (!Element.prototype.hasPointerCapture) {
  Element.prototype.hasPointerCapture = () => false
}
if (!Element.prototype.setPointerCapture) {
  Element.prototype.setPointerCapture = () => {}
}
if (!Element.prototype.releasePointerCapture) {
  Element.prototype.releasePointerCapture = () => {}
}

// Flatten Tooltip so TooltipContent renders inline in jsdom.
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
  useRenderTemplate: vi.fn(),
  useListTemplateExamples: vi.fn(),
}))

// @/queries/organizations is no longer imported by TemplateCreateForm; the mock
// is kept for safety in case any indirect import path references it during tests.
vi.mock('@/queries/organizations', () => ({
  useGetOrganization: vi.fn(),
}))

vi.mock('@/hooks/use-debounced-value', () => ({
  useDebouncedValue: vi.fn((value: unknown) => value),
}))

import {
  useRenderTemplate,
  useListTemplateExamples,
} from '@/queries/templates'
import { TemplateCreateForm } from './TemplateCreateForm'

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

function setupQueryMocks({
  renderData,
  renderError,
  examples = [EXAMPLE_HTTPROUTE, EXAMPLE_SECOND],
}: {
  renderData?: { renderedJson?: string; platformResourcesJson?: string; projectResourcesJson?: string }
  renderError?: Error
  examples?: typeof EXAMPLE_HTTPROUTE[]
} = {}) {
  ;(useRenderTemplate as Mock).mockReturnValue({
    data: renderData ?? undefined,
    error: renderError ?? null,
    isLoading: false,
    isError: !!renderError,
  })
  ;(useListTemplateExamples as Mock).mockReturnValue({
    data: examples,
    isPending: false,
    error: null,
  })
}

beforeEach(() => {
  vi.clearAllMocks()
  setupQueryMocks()
})

describe('TemplateCreateForm — shared behavior', () => {
  const scopes = ['organization', 'folder', 'project'] as const

  it.each(scopes)('renders the four common fields (%s)', (scopeType) => {
    render(
      <TemplateCreateForm
        scopeType={scopeType}
        namespace="holos-org-test-org"
        organization="test-org"
        projectName={scopeType === 'project' ? 'test-project' : undefined}
        canWrite
        onSubmit={vi.fn()}
        onCancel={vi.fn()}
      />,
    )
    expect(screen.getByLabelText(/display name/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/name slug/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/description/i)).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: /cue template/i })).toBeInTheDocument()
  })

  it.each(scopes)('auto-derives slug from display name (%s)', (scopeType) => {
    render(
      <TemplateCreateForm
        scopeType={scopeType}
        namespace="holos-org-test-org"
        organization="test-org"
        projectName={scopeType === 'project' ? 'test-project' : undefined}
        canWrite
        onSubmit={vi.fn()}
        onCancel={vi.fn()}
      />,
    )
    fireEvent.change(screen.getByLabelText(/display name/i), {
      target: { value: 'My Web App' },
    })
    expect((screen.getByLabelText(/name slug/i) as HTMLInputElement).value).toBe(
      'my-web-app',
    )
  })

  it.each(scopes)('rejects submission when name is empty (%s)', async (scopeType) => {
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    render(
      <TemplateCreateForm
        scopeType={scopeType}
        namespace="holos-org-test-org"
        organization="test-org"
        projectName={scopeType === 'project' ? 'test-project' : undefined}
        canWrite
        onSubmit={onSubmit}
        onCancel={vi.fn()}
      />,
    )
    const label = scopeType === 'project' ? /create template/i : /^create$/i
    fireEvent.click(screen.getByRole('button', { name: label }))
    await waitFor(() => {
      expect(screen.getByText(/template name is required/i)).toBeInTheDocument()
    })
    expect(onSubmit).not.toHaveBeenCalled()
  })

  it.each(scopes)('cancel button calls onCancel (%s)', (scopeType) => {
    const onCancel = vi.fn()
    render(
      <TemplateCreateForm
        scopeType={scopeType}
        namespace="holos-org-test-org"
        organization="test-org"
        projectName={scopeType === 'project' ? 'test-project' : undefined}
        canWrite
        onSubmit={vi.fn()}
        onCancel={onCancel}
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: /cancel/i }))
    expect(onCancel).toHaveBeenCalledTimes(1)
  })

  it.each(scopes)(
    'selecting an example populates all four fields (%s)',
    async (scopeType) => {
      render(
        <TemplateCreateForm
          scopeType={scopeType}
          namespace="holos-org-test-org"
          organization="test-org"
          projectName={scopeType === 'project' ? 'test-project' : undefined}
          canWrite
          onSubmit={vi.fn()}
          onCancel={vi.fn()}
        />,
      )
      fireEvent.click(screen.getByRole('combobox', { name: /load example/i }))
      const item = await screen.findByText(EXAMPLE_HTTPROUTE.displayName)
      fireEvent.click(item)

      await waitFor(() => {
        expect(
          (screen.getByLabelText(/display name/i) as HTMLInputElement).value,
        ).toBe(EXAMPLE_HTTPROUTE.displayName)
      })
      expect((screen.getByLabelText(/name slug/i) as HTMLInputElement).value).toBe(
        EXAMPLE_HTTPROUTE.name,
      )
      expect(
        (screen.getByLabelText(/description/i) as HTMLInputElement).value,
      ).toBe(EXAMPLE_HTTPROUTE.description)
      expect(
        (screen.getByRole('textbox', { name: /cue template/i }) as HTMLTextAreaElement).value,
      ).toBe(EXAMPLE_HTTPROUTE.cueTemplate)
    },
  )

  it.each(scopes)(
    'surface-level error from onSubmit renders an alert (%s)',
    async (scopeType) => {
      const onSubmit = vi.fn().mockRejectedValue(new Error('server error'))
      render(
        <TemplateCreateForm
          scopeType={scopeType}
          namespace="holos-org-test-org"
          organization="test-org"
          projectName={scopeType === 'project' ? 'test-project' : undefined}
          canWrite
          onSubmit={onSubmit}
          onCancel={vi.fn()}
        />,
      )
      fireEvent.change(screen.getByLabelText(/display name/i), {
        target: { value: 'My Template' },
      })
      const label = scopeType === 'project' ? /create template/i : /^create$/i
      fireEvent.click(screen.getByRole('button', { name: label }))
      await waitFor(() => {
        expect(screen.getByText(/server error/i)).toBeInTheDocument()
      })
    },
  )
})

describe('TemplateCreateForm — organization scope', () => {
  it('renders Enabled switch defaulting to unchecked', () => {
    render(
      <TemplateCreateForm
        scopeType="organization"
        namespace="holos-org-test-org"
        organization="test-org"
        canWrite
        onSubmit={vi.fn()}
        onCancel={vi.fn()}
      />,
    )
    const toggle = screen.getByRole('switch', { name: /enabled/i })
    expect(toggle).toHaveAttribute('data-state', 'unchecked')
  })

  it('renders the platform-template CUE info tooltip next to the example picker', () => {
    render(
      <TemplateCreateForm
        scopeType="organization"
        namespace="holos-org-test-org"
        organization="test-org"
        canWrite
        onSubmit={vi.fn()}
        onCancel={vi.fn()}
      />,
    )
    expect(
      screen.getByText(/platform templates are unified with project deployment templates/i),
    ).toBeInTheDocument()
  })

  it('enabled label reads "Enabled (apply to projects in this organization)"', () => {
    render(
      <TemplateCreateForm
        scopeType="organization"
        namespace="holos-org-test-org"
        organization="test-org"
        canWrite
        onSubmit={vi.fn()}
        onCancel={vi.fn()}
      />,
    )
    expect(
      screen.getByText(/Enabled \(apply to projects in this organization\)/i),
    ).toBeInTheDocument()
  })

  it('submit label is "Create"', () => {
    render(
      <TemplateCreateForm
        scopeType="organization"
        namespace="holos-org-test-org"
        organization="test-org"
        canWrite
        onSubmit={vi.fn()}
        onCancel={vi.fn()}
      />,
    )
    expect(screen.getByRole('button', { name: /^create$/i })).toBeInTheDocument()
  })

  it('passes enabled=false by default in onSubmit payload', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    render(
      <TemplateCreateForm
        scopeType="organization"
        namespace="holos-org-test-org"
        organization="test-org"
        canWrite
        onSubmit={onSubmit}
        onCancel={vi.fn()}
      />,
    )
    fireEvent.change(screen.getByLabelText(/display name/i), {
      target: { value: 'My Template' },
    })
    fireEvent.change(screen.getByLabelText(/description/i), {
      target: { value: 'A description' },
    })
    fireEvent.click(screen.getByRole('button', { name: /^create$/i }))

    await waitFor(() => {
      expect(onSubmit).toHaveBeenCalledWith(
        expect.objectContaining({
          name: 'my-template',
          displayName: 'My Template',
          description: 'A description',
          enabled: false,
        }),
      )
    })
  })

  it('disables form fields when canWrite is false', () => {
    render(
      <TemplateCreateForm
        scopeType="organization"
        namespace="holos-org-test-org"
        organization="test-org"
        canWrite={false}
        onSubmit={vi.fn()}
        onCancel={vi.fn()}
      />,
    )
    expect(screen.getByLabelText(/display name/i)).toBeDisabled()
    expect(screen.getByLabelText(/name slug/i)).toBeDisabled()
    expect(screen.getByLabelText(/description/i)).toBeDisabled()
    expect(screen.getByRole('textbox', { name: /cue template/i })).toBeDisabled()
    expect(screen.getByRole('switch', { name: /enabled/i })).toBeDisabled()
    expect(screen.getByRole('button', { name: /^create$/i })).toBeDisabled()
    expect(screen.getByRole('combobox', { name: /load example/i })).toBeDisabled()
  })
})

describe('TemplateCreateForm — folder scope', () => {
  it('renders Enabled switch defaulting to checked (HOL-789 AC 5)', () => {
    render(
      <TemplateCreateForm
        scopeType="folder"
        namespace="holos-fld-test-folder"
        organization="test-org"
        canWrite
        onSubmit={vi.fn()}
        onCancel={vi.fn()}
      />,
    )
    const toggle = screen.getByRole('switch', { name: /enabled/i })
    expect(toggle).toHaveAttribute('data-state', 'checked')
  })

  it('enabled label has no organization parenthetical', () => {
    render(
      <TemplateCreateForm
        scopeType="folder"
        namespace="holos-fld-test-folder"
        organization="test-org"
        canWrite
        onSubmit={vi.fn()}
        onCancel={vi.fn()}
      />,
    )
    expect(screen.getByText(/^Enabled$/)).toBeInTheDocument()
    expect(
      screen.queryByText(/apply to projects in this organization/i),
    ).not.toBeInTheDocument()
  })

  it('renders the TemplatePolicyBinding tooltip copy next to the Enabled toggle', () => {
    render(
      <TemplateCreateForm
        scopeType="folder"
        namespace="holos-fld-test-folder"
        organization="test-org"
        canWrite
        onSubmit={vi.fn()}
        onCancel={vi.fn()}
      />,
    )
    const expected =
      'Unified with resources bound to this Template by Policy when enabled. See TemplatePolicyBinding.'
    const node = screen.getByText((_content, element) => {
      if (!element || element.tagName !== 'P') return false
      const text = element.textContent?.replace(/\s+/g, ' ').trim()
      return text === expected
    })
    expect(node).toBeInTheDocument()
  })

  it('passes enabled=true by default in onSubmit payload', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    render(
      <TemplateCreateForm
        scopeType="folder"
        namespace="holos-fld-test-folder"
        organization="test-org"
        canWrite
        onSubmit={onSubmit}
        onCancel={vi.fn()}
      />,
    )
    fireEvent.change(screen.getByLabelText(/display name/i), {
      target: { value: 'My Template' },
    })
    fireEvent.click(screen.getByRole('button', { name: /^create$/i }))

    await waitFor(() => {
      expect(onSubmit).toHaveBeenCalledWith(
        expect.objectContaining({ enabled: true }),
      )
    })
  })

  it('carries enabled=false when the user flips the toggle off', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    render(
      <TemplateCreateForm
        scopeType="folder"
        namespace="holos-fld-test-folder"
        organization="test-org"
        canWrite
        onSubmit={onSubmit}
        onCancel={vi.fn()}
      />,
    )
    fireEvent.change(screen.getByLabelText(/display name/i), {
      target: { value: 'My Template' },
    })
    fireEvent.click(screen.getByRole('switch', { name: /enabled/i }))
    fireEvent.click(screen.getByRole('button', { name: /^create$/i }))

    await waitFor(() => {
      expect(onSubmit).toHaveBeenCalledWith(
        expect.objectContaining({ enabled: false }),
      )
    })
  })

  // HOL-555 removed the Mandatory proto field; HOL-558 shifted the concept to
  // TemplatePolicy REQUIRE rules. The form must not carry mandatory.
  it('does not render a Mandatory toggle', () => {
    render(
      <TemplateCreateForm
        scopeType="folder"
        namespace="holos-fld-test-folder"
        organization="test-org"
        canWrite
        onSubmit={vi.fn()}
        onCancel={vi.fn()}
      />,
    )
    expect(screen.queryByRole('switch', { name: /mandatory/i })).not.toBeInTheDocument()
  })

  it('does not include mandatory in onSubmit payload', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    render(
      <TemplateCreateForm
        scopeType="folder"
        namespace="holos-fld-test-folder"
        organization="test-org"
        canWrite
        onSubmit={onSubmit}
        onCancel={vi.fn()}
      />,
    )
    fireEvent.change(screen.getByLabelText(/display name/i), {
      target: { value: 'My Template' },
    })
    fireEvent.click(screen.getByRole('button', { name: /^create$/i }))

    await waitFor(() => {
      expect(onSubmit).toHaveBeenCalled()
    })
    expect(onSubmit.mock.calls[0][0]).not.toHaveProperty('mandatory')
  })
})

describe('TemplateCreateForm — project scope', () => {
  it('renders the Create Template submit button', () => {
    render(
      <TemplateCreateForm
        scopeType="project"
        namespace="holos-prj-test-project"
        organization="test-org"
        projectName="test-project"
        canWrite
        onSubmit={vi.fn()}
        onCancel={vi.fn()}
      />,
    )
    expect(
      screen.getByRole('button', { name: /create template/i }),
    ).toBeInTheDocument()
  })

  it('does not render the Enabled switch', () => {
    render(
      <TemplateCreateForm
        scopeType="project"
        namespace="holos-prj-test-project"
        organization="test-org"
        projectName="test-project"
        canWrite
        onSubmit={vi.fn()}
        onCancel={vi.fn()}
      />,
    )
    expect(screen.queryByRole('switch', { name: /enabled/i })).not.toBeInTheDocument()
  })

  it('CUE template ships with a default scaffold', () => {
    render(
      <TemplateCreateForm
        scopeType="project"
        namespace="holos-prj-test-project"
        organization="test-org"
        projectName="test-project"
        canWrite
        onSubmit={vi.fn()}
        onCancel={vi.fn()}
      />,
    )
    const cueEditor = screen.getByRole('textbox', {
      name: /cue template/i,
    }) as HTMLTextAreaElement
    expect(cueEditor.value).toContain('projectResources')
  })

  it('renders Preview toggle button', () => {
    render(
      <TemplateCreateForm
        scopeType="project"
        namespace="holos-prj-test-project"
        organization="test-org"
        projectName="test-project"
        canWrite
        onSubmit={vi.fn()}
        onCancel={vi.fn()}
      />,
    )
    expect(screen.getByRole('button', { name: /preview/i })).toBeInTheDocument()
  })

  it('shows rendered JSON when preview is toggled and data is available', async () => {
    setupQueryMocks({ renderData: { renderedJson: '{"apiVersion":"apps/v1"}' } })
    render(
      <TemplateCreateForm
        scopeType="project"
        namespace="holos-prj-test-project"
        organization="test-org"
        projectName="test-project"
        canWrite
        onSubmit={vi.fn()}
        onCancel={vi.fn()}
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: /preview/i }))
    await waitFor(() => {
      expect(screen.getByText(/"apiVersion":"apps\/v1"/)).toBeInTheDocument()
    })
  })

  it('useRenderTemplate receives the project-only input', () => {
    render(
      <TemplateCreateForm
        scopeType="project"
        namespace="holos-prj-test-project"
        organization="test-org"
        projectName="test-project"
        canWrite
        onSubmit={vi.fn()}
        onCancel={vi.fn()}
      />,
    )
    const calls = (useRenderTemplate as Mock).mock.calls
    const projectInput = calls[0][2]
    expect(projectInput).toContain('input:')
    expect(projectInput).not.toContain('project:')
    expect(projectInput).not.toContain('namespace:')
  })

  it('does not render linked platform templates section (HOL-907)', () => {
    render(
      <TemplateCreateForm
        scopeType="project"
        namespace="holos-prj-test-project"
        organization="test-org"
        projectName="test-project"
        canWrite
        onSubmit={vi.fn()}
        onCancel={vi.fn()}
      />,
    )
    expect(screen.queryByText(/linked platform templates/i)).not.toBeInTheDocument()
  })

  it('submit payload does not contain linkedTemplates (HOL-907)', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    render(
      <TemplateCreateForm
        scopeType="project"
        namespace="holos-prj-test-project"
        organization="test-org"
        projectName="test-project"
        canWrite
        onSubmit={onSubmit}
        onCancel={vi.fn()}
      />,
    )
    fireEvent.change(screen.getByLabelText(/display name/i), {
      target: { value: 'My Template' },
    })
    fireEvent.click(screen.getByRole('button', { name: /create template/i }))

    await waitFor(() => {
      expect(onSubmit).toHaveBeenCalled()
    })
    expect(onSubmit.mock.calls[0][0]).not.toHaveProperty('linkedTemplates')
  })

  describe('per-collection preview sections', () => {
    it('shows both platform and project JSON sections when present', () => {
      setupQueryMocks({
        renderData: {
          renderedJson: '{"unified":"data"}',
          platformResourcesJson: '{"kind":"ReferenceGrant"}',
          projectResourcesJson: '{"kind":"Deployment"}',
        },
      })
      render(
        <TemplateCreateForm
          scopeType="project"
          namespace="holos-prj-test-project"
          organization="test-org"
          projectName="test-project"
          canWrite
          onSubmit={vi.fn()}
          onCancel={vi.fn()}
        />,
      )
      fireEvent.click(screen.getByRole('button', { name: /preview/i }))

      expect(screen.getByText('Platform Resources')).toBeInTheDocument()
      expect(screen.getByText('Project Resources')).toBeInTheDocument()
      expect(screen.getByLabelText('Platform Resources JSON')).toHaveTextContent('ReferenceGrant')
      expect(screen.getByLabelText('Project Resources JSON')).toHaveTextContent('Deployment')
    })

    it('falls back to unified renderedJson when no per-collection fields', () => {
      setupQueryMocks({ renderData: { renderedJson: '{"apiVersion":"apps/v1"}' } })
      render(
        <TemplateCreateForm
          scopeType="project"
          namespace="holos-prj-test-project"
          organization="test-org"
          projectName="test-project"
          canWrite
          onSubmit={vi.fn()}
          onCancel={vi.fn()}
        />,
      )
      fireEvent.click(screen.getByRole('button', { name: /preview/i }))

      expect(screen.queryByText('Platform Resources')).not.toBeInTheDocument()
      expect(screen.queryByText('Project Resources')).not.toBeInTheDocument()
      expect(screen.getByText(/"apiVersion":"apps\/v1"/)).toBeInTheDocument()
    })

    it('shows platform empty-state message when platform JSON is empty', () => {
      setupQueryMocks({
        renderData: {
          renderedJson: '{"unified":"data"}',
          platformResourcesJson: '',
          projectResourcesJson: '{"kind":"Deployment"}',
        },
      })
      render(
        <TemplateCreateForm
          scopeType="project"
          namespace="holos-prj-test-project"
          organization="test-org"
          projectName="test-project"
          canWrite
          onSubmit={vi.fn()}
          onCancel={vi.fn()}
        />,
      )
      fireEvent.click(screen.getByRole('button', { name: /preview/i }))

      expect(screen.getByText('Platform Resources')).toBeInTheDocument()
      expect(
        screen.getByText('No platform resources rendered by this template.'),
      ).toBeInTheDocument()
      expect(screen.getByLabelText('Project Resources JSON')).toHaveTextContent('Deployment')
    })
  })
})

import { render, screen, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

vi.mock('@/hooks/use-debounced-value', () => ({
  useDebouncedValue: vi.fn((value: unknown) => value),
}))

vi.mock('@/queries/templates', () => ({
  useRenderTemplate: vi.fn(),
}))

import { CueTemplateEditor } from './cue-template-editor'
import { useRenderTemplate } from '@/queries/templates'
import { TemplateScope } from '@/lib/scope-shim'
import { create } from '@bufbuild/protobuf'

// testScope is a placeholder scope used in tests.
const testScope = { scope: TemplateScope.PROJECT, scopeName: 'test-project' } as unknown as ReturnType<typeof create>

describe('CueTemplateEditor', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    ;(useRenderTemplate as Mock).mockReturnValue({ data: undefined, error: null, isFetching: false })
  })

  it('renders editor tab with textarea', () => {
    render(
      <CueTemplateEditor
        cueTemplate="// template content"
        onChange={vi.fn()}
        scope={testScope}
      />
    )
    const textarea = screen.getByRole('textbox', { name: /cue template/i })
    expect(textarea).toBeInTheDocument()
    expect(textarea).toHaveValue('// template content')
  })

  it('calls onChange when textarea is edited', () => {
    const onChange = vi.fn()
    render(
      <CueTemplateEditor
        cueTemplate="initial"
        onChange={onChange}
        scope={testScope}
      />
    )
    const textarea = screen.getByRole('textbox', { name: /cue template/i })
    fireEvent.change(textarea, { target: { value: 'updated content' } })
    expect(onChange).toHaveBeenCalledWith('updated content')
  })

  it('disables textarea when readOnly is true', () => {
    render(
      <CueTemplateEditor
        cueTemplate="content"
        onChange={vi.fn()}
        readOnly={true}
        scope={testScope}
      />
    )
    const textarea = screen.getByRole('textbox', { name: /cue template/i })
    expect(textarea).toHaveAttribute('readonly')
  })

  it('hides Save button when readOnly is true', () => {
    render(
      <CueTemplateEditor
        cueTemplate="content"
        onChange={vi.fn()}
        readOnly={true}
        onSave={vi.fn()}
        scope={testScope}
      />
    )
    expect(screen.queryByRole('button', { name: /save/i })).not.toBeInTheDocument()
  })

  it('shows Save button when readOnly is false and onSave is provided', () => {
    render(
      <CueTemplateEditor
        cueTemplate="content"
        onChange={vi.fn()}
        readOnly={false}
        onSave={vi.fn()}
        scope={testScope}
      />
    )
    expect(screen.getByRole('button', { name: /save/i })).toBeInTheDocument()
  })

  it('calls onSave when Save button is clicked', () => {
    const onSave = vi.fn().mockResolvedValue(undefined)
    render(
      <CueTemplateEditor
        cueTemplate="content"
        onChange={vi.fn()}
        readOnly={false}
        onSave={onSave}
        scope={testScope}
      />
    )
    fireEvent.click(screen.getByRole('button', { name: /save/i }))
    expect(onSave).toHaveBeenCalledTimes(1)
  })

  it('renders preview tab with platform input, project input, and rendered YAML sections', async () => {
    const user = userEvent.setup()
    ;(useRenderTemplate as Mock).mockReturnValue({
      data: { renderedYaml: 'apiVersion: v1\nkind: ReferenceGrant', renderedJson: '' },
      error: null,
      isFetching: false,
    })
    render(
      <CueTemplateEditor
        cueTemplate="content"
        onChange={vi.fn()}
        defaultPlatformInput="platform: {}"
        defaultProjectInput="input: {}"
        scope={testScope}
      />
    )

    // Switch to preview tab
    await user.click(screen.getByRole('tab', { name: /preview/i }))

    expect(screen.getByRole('textbox', { name: /platform input/i })).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: /project input/i })).toBeInTheDocument()
    expect(screen.getByLabelText('Rendered YAML')).toBeInTheDocument()
    expect(screen.getByLabelText('Rendered YAML')).toHaveTextContent('ReferenceGrant')
  })

  it('shows render error in preview tab', async () => {
    const user = userEvent.setup()
    ;(useRenderTemplate as Mock).mockReturnValue({
      data: undefined,
      error: new Error('CUE evaluation failed'),
      isFetching: false,
    })
    render(
      <CueTemplateEditor
        cueTemplate="content"
        onChange={vi.fn()}
        scope={testScope}
      />
    )

    await user.click(screen.getByRole('tab', { name: /preview/i }))

    expect(screen.getByLabelText('Preview error')).toHaveTextContent('CUE evaluation failed')
  })

  it('shows render status indicator: rendering state', async () => {
    const user = userEvent.setup()
    ;(useRenderTemplate as Mock).mockReturnValue({
      data: undefined,
      error: null,
      isFetching: true,
    })
    render(
      <CueTemplateEditor
        cueTemplate="content"
        onChange={vi.fn()}
        scope={testScope}
      />
    )
    await user.click(screen.getByRole('tab', { name: /preview/i }))
    expect(screen.getByLabelText('Render status: rendering')).toBeInTheDocument()
  })

  it('shows render status indicator: fresh state', async () => {
    const user = userEvent.setup()
    ;(useRenderTemplate as Mock).mockReturnValue({
      data: { renderedYaml: '', renderedJson: '' },
      error: null,
      isFetching: false,
    })
    render(
      <CueTemplateEditor
        cueTemplate="content"
        onChange={vi.fn()}
        scope={testScope}
      />
    )
    await user.click(screen.getByRole('tab', { name: /preview/i }))
    expect(screen.getByLabelText('Render status: fresh')).toBeInTheDocument()
  })

  describe('per-collection resource display', () => {
    it('renders both platform and project sections when both are present', async () => {
      const user = userEvent.setup()
      ;(useRenderTemplate as Mock).mockReturnValue({
        data: {
          renderedYaml: 'all-resources',
          renderedJson: '',
          platformResourcesYaml: 'apiVersion: v1\nkind: ReferenceGrant',
          platformResourcesJson: '',
          projectResourcesYaml: 'apiVersion: apps/v1\nkind: Deployment',
          projectResourcesJson: '',
        },
        error: null,
        isFetching: false,
      })
      render(
        <CueTemplateEditor
          cueTemplate="content"
          onChange={vi.fn()}
          scope={testScope}
        />
      )
      await user.click(screen.getByRole('tab', { name: /preview/i }))

      // Both labeled sections should be present
      expect(screen.getByText('Platform Resources')).toBeInTheDocument()
      expect(screen.getByText('Project Resources')).toBeInTheDocument()
      expect(screen.getByLabelText('Platform Resources YAML')).toHaveTextContent('ReferenceGrant')
      expect(screen.getByLabelText('Project Resources YAML')).toHaveTextContent('Deployment')
      // Unified renderedYaml should NOT be shown
      expect(screen.queryByText('all-resources')).not.toBeInTheDocument()
    })

    it('shows empty-state message when platform resources are empty but project resources exist', async () => {
      const user = userEvent.setup()
      ;(useRenderTemplate as Mock).mockReturnValue({
        data: {
          renderedYaml: 'unified-yaml',
          renderedJson: '',
          platformResourcesYaml: '',
          platformResourcesJson: '',
          projectResourcesYaml: 'apiVersion: apps/v1\nkind: Deployment',
          projectResourcesJson: '',
        },
        error: null,
        isFetching: false,
      })
      render(
        <CueTemplateEditor
          cueTemplate="content"
          onChange={vi.fn()}
          scope={testScope}
        />
      )
      await user.click(screen.getByRole('tab', { name: /preview/i }))

      // Both headings should be shown
      expect(screen.getByText('Platform Resources')).toBeInTheDocument()
      expect(screen.getByText('Project Resources')).toBeInTheDocument()
      // Empty-state message replaces the platform YAML block
      expect(screen.getByText('No platform resources rendered by this template.')).toBeInTheDocument()
      // Project resources should be displayed
      expect(screen.getByLabelText('Project Resources YAML')).toHaveTextContent('Deployment')
    })

    it('shows both Platform Resources and Project Resources headings when only project resources exist', async () => {
      const user = userEvent.setup()
      ;(useRenderTemplate as Mock).mockReturnValue({
        data: {
          renderedYaml: '',
          renderedJson: '',
          platformResourcesYaml: '',
          platformResourcesJson: '',
          projectResourcesYaml: 'apiVersion: v1\nkind: ConfigMap',
          projectResourcesJson: '',
        },
        error: null,
        isFetching: false,
      })
      render(
        <CueTemplateEditor
          cueTemplate="content"
          onChange={vi.fn()}
          scope={testScope}
        />
      )
      await user.click(screen.getByRole('tab', { name: /preview/i }))

      // Both headings must always be present when hasPerCollectionFields is true
      expect(screen.getByText('Platform Resources')).toBeInTheDocument()
      expect(screen.getByText('Project Resources')).toBeInTheDocument()
      // Empty-state message for platform
      expect(screen.getByText('No platform resources rendered by this template.')).toBeInTheDocument()
      // Project content is shown
      expect(screen.getByLabelText('Project Resources YAML')).toHaveTextContent('ConfigMap')
    })

    it('pretty-prints JSON default inputs in textareas', async () => {
      const user = userEvent.setup()
      ;(useRenderTemplate as Mock).mockReturnValue({
        data: undefined,
        error: null,
        isFetching: false,
      })
      const compactJson = '{"name":"test","replicas":3}'
      render(
        <CueTemplateEditor
          cueTemplate="content"
          onChange={vi.fn()}
          defaultPlatformInput={compactJson}
          defaultProjectInput={compactJson}
          scope={testScope}
        />
      )
      await user.click(screen.getByRole('tab', { name: /preview/i }))

      const expectedPretty = JSON.stringify(JSON.parse(compactJson), null, 2)
      expect(screen.getByRole('textbox', { name: /platform input/i })).toHaveValue(expectedPretty)
      expect(screen.getByRole('textbox', { name: /project input/i })).toHaveValue(expectedPretty)
    })

    it('passes CUE default inputs through unchanged', async () => {
      const user = userEvent.setup()
      ;(useRenderTemplate as Mock).mockReturnValue({
        data: undefined,
        error: null,
        isFetching: false,
      })
      const cueInput = 'name: "test"\nreplicas: 3'
      render(
        <CueTemplateEditor
          cueTemplate="content"
          onChange={vi.fn()}
          defaultPlatformInput={cueInput}
          defaultProjectInput={cueInput}
          scope={testScope}
        />
      )
      await user.click(screen.getByRole('tab', { name: /preview/i }))

      // CUE is not valid JSON, so it should pass through unchanged
      expect(screen.getByRole('textbox', { name: /platform input/i })).toHaveValue(cueInput)
      expect(screen.getByRole('textbox', { name: /project input/i })).toHaveValue(cueInput)
    })

    it('falls back to unified renderedYaml when no per-collection fields are present', async () => {
      const user = userEvent.setup()
      ;(useRenderTemplate as Mock).mockReturnValue({
        data: {
          renderedYaml: 'apiVersion: v1\nkind: Service',
          renderedJson: '',
        },
        error: null,
        isFetching: false,
      })
      render(
        <CueTemplateEditor
          cueTemplate="content"
          onChange={vi.fn()}
          scope={testScope}
        />
      )
      await user.click(screen.getByRole('tab', { name: /preview/i }))

      expect(screen.getByText('Rendered YAML')).toBeInTheDocument()
      expect(screen.getByLabelText('Rendered YAML')).toHaveTextContent('Service')
      expect(screen.queryByText('Platform Resources')).not.toBeInTheDocument()
      expect(screen.queryByText('Project Resources')).not.toBeInTheDocument()
    })
  })
})

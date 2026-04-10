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
import { TemplateScope } from '@/gen/holos/console/v1/templates_pb.js'
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
})

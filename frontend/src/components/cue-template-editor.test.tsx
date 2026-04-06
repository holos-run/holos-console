import { render, screen, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { vi } from 'vitest'
import React from 'react'

vi.mock('@/hooks/use-debounced-value', () => ({
  useDebouncedValue: vi.fn((value: unknown) => value),
}))

import { CueTemplateEditor, type RenderFn } from './cue-template-editor'

// A mock render function that returns no data (no error, no render)
const noOpRenderFn: RenderFn = () => ({
  data: undefined,
  error: null,
  isFetching: false,
})

// A mock render function that returns YAML
function makeRenderFn(yaml: string): RenderFn {
  return () => ({
    data: { renderedYaml: yaml, renderedJson: '' },
    error: null,
    isFetching: false,
  })
}

// A mock render function that returns an error
function makeErrorRenderFn(message: string): RenderFn {
  return () => ({
    data: undefined,
    error: new Error(message),
    isFetching: false,
  })
}

describe('CueTemplateEditor', () => {
  it('renders editor tab with textarea', () => {
    render(
      <CueTemplateEditor
        cueTemplate="// template content"
        onChange={vi.fn()}
        useRenderFn={noOpRenderFn}
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
        useRenderFn={noOpRenderFn}
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
        useRenderFn={noOpRenderFn}
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
        useRenderFn={noOpRenderFn}
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
        useRenderFn={noOpRenderFn}
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
        useRenderFn={noOpRenderFn}
      />
    )
    fireEvent.click(screen.getByRole('button', { name: /save/i }))
    expect(onSave).toHaveBeenCalledTimes(1)
  })

  it('renders preview tab with system input, user input, and rendered YAML sections', async () => {
    const user = userEvent.setup()
    render(
      <CueTemplateEditor
        cueTemplate="content"
        onChange={vi.fn()}
        defaultSystemInput="system: {}"
        defaultUserInput="input: {}"
        useRenderFn={makeRenderFn('apiVersion: v1\nkind: ReferenceGrant')}
      />
    )

    // Switch to preview tab
    await user.click(screen.getByRole('tab', { name: /preview/i }))

    expect(screen.getByRole('textbox', { name: /system input/i })).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: /user input/i })).toBeInTheDocument()
    expect(screen.getByLabelText('Rendered YAML')).toBeInTheDocument()
    expect(screen.getByLabelText('Rendered YAML')).toHaveTextContent('ReferenceGrant')
  })

  it('shows render error in preview tab', async () => {
    const user = userEvent.setup()
    render(
      <CueTemplateEditor
        cueTemplate="content"
        onChange={vi.fn()}
        useRenderFn={makeErrorRenderFn('CUE evaluation failed')}
      />
    )

    await user.click(screen.getByRole('tab', { name: /preview/i }))

    expect(screen.getByLabelText('Preview error')).toHaveTextContent('CUE evaluation failed')
  })

  it('shows render status indicator: rendering state', async () => {
    const user = userEvent.setup()
    const renderingFn: RenderFn = () => ({
      data: undefined,
      error: null,
      isFetching: true,
    })
    render(
      <CueTemplateEditor
        cueTemplate="content"
        onChange={vi.fn()}
        useRenderFn={renderingFn}
      />
    )
    await user.click(screen.getByRole('tab', { name: /preview/i }))
    expect(screen.getByLabelText('Render status: rendering')).toBeInTheDocument()
  })

  it('shows render status indicator: fresh state', async () => {
    const user = userEvent.setup()
    render(
      <CueTemplateEditor
        cueTemplate="content"
        onChange={vi.fn()}
        useRenderFn={makeRenderFn('')}
      />
    )
    await user.click(screen.getByRole('tab', { name: /preview/i }))
    expect(screen.getByLabelText('Render status: fresh')).toBeInTheDocument()
  })
})

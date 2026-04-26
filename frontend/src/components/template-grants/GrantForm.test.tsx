// HOL-1022: Tests for the GrantForm component.
//
// Covers:
// 1. Create-mode render — name, from, to fields present.
// 2. Edit-mode prefill — initial values populate the fields.
// 3. Validation — missing name shows error.
// 4. Validation — missing from namespace shows error.
// 5. Submits with correct values.
// 6. OWNER can submit; VIEWER cannot.

import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { vi } from 'vitest'
import React from 'react'
import { GrantForm } from './GrantForm'

describe('GrantForm (HOL-1022)', () => {
  const baseProps = {
    mode: 'create' as const,
    namespace: 'holos-org-test-org',
    canWrite: true,
    submitLabel: 'Create',
    pendingLabel: 'Creating...',
    onSubmit: vi.fn().mockResolvedValue(undefined),
    onCancel: vi.fn(),
  }

  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders in create mode with all fields', () => {
    render(<GrantForm {...baseProps} />)
    expect(screen.getByLabelText('Grant name')).toBeInTheDocument()
    expect(screen.getByLabelText('From namespace')).toBeInTheDocument()
    expect(screen.getByLabelText('To namespace')).toBeInTheDocument()
    expect(screen.getByLabelText('To name')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /^create$/i })).toBeInTheDocument()
  })

  it('prefills fields in edit mode', () => {
    render(
      <GrantForm
        {...baseProps}
        mode="edit"
        lockName
        initialValues={{
          name: 'allow-project-foo',
          fromNamespace: 'holos-project-foo',
          toNamespace: 'holos-org-test-org',
          toName: 'base-template',
        }}
        submitLabel="Save"
        pendingLabel="Saving..."
      />,
    )
    expect(screen.getByLabelText('Grant name')).toHaveValue('allow-project-foo')
    expect(screen.getByLabelText('From namespace')).toHaveValue('holos-project-foo')
    expect(screen.getByLabelText('To namespace')).toHaveValue('holos-org-test-org')
    expect(screen.getByLabelText('To name')).toHaveValue('base-template')
  })

  it('shows an error when name is empty on submit', async () => {
    render(<GrantForm {...baseProps} />)
    fireEvent.click(screen.getByRole('button', { name: /^create$/i }))
    await waitFor(() => {
      expect(screen.getByTestId('grant-form-error')).toHaveTextContent(
        'Grant name is required.',
      )
    })
    expect(baseProps.onSubmit).not.toHaveBeenCalled()
  })

  it('shows an error when from namespace is empty on submit', async () => {
    render(<GrantForm {...baseProps} />)
    fireEvent.change(screen.getByLabelText('Grant name'), {
      target: { value: 'allow-project-foo' },
    })
    fireEvent.click(screen.getByRole('button', { name: /^create$/i }))
    await waitFor(() => {
      expect(screen.getByTestId('grant-form-error')).toHaveTextContent(
        'From namespace is required.',
      )
    })
    expect(baseProps.onSubmit).not.toHaveBeenCalled()
  })

  it('submits with correct from/to values', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    render(<GrantForm {...baseProps} onSubmit={onSubmit} />)

    fireEvent.change(screen.getByLabelText('Grant name'), {
      target: { value: 'allow-project-foo' },
    })
    fireEvent.change(screen.getByLabelText('From namespace'), {
      target: { value: 'holos-project-foo' },
    })
    fireEvent.change(screen.getByLabelText('To namespace'), {
      target: { value: 'holos-org-test-org' },
    })
    fireEvent.change(screen.getByLabelText('To name'), {
      target: { value: 'base-template' },
    })

    fireEvent.click(screen.getByRole('button', { name: /^create$/i }))

    await waitFor(() => {
      expect(onSubmit).toHaveBeenCalledWith({
        name: 'allow-project-foo',
        from: [
          {
            $typeName: 'holos.console.v1.TemplateGrantFromRef',
            namespace: 'holos-project-foo',
          },
        ],
        to: [
          {
            $typeName: 'holos.console.v1.TemplateGrantToRef',
            namespace: 'holos-org-test-org',
            name: 'base-template',
          },
        ],
      })
    })
  })

  it('submits with empty to array when to fields are blank', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    render(<GrantForm {...baseProps} onSubmit={onSubmit} />)

    fireEvent.change(screen.getByLabelText('Grant name'), {
      target: { value: 'allow-all' },
    })
    fireEvent.change(screen.getByLabelText('From namespace'), {
      target: { value: '*' },
    })

    fireEvent.click(screen.getByRole('button', { name: /^create$/i }))

    await waitFor(() => {
      expect(onSubmit).toHaveBeenCalledWith(
        expect.objectContaining({
          to: [],
        }),
      )
    })
  })

  it('disables submit button when canWrite is false', () => {
    render(<GrantForm {...baseProps} canWrite={false} />)
    expect(screen.getByRole('button', { name: /^create$/i })).toBeDisabled()
  })

  it('calls onCancel when Cancel is clicked', () => {
    const onCancel = vi.fn()
    render(<GrantForm {...baseProps} onCancel={onCancel} />)
    fireEvent.click(screen.getByRole('button', { name: /cancel/i }))
    expect(onCancel).toHaveBeenCalled()
  })
})

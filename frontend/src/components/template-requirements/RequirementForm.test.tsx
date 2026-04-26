// HOL-1021: Unit tests for the shared RequirementForm component.
//
// Tests cover:
// 1. Renders correctly in create mode.
// 2. Renders correctly in edit mode with prefilled values.
// 3. Validates required fields before submit.
// 4. Calls onSubmit with correct values.
// 5. Calls onCancel when Cancel is clicked.
// 6. Disables controls when canWrite is false.

import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { vi } from 'vitest'
import { RequirementForm } from './RequirementForm'

describe('RequirementForm', () => {
  const defaultProps = {
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

  it('renders in create mode with empty fields', () => {
    render(<RequirementForm {...defaultProps} />)
    expect(screen.getByLabelText('Requirement name')).toHaveValue('')
    expect(screen.getByLabelText('Requires name')).toHaveValue('')
    expect(screen.getByRole('button', { name: /create/i })).toBeInTheDocument()
  })

  it('renders in edit mode with prefilled values and locked name', () => {
    const initialValues = {
      name: 'my-req',
      requiresNamespace: 'holos-org-my-org',
      requiresName: 'base-template',
      cascadeDelete: true,
    }
    render(
      <RequirementForm
        {...defaultProps}
        mode="edit"
        initialValues={initialValues}
        lockName
        submitLabel="Save"
        pendingLabel="Saving..."
      />,
    )
    expect(screen.getByLabelText('Requirement name')).toHaveValue('my-req')
    expect(screen.getByLabelText('Requirement name')).toBeDisabled()
    expect(screen.getByLabelText('Requires name')).toHaveValue('base-template')
    expect(screen.getByRole('button', { name: /^save$/i })).toBeInTheDocument()
  })

  it('shows an error when name is empty on submit', async () => {
    render(<RequirementForm {...defaultProps} />)
    fireEvent.click(screen.getByRole('button', { name: /create/i }))
    await waitFor(() => {
      expect(screen.getByTestId('requirement-form-error')).toHaveTextContent(
        /requirement name is required/i,
      )
    })
    expect(defaultProps.onSubmit).not.toHaveBeenCalled()
  })

  it('shows an error when requires fields are empty on submit', async () => {
    render(<RequirementForm {...defaultProps} />)
    fireEvent.change(screen.getByLabelText('Requirement name'), {
      target: { value: 'my-req' },
    })
    fireEvent.click(screen.getByRole('button', { name: /create/i }))
    await waitFor(() => {
      expect(screen.getByTestId('requirement-form-error')).toHaveTextContent(
        /requires namespace and name are required/i,
      )
    })
    expect(defaultProps.onSubmit).not.toHaveBeenCalled()
  })

  it('calls onSubmit with correct values when all fields are filled', async () => {
    render(<RequirementForm {...defaultProps} />)
    fireEvent.change(screen.getByLabelText('Requirement name'), {
      target: { value: 'my-req' },
    })
    fireEvent.change(screen.getByLabelText('Requires namespace'), {
      target: { value: 'holos-org-my-org' },
    })
    fireEvent.change(screen.getByLabelText('Requires name'), {
      target: { value: 'base-template' },
    })
    fireEvent.click(screen.getByRole('button', { name: /create/i }))
    await waitFor(() => {
      expect(defaultProps.onSubmit).toHaveBeenCalledWith({
        name: 'my-req',
        requires: expect.objectContaining({
          namespace: 'holos-org-my-org',
          name: 'base-template',
        }),
        cascadeDelete: true,
      })
    })
  })

  it('calls onCancel when Cancel is clicked', () => {
    render(<RequirementForm {...defaultProps} />)
    fireEvent.click(screen.getByRole('button', { name: /cancel/i }))
    expect(defaultProps.onCancel).toHaveBeenCalledTimes(1)
  })

  it('disables all controls when canWrite is false', () => {
    render(<RequirementForm {...defaultProps} canWrite={false} />)
    expect(screen.getByLabelText('Requirement name')).toBeDisabled()
    expect(screen.getByLabelText('Requires name')).toBeDisabled()
    expect(screen.getByRole('button', { name: /create/i })).toBeDisabled()
  })
})

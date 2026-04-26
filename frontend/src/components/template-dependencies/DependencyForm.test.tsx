// HOL-1020: Unit tests for the shared DependencyForm component.
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
import { DependencyForm } from './DependencyForm'

describe('DependencyForm', () => {
  const defaultProps = {
    mode: 'create' as const,
    namespace: 'holos-project-my-project',
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
    render(<DependencyForm {...defaultProps} />)
    expect(screen.getByLabelText('Dependency name')).toHaveValue('')
    expect(screen.getByLabelText('Dependent name')).toHaveValue('')
    expect(screen.getByLabelText('Requires name')).toHaveValue('')
    expect(screen.getByRole('button', { name: /create/i })).toBeInTheDocument()
  })

  it('renders in edit mode with prefilled values', () => {
    const initialValues = {
      name: 'my-dep',
      dependentNamespace: 'holos-project-my-project',
      dependentName: 'my-template',
      requiresNamespace: 'holos-org-my-org',
      requiresName: 'base-template',
      cascadeDelete: true,
    }
    render(
      <DependencyForm
        {...defaultProps}
        mode="edit"
        initialValues={initialValues}
        lockName
        submitLabel="Save"
        pendingLabel="Saving..."
      />,
    )
    expect(screen.getByLabelText('Dependency name')).toHaveValue('my-dep')
    expect(screen.getByLabelText('Dependency name')).toBeDisabled()
    expect(screen.getByLabelText('Dependent name')).toHaveValue('my-template')
    expect(screen.getByLabelText('Requires name')).toHaveValue('base-template')
    expect(screen.getByRole('button', { name: /^save$/i })).toBeInTheDocument()
  })

  it('shows an error when name is empty on submit', async () => {
    render(<DependencyForm {...defaultProps} />)
    fireEvent.click(screen.getByRole('button', { name: /create/i }))
    await waitFor(() => {
      expect(screen.getByTestId('dependency-form-error')).toHaveTextContent(
        /dependency name is required/i,
      )
    })
    expect(defaultProps.onSubmit).not.toHaveBeenCalled()
  })

  it('shows an error when dependent fields are empty on submit', async () => {
    render(<DependencyForm {...defaultProps} />)
    // Fill name but not dependent
    fireEvent.change(screen.getByLabelText('Dependency name'), {
      target: { value: 'my-dep' },
    })
    fireEvent.click(screen.getByRole('button', { name: /create/i }))
    await waitFor(() => {
      expect(screen.getByTestId('dependency-form-error')).toHaveTextContent(
        /dependent namespace and name are required/i,
      )
    })
    expect(defaultProps.onSubmit).not.toHaveBeenCalled()
  })

  it('shows an error when requires fields are empty on submit', async () => {
    render(<DependencyForm {...defaultProps} />)
    fireEvent.change(screen.getByLabelText('Dependency name'), {
      target: { value: 'my-dep' },
    })
    fireEvent.change(screen.getByLabelText('Dependent namespace'), {
      target: { value: 'holos-project-my-project' },
    })
    fireEvent.change(screen.getByLabelText('Dependent name'), {
      target: { value: 'my-template' },
    })
    fireEvent.click(screen.getByRole('button', { name: /create/i }))
    await waitFor(() => {
      expect(screen.getByTestId('dependency-form-error')).toHaveTextContent(
        /requires namespace and name are required/i,
      )
    })
    expect(defaultProps.onSubmit).not.toHaveBeenCalled()
  })

  it('calls onSubmit with correct values when all fields are filled', async () => {
    render(<DependencyForm {...defaultProps} />)
    // Fill dependent and requires first; auto-derive will set the name.
    // Then override the name field last to set a custom slug.
    fireEvent.change(screen.getByLabelText('Dependent namespace'), {
      target: { value: 'holos-project-my-project' },
    })
    fireEvent.change(screen.getByLabelText('Dependent name'), {
      target: { value: 'my-template' },
    })
    fireEvent.change(screen.getByLabelText('Requires namespace'), {
      target: { value: 'holos-org-my-org' },
    })
    fireEvent.change(screen.getByLabelText('Requires name'), {
      target: { value: 'base-template' },
    })
    // Override the auto-derived name with a custom one.
    fireEvent.change(screen.getByLabelText('Dependency name'), {
      target: { value: 'my-dep' },
    })
    fireEvent.click(screen.getByRole('button', { name: /create/i }))
    await waitFor(() => {
      expect(defaultProps.onSubmit).toHaveBeenCalledWith({
        name: 'my-dep',
        dependent: expect.objectContaining({
          namespace: 'holos-project-my-project',
          name: 'my-template',
        }),
        requires: expect.objectContaining({
          namespace: 'holos-org-my-org',
          name: 'base-template',
        }),
        cascadeDelete: true,
      })
    })
  })

  it('calls onCancel when Cancel is clicked', () => {
    render(<DependencyForm {...defaultProps} />)
    fireEvent.click(screen.getByRole('button', { name: /cancel/i }))
    expect(defaultProps.onCancel).toHaveBeenCalledTimes(1)
  })

  it('disables all controls when canWrite is false', () => {
    render(<DependencyForm {...defaultProps} canWrite={false} />)
    expect(screen.getByLabelText('Dependency name')).toBeDisabled()
    expect(screen.getByLabelText('Dependent name')).toBeDisabled()
    expect(screen.getByLabelText('Requires name')).toBeDisabled()
    expect(screen.getByRole('button', { name: /create/i })).toBeDisabled()
  })

  it('auto-derives the name from dependent and requires names in create mode', () => {
    render(<DependencyForm {...defaultProps} />)
    fireEvent.change(screen.getByLabelText('Dependent name'), {
      target: { value: 'my-template' },
    })
    fireEvent.change(screen.getByLabelText('Requires name'), {
      target: { value: 'base-template' },
    })
    expect(screen.getByLabelText('Dependency name')).toHaveValue(
      'my-template-depends-on-base-template',
    )
  })
})

import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { vi } from 'vitest'
import React from 'react'
import { ConfirmDeleteDialog } from './confirm-delete-dialog'

describe('ConfirmDeleteDialog', () => {
  const defaultProps = {
    open: true,
    onOpenChange: vi.fn(),
    name: 'my-secret',
    namespace: 'project-billing',
    onConfirm: vi.fn().mockResolvedValue(undefined),
  }

  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders dialog with resource name and namespace', () => {
    render(<ConfirmDeleteDialog {...defaultProps} />)
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    expect(screen.getByText('my-secret')).toBeInTheDocument()
    expect(screen.getByText('project-billing')).toBeInTheDocument()
  })

  it('renders displayName when provided', () => {
    render(
      <ConfirmDeleteDialog
        {...defaultProps}
        displayName="My Secret"
      />,
    )
    expect(screen.getByText('My Secret')).toBeInTheDocument()
  })

  it('falls back to name when displayName is absent', () => {
    render(<ConfirmDeleteDialog {...defaultProps} />)
    // name "my-secret" should be displayed as the bold label
    expect(screen.getByText('my-secret')).toBeInTheDocument()
  })

  it('does not render when open is false', () => {
    render(<ConfirmDeleteDialog {...defaultProps} open={false} />)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('calls onConfirm when Delete button is clicked', async () => {
    const onConfirm = vi.fn().mockResolvedValue(undefined)
    render(<ConfirmDeleteDialog {...defaultProps} onConfirm={onConfirm} />)
    fireEvent.click(screen.getByRole('button', { name: /^delete$/i }))
    await waitFor(() => expect(onConfirm).toHaveBeenCalledOnce())
  })

  it('calls onOpenChange(false) when Cancel is clicked', () => {
    const onOpenChange = vi.fn()
    render(
      <ConfirmDeleteDialog {...defaultProps} onOpenChange={onOpenChange} />,
    )
    fireEvent.click(screen.getByRole('button', { name: /cancel/i }))
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })

  it('disables buttons while isDeleting is true', () => {
    render(<ConfirmDeleteDialog {...defaultProps} isDeleting={true} />)
    expect(screen.getByRole('button', { name: /deleting/i })).toBeDisabled()
    expect(screen.getByRole('button', { name: /cancel/i })).toBeDisabled()
  })

  it('renders Delete text when isDeleting is false', () => {
    render(<ConfirmDeleteDialog {...defaultProps} isDeleting={false} />)
    expect(
      screen.getByRole('button', { name: /^delete$/i }),
    ).not.toBeDisabled()
  })

  it('renders error alert when error prop is set', () => {
    render(
      <ConfirmDeleteDialog
        {...defaultProps}
        error={new Error('delete failed')}
      />,
    )
    expect(screen.getByText('delete failed')).toBeInTheDocument()
  })

  it('does not render error alert when error is null', () => {
    render(<ConfirmDeleteDialog {...defaultProps} error={null} />)
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })
})

/**
 * Tests for CascadeDeleteToggle (HOL-963).
 *
 * Covers:
 *  - Default value is true (cascade on)
 *  - Toggle interaction fires onChange
 *  - Disabled state prevents interaction
 *  - Label text updates based on value
 */

import { render, screen, fireEvent } from '@testing-library/react'
import { vi } from 'vitest'
import React from 'react'
import { CascadeDeleteToggle } from './CascadeDeleteToggle'

describe('CascadeDeleteToggle', () => {
  it('renders with cascade on by default', () => {
    render(<CascadeDeleteToggle />)
    const toggle = screen.getByTestId('cascade-delete-toggle')
    expect(toggle).toBeInTheDocument()
    // Switch defaults to checked=true
    expect(toggle).toHaveAttribute('aria-checked', 'true')
  })

  it('renders unchecked when value is false', () => {
    render(<CascadeDeleteToggle value={false} />)
    const toggle = screen.getByTestId('cascade-delete-toggle')
    expect(toggle).toHaveAttribute('aria-checked', 'false')
  })

  it('calls onChange when toggled', () => {
    const onChange = vi.fn()
    render(<CascadeDeleteToggle value={true} onChange={onChange} />)
    const toggle = screen.getByTestId('cascade-delete-toggle')
    fireEvent.click(toggle)
    expect(onChange).toHaveBeenCalledWith(false)
  })

  it('calls onChange with true when toggled from false to true', () => {
    const onChange = vi.fn()
    render(<CascadeDeleteToggle value={false} onChange={onChange} />)
    const toggle = screen.getByTestId('cascade-delete-toggle')
    fireEvent.click(toggle)
    expect(onChange).toHaveBeenCalledWith(true)
  })

  it('does not call onChange when disabled', () => {
    const onChange = vi.fn()
    render(<CascadeDeleteToggle value={true} onChange={onChange} disabled />)
    const toggle = screen.getByTestId('cascade-delete-toggle')
    expect(toggle).toBeDisabled()
    fireEvent.click(toggle)
    expect(onChange).not.toHaveBeenCalled()
  })

  it('shows "cascade" description text when value is true', () => {
    render(<CascadeDeleteToggle value={true} />)
    expect(screen.getByText(/Deleting this deployment will also remove/i)).toBeInTheDocument()
  })

  it('shows "leave in place" description text when value is false', () => {
    render(<CascadeDeleteToggle value={false} />)
    expect(screen.getByText(/Deleting this deployment will leave/i)).toBeInTheDocument()
  })

  it('renders the Cascade delete label', () => {
    render(<CascadeDeleteToggle />)
    expect(screen.getByText(/Cascade delete/i)).toBeInTheDocument()
  })
})

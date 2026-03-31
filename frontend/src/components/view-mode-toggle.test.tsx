import { render, screen, fireEvent } from '@testing-library/react'
import { vi } from 'vitest'
import { ViewModeToggle } from './view-mode-toggle'

describe('ViewModeToggle', () => {
  it('renders both option buttons', () => {
    const onValueChange = vi.fn()
    render(
      <ViewModeToggle
        value="data"
        onValueChange={onValueChange}
        options={[
          { value: 'data', label: 'Data' },
          { value: 'resource', label: 'Resource' },
        ]}
      />,
    )
    expect(screen.getByRole('button', { name: 'Data' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Resource' })).toBeInTheDocument()
  })

  it('calls onValueChange when a non-active option is clicked', () => {
    const onValueChange = vi.fn()
    render(
      <ViewModeToggle
        value="data"
        onValueChange={onValueChange}
        options={[
          { value: 'data', label: 'Data' },
          { value: 'resource', label: 'Resource' },
        ]}
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: 'Resource' }))
    expect(onValueChange).toHaveBeenCalledWith('resource')
  })

  it('calls onValueChange when the active option is re-clicked', () => {
    const onValueChange = vi.fn()
    render(
      <ViewModeToggle
        value="data"
        onValueChange={onValueChange}
        options={[
          { value: 'data', label: 'Data' },
          { value: 'resource', label: 'Resource' },
        ]}
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: 'Data' }))
    expect(onValueChange).toHaveBeenCalledWith('data')
  })

  it('renders Claims/Raw options for profile page toggle', () => {
    const onValueChange = vi.fn()
    render(
      <ViewModeToggle
        value="claims"
        onValueChange={onValueChange}
        options={[
          { value: 'claims', label: 'Claims' },
          { value: 'raw', label: 'Raw' },
        ]}
      />,
    )
    expect(screen.getByRole('button', { name: 'Claims' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Raw' })).toBeInTheDocument()
  })

  it('switching to raw view calls onValueChange with raw', () => {
    const onValueChange = vi.fn()
    render(
      <ViewModeToggle
        value="claims"
        onValueChange={onValueChange}
        options={[
          { value: 'claims', label: 'Claims' },
          { value: 'raw', label: 'Raw' },
        ]}
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: 'Raw' }))
    expect(onValueChange).toHaveBeenCalledWith('raw')
  })
})

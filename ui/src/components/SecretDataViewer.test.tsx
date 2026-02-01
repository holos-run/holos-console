import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { vi } from 'vitest'
import { SecretDataViewer } from './SecretDataViewer'

const encode = (s: string) => new TextEncoder().encode(s)

describe('SecretDataViewer', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders key names as labels', () => {
    const onChange = vi.fn()
    render(
      <SecretDataViewer
        data={{
          username: encode('admin'),
          password: encode('secret123'),
        }}
        onChange={onChange}
      />,
    )

    expect(screen.getByText('username')).toBeInTheDocument()
    expect(screen.getByText('password')).toBeInTheDocument()
  })

  it('hides values by default with masked placeholder', () => {
    const onChange = vi.fn()
    render(
      <SecretDataViewer
        data={{ username: encode('admin') }}
        onChange={onChange}
      />,
    )

    // Value should not be visible
    expect(screen.queryByText('admin')).not.toBeInTheDocument()
    // Masked placeholder should be visible
    expect(screen.getByText(/••••/)).toBeInTheDocument()
  })

  it('clicking Reveal shows the value in a read-only monospace block', () => {
    const onChange = vi.fn()
    render(
      <SecretDataViewer
        data={{ username: encode('admin') }}
        onChange={onChange}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: /reveal/i }))

    expect(screen.getByText('admin')).toBeInTheDocument()
    // Should be in a pre element for monospace display
    const pre = screen.getByText('admin').closest('pre')
    expect(pre).toBeInTheDocument()
  })

  it('clicking Reveal again (now Hide) hides the value', () => {
    const onChange = vi.fn()
    render(
      <SecretDataViewer
        data={{ username: encode('admin') }}
        onChange={onChange}
      />,
    )

    // Reveal
    fireEvent.click(screen.getByRole('button', { name: /reveal/i }))
    expect(screen.getByText('admin')).toBeInTheDocument()

    // Hide
    fireEvent.click(screen.getByRole('button', { name: /hide/i }))
    expect(screen.queryByText('admin')).not.toBeInTheDocument()
  })

  it('clicking Copy calls navigator.clipboard.writeText with decoded value', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.assign(navigator, { clipboard: { writeText } })

    const onChange = vi.fn()
    render(
      <SecretDataViewer
        data={{ username: encode('admin') }}
        onChange={onChange}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: /copy/i }))

    await waitFor(() => {
      expect(writeText).toHaveBeenCalledWith('admin')
    })
  })

  it('clicking Edit shows an editable TextField for that key', () => {
    const onChange = vi.fn()
    render(
      <SecretDataViewer
        data={{ username: encode('admin') }}
        onChange={onChange}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: /^edit$/i }))

    // Should show a text field with the value
    const input = screen.getByDisplayValue('admin')
    expect(input).toBeInTheDocument()
  })

  it('Edit mode shows Save/Cancel; Save calls onChange with updated data', () => {
    const onChange = vi.fn()
    render(
      <SecretDataViewer
        data={{ username: encode('admin') }}
        onChange={onChange}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: /^edit$/i }))

    // Change value
    fireEvent.change(screen.getByDisplayValue('admin'), { target: { value: 'root' } })

    // Save
    fireEvent.click(screen.getByRole('button', { name: /^save$/i }))

    expect(onChange).toHaveBeenCalled()
    const lastCall = onChange.mock.calls[onChange.mock.calls.length - 1][0]
    expect(new TextDecoder().decode(lastCall['username'])).toBe('root')
  })

  it('Edit mode Cancel reverts without calling onChange', () => {
    const onChange = vi.fn()
    render(
      <SecretDataViewer
        data={{ username: encode('admin') }}
        onChange={onChange}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: /^edit$/i }))
    fireEvent.change(screen.getByDisplayValue('admin'), { target: { value: 'root' } })
    fireEvent.click(screen.getByRole('button', { name: /cancel/i }))

    // Should not have called onChange
    expect(onChange).not.toHaveBeenCalled()
    // Should be back to masked view
    expect(screen.queryByDisplayValue('root')).not.toBeInTheDocument()
  })

  it('does not show an Add Key button', () => {
    const onChange = vi.fn()
    render(
      <SecretDataViewer
        data={{ username: encode('admin') }}
        onChange={onChange}
      />,
    )

    expect(screen.queryByRole('button', { name: /add key/i })).not.toBeInTheDocument()
  })
})

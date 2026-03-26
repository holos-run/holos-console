import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { vi } from 'vitest'
import { SecretDataGrid } from './secret-data-grid'

const encode = (s: string) => new TextEncoder().encode(s)

describe('SecretDataGrid', () => {
  it('renders existing key-value pairs in a grid', () => {
    const onChange = vi.fn()
    render(
      <SecretDataGrid
        data={{ username: encode('admin'), password: encode('secret') }}
        onChange={onChange}
      />,
    )

    const keyInputs = screen.getAllByPlaceholderText('key')
    expect(keyInputs).toHaveLength(2)
    const keys = keyInputs.map((el) => (el as HTMLInputElement).value)
    expect(keys).toContain('username')
    expect(keys).toContain('password')
  })

  it('shows one empty row when data is empty', () => {
    const onChange = vi.fn()
    render(<SecretDataGrid data={{}} onChange={onChange} />)

    expect(screen.getAllByPlaceholderText('key')).toHaveLength(1)
    expect(screen.getAllByPlaceholderText('value')).toHaveLength(1)
    expect((screen.getByPlaceholderText('key') as HTMLInputElement).value).toBe('')
  })

  it('Add Row button appends a new empty row', () => {
    const onChange = vi.fn()
    render(
      <SecretDataGrid data={{ token: encode('abc') }} onChange={onChange} />,
    )

    expect(screen.getAllByPlaceholderText('key')).toHaveLength(1)
    fireEvent.click(screen.getByRole('button', { name: /add row/i }))
    expect(screen.getAllByPlaceholderText('key')).toHaveLength(2)
  })

  it('remove button deletes a row', () => {
    const onChange = vi.fn()
    render(
      <SecretDataGrid
        data={{ a: encode('1'), b: encode('2') }}
        onChange={onChange}
      />,
    )

    expect(screen.getAllByPlaceholderText('key')).toHaveLength(2)
    const removeButtons = screen.getAllByLabelText('remove row')
    fireEvent.click(removeButtons[0])
    expect(screen.getAllByPlaceholderText('key')).toHaveLength(1)
  })

  it('removing last row shows one empty row', () => {
    const onChange = vi.fn()
    render(<SecretDataGrid data={{ a: encode('1') }} onChange={onChange} />)

    fireEvent.click(screen.getByLabelText('remove row'))
    expect(screen.getAllByPlaceholderText('key')).toHaveLength(1)
    expect((screen.getByPlaceholderText('key') as HTMLInputElement).value).toBe('')
  })

  it('fires onChange with correct data on key change (trailing newline by default)', () => {
    const onChange = vi.fn()
    render(<SecretDataGrid data={{ old: encode('val') }} onChange={onChange} />)

    fireEvent.change(screen.getByPlaceholderText('key'), { target: { value: 'new' } })

    const lastCall = onChange.mock.calls[onChange.mock.calls.length - 1][0]
    expect(lastCall).toHaveProperty('new')
    expect(new TextDecoder().decode(lastCall['new'])).toBe('val\n')
  })

  it('fires onChange with correct data on value change', () => {
    const onChange = vi.fn()
    render(<SecretDataGrid data={{ token: encode('old') }} onChange={onChange} />)

    fireEvent.change(screen.getByPlaceholderText('value'), { target: { value: 'new' } })

    const lastCall = onChange.mock.calls[onChange.mock.calls.length - 1][0]
    expect(new TextDecoder().decode(lastCall['token'])).toBe('new\n')
  })

  it('does not add trailing newline to empty values', () => {
    const onChange = vi.fn()
    render(<SecretDataGrid data={{ token: encode('val') }} onChange={onChange} />)

    fireEvent.change(screen.getByPlaceholderText('value'), { target: { value: '' } })

    const lastCall = onChange.mock.calls[onChange.mock.calls.length - 1][0]
    expect(new TextDecoder().decode(lastCall['token'])).toBe('')
  })

  it('unchecking trailing newline removes it', () => {
    const onChange = vi.fn()
    render(<SecretDataGrid data={{ token: encode('val') }} onChange={onChange} />)

    fireEvent.click(screen.getByRole('checkbox', { name: /ensure trailing newline/i }))

    const lastCall = onChange.mock.calls[onChange.mock.calls.length - 1][0]
    expect(new TextDecoder().decode(lastCall['token'])).toBe('val')
  })

  it('shows duplicate key error', () => {
    const onChange = vi.fn()
    render(
      <SecretDataGrid data={{ a: encode('1'), b: encode('2') }} onChange={onChange} />,
    )

    const keyInputs = screen.getAllByPlaceholderText('key')
    fireEvent.change(keyInputs[1], { target: { value: 'a' } })

    expect(screen.getAllByText(/duplicate key/i)).toHaveLength(2)
  })
})

describe('SecretDataGrid readOnly', () => {
  it('renders key names and masked values', () => {
    const onChange = vi.fn()
    render(
      <SecretDataGrid
        data={{ username: encode('admin') }}
        onChange={onChange}
        readOnly
      />,
    )

    expect(screen.getByText('username')).toBeInTheDocument()
    expect(screen.getByText(/••••/)).toBeInTheDocument()
    expect(screen.queryByText('admin')).not.toBeInTheDocument()
  })

  it('reveal button shows the value', () => {
    const onChange = vi.fn()
    render(
      <SecretDataGrid
        data={{ username: encode('admin') }}
        onChange={onChange}
        readOnly
      />,
    )

    fireEvent.click(screen.getByLabelText('reveal'))
    expect(screen.getByText('admin')).toBeInTheDocument()
  })

  it('hide button hides the value again', () => {
    const onChange = vi.fn()
    render(
      <SecretDataGrid
        data={{ username: encode('admin') }}
        onChange={onChange}
        readOnly
      />,
    )

    fireEvent.click(screen.getByLabelText('reveal'))
    expect(screen.getByText('admin')).toBeInTheDocument()
    fireEvent.click(screen.getByLabelText('hide'))
    expect(screen.queryByText('admin')).not.toBeInTheDocument()
  })

  it('copy button copies the value', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.assign(navigator, { clipboard: { writeText } })

    const onChange = vi.fn()
    render(
      <SecretDataGrid
        data={{ username: encode('admin') }}
        onChange={onChange}
        readOnly
      />,
    )

    fireEvent.click(screen.getByLabelText('copy'))
    await waitFor(() => expect(writeText).toHaveBeenCalledWith('admin'))
  })

  it('shows empty message when data is empty', () => {
    const onChange = vi.fn()
    render(<SecretDataGrid data={{}} onChange={onChange} readOnly />)

    expect(screen.getByText(/no data/i)).toBeInTheDocument()
  })
})

import { render, screen, fireEvent } from '@testing-library/react'
import { vi } from 'vitest'
import { SecretDataEditor } from './SecretDataEditor'

const encode = (s: string) => new TextEncoder().encode(s)

describe('SecretDataEditor', () => {
  it('renders existing entries with keys and content', () => {
    const onChange = vi.fn()
    render(
      <SecretDataEditor
        initialData={{
          '.env': encode('KEY=val'),
          'config.yaml': encode('port: 8080'),
        }}
        onChange={onChange}
      />,
    )

    const keyInputs = screen.getAllByPlaceholderText('key')
    expect(keyInputs).toHaveLength(2)

    const contentAreas = screen.getAllByPlaceholderText('value')
    expect(contentAreas).toHaveLength(2)

    // Check values (order depends on Object.entries order)
    const keys = keyInputs.map((el) => (el as HTMLInputElement).value)
    expect(keys).toContain('.env')
    expect(keys).toContain('config.yaml')

    const contents = contentAreas.map((el) => (el as HTMLTextAreaElement).value)
    expect(contents).toContain('KEY=val')
    expect(contents).toContain('port: 8080')
  })

  it('shows Add Key button and no entries when initialData is empty', () => {
    const onChange = vi.fn()
    render(<SecretDataEditor initialData={{}} onChange={onChange} />)

    expect(screen.getByRole('button', { name: /add key/i })).toBeInTheDocument()
    expect(screen.queryAllByPlaceholderText('key')).toHaveLength(0)
  })

  it('appends a new empty entry on Add Key click', () => {
    const onChange = vi.fn()
    render(<SecretDataEditor initialData={{}} onChange={onChange} />)

    fireEvent.click(screen.getByRole('button', { name: /add key/i }))

    expect(screen.getAllByPlaceholderText('key')).toHaveLength(1)
    expect(screen.getAllByPlaceholderText('value')).toHaveLength(1)
  })

  it('removes an entry and fires onChange with updated map', () => {
    const onChange = vi.fn()
    render(
      <SecretDataEditor
        initialData={{
          '.env': encode('KEY=val'),
          'config.yaml': encode('port: 8080'),
        }}
        onChange={onChange}
      />,
    )

    const removeButtons = screen.getAllByLabelText('remove key entry')
    fireEvent.click(removeButtons[0])

    expect(screen.getAllByPlaceholderText('key')).toHaveLength(1)
    expect(onChange).toHaveBeenCalled()
  })

  it('fires onChange with updated key on key change', () => {
    const onChange = vi.fn()
    render(
      <SecretDataEditor
        initialData={{ '.env': encode('KEY=val') }}
        onChange={onChange}
      />,
    )

    fireEvent.change(screen.getByPlaceholderText('key'), {
      target: { value: 'renamed.env' },
    })

    expect(onChange).toHaveBeenCalled()
    const lastCall = onChange.mock.calls[onChange.mock.calls.length - 1][0]
    expect(lastCall).toHaveProperty('renamed.env')
    expect(new TextDecoder().decode(lastCall['renamed.env'])).toBe('KEY=val')
  })

  it('fires onChange with new UTF-8 encoded bytes on content change', () => {
    const onChange = vi.fn()
    render(
      <SecretDataEditor
        initialData={{ '.env': encode('KEY=val') }}
        onChange={onChange}
      />,
    )

    fireEvent.change(screen.getByPlaceholderText('value'), {
      target: { value: 'NEW=content' },
    })

    expect(onChange).toHaveBeenCalled()
    const lastCall = onChange.mock.calls[onChange.mock.calls.length - 1][0]
    expect(new TextDecoder().decode(lastCall['.env'])).toBe('NEW=content')
  })

  it('shows error helper text for duplicate keys', () => {
    const onChange = vi.fn()
    render(
      <SecretDataEditor
        initialData={{
          '.env': encode('KEY=val'),
          'config.yaml': encode('port: 8080'),
        }}
        onChange={onChange}
      />,
    )

    // Rename second file to match first
    const keyInputs = screen.getAllByPlaceholderText('key')
    fireEvent.change(keyInputs[1], { target: { value: '.env' } })

    const errors = screen.getAllByText(/duplicate key/i)
    expect(errors).toHaveLength(2)
  })
})

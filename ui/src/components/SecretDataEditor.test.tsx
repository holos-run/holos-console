import { render, screen, fireEvent } from '@testing-library/react'
import { vi } from 'vitest'
import { SecretDataEditor } from './SecretDataEditor'

const encode = (s: string) => new TextEncoder().encode(s)

describe('SecretDataEditor', () => {
  it('renders existing entries with filenames and content', () => {
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

    const filenameInputs = screen.getAllByPlaceholderText('filename')
    expect(filenameInputs).toHaveLength(2)

    const contentAreas = screen.getAllByPlaceholderText('file content')
    expect(contentAreas).toHaveLength(2)

    // Check values (order depends on Object.entries order)
    const filenames = filenameInputs.map((el) => (el as HTMLInputElement).value)
    expect(filenames).toContain('.env')
    expect(filenames).toContain('config.yaml')

    const contents = contentAreas.map((el) => (el as HTMLTextAreaElement).value)
    expect(contents).toContain('KEY=val')
    expect(contents).toContain('port: 8080')
  })

  it('shows Add File button and no entries when initialData is empty', () => {
    const onChange = vi.fn()
    render(<SecretDataEditor initialData={{}} onChange={onChange} />)

    expect(screen.getByRole('button', { name: /add file/i })).toBeInTheDocument()
    expect(screen.queryAllByPlaceholderText('filename')).toHaveLength(0)
  })

  it('appends a new empty entry on Add File click', () => {
    const onChange = vi.fn()
    render(<SecretDataEditor initialData={{}} onChange={onChange} />)

    fireEvent.click(screen.getByRole('button', { name: /add file/i }))

    expect(screen.getAllByPlaceholderText('filename')).toHaveLength(1)
    expect(screen.getAllByPlaceholderText('file content')).toHaveLength(1)
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

    const removeButtons = screen.getAllByLabelText('remove file entry')
    fireEvent.click(removeButtons[0])

    expect(screen.getAllByPlaceholderText('filename')).toHaveLength(1)
    expect(onChange).toHaveBeenCalled()
  })

  it('fires onChange with updated key on filename change', () => {
    const onChange = vi.fn()
    render(
      <SecretDataEditor
        initialData={{ '.env': encode('KEY=val') }}
        onChange={onChange}
      />,
    )

    fireEvent.change(screen.getByPlaceholderText('filename'), {
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

    fireEvent.change(screen.getByPlaceholderText('file content'), {
      target: { value: 'NEW=content' },
    })

    expect(onChange).toHaveBeenCalled()
    const lastCall = onChange.mock.calls[onChange.mock.calls.length - 1][0]
    expect(new TextDecoder().decode(lastCall['.env'])).toBe('NEW=content')
  })

  it('shows error helper text for duplicate filenames', () => {
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
    const filenameInputs = screen.getAllByPlaceholderText('filename')
    fireEvent.change(filenameInputs[1], { target: { value: '.env' } })

    const errors = screen.getAllByText(/duplicate filename/i)
    expect(errors).toHaveLength(2)
  })
})

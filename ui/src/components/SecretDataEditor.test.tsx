import { render, screen, fireEvent } from '@testing-library/react'
import { vi } from 'vitest'
import { SecretDataEditor } from './SecretDataEditor'

const encode = (s: string) => new TextEncoder().encode(s)

/**
 * Override window.matchMedia so that queries matching the given pattern
 * return matches=true while all others return matches=false.
 */
function mockMatchMedia(matchPattern: RegExp): () => void {
  const original = window.matchMedia
  window.matchMedia = (query: string) => ({
    matches: matchPattern.test(query),
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  })
  return () => {
    window.matchMedia = original
  }
}

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

  it('fires onChange with updated key on key change (trailing newline added by default)', () => {
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
    expect(new TextDecoder().decode(lastCall['renamed.env'])).toBe('KEY=val\n')
  })

  it('fires onChange with new UTF-8 encoded bytes on content change (trailing newline added by default)', () => {
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
    expect(new TextDecoder().decode(lastCall['.env'])).toBe('NEW=content\n')
  })

  it('does not add trailing newline to empty values', () => {
    const onChange = vi.fn()
    render(
      <SecretDataEditor
        initialData={{ '.env': encode('KEY=val') }}
        onChange={onChange}
      />,
    )

    fireEvent.change(screen.getByPlaceholderText('value'), {
      target: { value: '' },
    })

    expect(onChange).toHaveBeenCalled()
    const lastCall = onChange.mock.calls[onChange.mock.calls.length - 1][0]
    expect(new TextDecoder().decode(lastCall['.env'])).toBe('')
  })

  it('does not add trailing newline when checkbox is unchecked', () => {
    const onChange = vi.fn()
    render(
      <SecretDataEditor
        initialData={{ '.env': encode('KEY=val') }}
        onChange={onChange}
      />,
    )

    // Uncheck the trailing newline checkbox
    fireEvent.click(screen.getByRole('checkbox', { name: /ensure trailing newline/i }))

    // The onChange should have been called with the updated data (no trailing newline)
    const lastCall = onChange.mock.calls[onChange.mock.calls.length - 1][0]
    expect(new TextDecoder().decode(lastCall['.env'])).toBe('KEY=val')
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

  it('stacks key above value on mobile', () => {
    const cleanup = mockMatchMedia(/max-width/)
    try {
      const onChange = vi.fn()
      render(
        <SecretDataEditor
          initialData={{ '.env': encode('KEY=val') }}
          onChange={onChange}
        />,
      )

      // The entry row should use column direction on mobile
      const keyInput = screen.getByPlaceholderText('key')
      const entryRow = keyInput.closest('.MuiStack-root')
      expect(entryRow).toHaveStyle({ 'flex-direction': 'column' })

      // Key field should be full width on mobile (no fixed 200px)
      expect(keyInput.closest('.MuiTextField-root')).not.toHaveStyle({ width: '200px' })
    } finally {
      cleanup()
    }
  })

  it('shows key and value side-by-side on desktop', () => {
    const onChange = vi.fn()
    render(
      <SecretDataEditor
        initialData={{ '.env': encode('KEY=val') }}
        onChange={onChange}
      />,
    )

    // The entry row should use row direction on desktop
    const keyInput = screen.getByPlaceholderText('key')
    const entryRow = keyInput.closest('.MuiStack-root')
    expect(entryRow).toHaveStyle({ 'flex-direction': 'row' })
  })
})

import { render, screen } from '@testing-library/react'
import { LinkifiedText } from './linkified-text'

describe('LinkifiedText', () => {
  it('renders plain text with no links', () => {
    render(<LinkifiedText text="Just some plain text" />)
    expect(screen.getByText('Just some plain text')).toBeInTheDocument()
    expect(screen.queryByRole('link')).not.toBeInTheDocument()
  })

  it('renders a single URL as a clickable link', () => {
    render(<LinkifiedText text="https://example.com" />)
    const link = screen.getByRole('link', { name: /https:\/\/example\.com/ })
    expect(link).toBeInTheDocument()
    expect(link).toHaveAttribute('href', 'https://example.com')
    expect(link).toHaveAttribute('target', '_blank')
    expect(link).toHaveAttribute('rel', 'noopener noreferrer')
  })

  it('renders multiple URLs as separate links', () => {
    render(<LinkifiedText text="https://one.com and https://two.com" />)
    const links = screen.getAllByRole('link')
    expect(links).toHaveLength(2)
    expect(links[0]).toHaveAttribute('href', 'https://one.com')
    expect(links[1]).toHaveAttribute('href', 'https://two.com')
  })

  it('renders a URL embedded mid-sentence', () => {
    render(<LinkifiedText text="See https://example.com for details" />)
    const link = screen.getByRole('link', { name: /https:\/\/example\.com/ })
    expect(link).toBeInTheDocument()
    expect(screen.getByText(/See/)).toBeInTheDocument()
    expect(screen.getByText(/for details/)).toBeInTheDocument()
  })

  it('renders nothing for an empty string', () => {
    const { container } = render(<LinkifiedText text="" />)
    expect(container).toBeEmptyDOMElement()
  })

  it('renders nothing when text is undefined', () => {
    const { container } = render(<LinkifiedText text={undefined as unknown as string} />)
    expect(container).toBeEmptyDOMElement()
  })

  it('applies underline and text-primary classes to links', () => {
    render(<LinkifiedText text="https://example.com" />)
    const link = screen.getByRole('link')
    expect(link.className).toContain('underline')
    expect(link.className).toContain('text-primary')
  })
})

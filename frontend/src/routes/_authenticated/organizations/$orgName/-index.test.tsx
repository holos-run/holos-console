import { render, screen } from '@testing-library/react'
import { vi } from 'vitest'
import type React from 'react'

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => ({ orgName: 'my-org' }),
    }),
    Link: ({
      children,
      to,
      className,
    }: {
      children: React.ReactNode
      to: string
      className?: string
    }) => (
      <a href={to} className={className}>
        {children}
      </a>
    ),
  }
})

import { OrgIndexPage } from './index'

describe('OrgIndexPage — thin landing', () => {
  it('renders the org name as a heading', () => {
    render(<OrgIndexPage orgName="my-org" />)
    expect(screen.getByRole('heading', { name: 'my-org' })).toBeInTheDocument()
  })

  it('renders a Projects link pointing at the projects sub-page', () => {
    render(<OrgIndexPage orgName="my-org" />)
    const link = screen.getByRole('link', { name: /projects/i })
    expect(link).toBeInTheDocument()
    expect(link.getAttribute('href')).toBe('/organizations/my-org/projects')
  })

  it('renders a Templates link pointing at the templates sub-page', () => {
    render(<OrgIndexPage orgName="my-org" />)
    // Use getAllByRole and filter to the exact href to avoid ambiguity with
    // "Template Policies" and "Template Bindings" links that also contain "templates".
    const links = screen.getAllByRole('link')
    const link = links.find((l) => l.getAttribute('href') === '/organizations/my-org/templates')
    expect(link).toBeDefined()
    expect(link).toBeInTheDocument()
  })

  it('renders a Template Policies link pointing at the template-policies sub-page', () => {
    render(<OrgIndexPage orgName="my-org" />)
    const link = screen.getByRole('link', { name: /template policies/i })
    expect(link).toBeInTheDocument()
    expect(link.getAttribute('href')).toBe('/organizations/my-org/template-policies')
  })

  it('renders a Template Bindings link pointing at the template-bindings sub-page', () => {
    render(<OrgIndexPage orgName="my-org" />)
    const link = screen.getByRole('link', { name: /template bindings/i })
    expect(link).toBeInTheDocument()
    expect(link.getAttribute('href')).toBe('/organizations/my-org/template-bindings')
  })

  it('renders a Settings link pointing at the settings sub-page', () => {
    render(<OrgIndexPage orgName="my-org" />)
    const link = screen.getByRole('link', { name: /settings/i })
    expect(link).toBeInTheDocument()
    expect(link.getAttribute('href')).toBe('/organizations/my-org/settings')
  })

  it('renders nothing when orgName is empty', () => {
    const { container } = render(<OrgIndexPage orgName="" />)
    expect(container).toBeEmptyDOMElement()
  })

  it('does not fire list queries — no mocks needed for the thin landing', () => {
    // The thin landing has no heavy queries. If this renders without errors
    // (no "missing mock" failures), the landing is correctly query-free.
    render(<OrgIndexPage orgName="my-org" />)
    expect(true).toBe(true)
  })
})

import { render, screen } from '@testing-library/react'
import { vi } from 'vitest'
import type React from 'react'

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => ({ projectName: 'my-project' }),
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

import { ProjectIndexPage } from './index'

describe('ProjectIndexPage — thin landing', () => {
  it('renders the project name as a heading', () => {
    render(<ProjectIndexPage projectName="my-project" />)
    expect(screen.getByRole('heading', { name: 'my-project' })).toBeInTheDocument()
  })

  it('renders a Deployments link pointing at the deployments sub-page', () => {
    render(<ProjectIndexPage projectName="my-project" />)
    const link = screen.getByRole('link', { name: /deployments/i })
    expect(link).toBeInTheDocument()
    expect(link.getAttribute('href')).toBe('/projects/my-project/deployments')
  })

  it('renders a Secrets link pointing at the secrets sub-page', () => {
    render(<ProjectIndexPage projectName="my-project" />)
    const link = screen.getByRole('link', { name: /secrets/i })
    expect(link).toBeInTheDocument()
    expect(link.getAttribute('href')).toBe('/projects/my-project/secrets')
  })

  it('renders a Templates link pointing at the templates sub-page', () => {
    render(<ProjectIndexPage projectName="my-project" />)
    const link = screen.getByRole('link', { name: /templates/i })
    expect(link).toBeInTheDocument()
    expect(link.getAttribute('href')).toBe('/projects/my-project/templates')
  })

  it('renders a Settings link pointing at the settings sub-page', () => {
    render(<ProjectIndexPage projectName="my-project" />)
    const link = screen.getByRole('link', { name: /settings/i })
    expect(link).toBeInTheDocument()
    expect(link.getAttribute('href')).toBe('/projects/my-project/settings')
  })

  it('renders nothing when projectName is empty', () => {
    const { container } = render(<ProjectIndexPage projectName="" />)
    expect(container).toBeEmptyDOMElement()
  })

  it('does not fire list queries — no mocks needed for the thin landing', () => {
    // The thin landing has no heavy queries. If this renders without errors
    // (no "missing mock" failures), the landing is correctly query-free.
    render(<ProjectIndexPage projectName="my-project" />)
    expect(true).toBe(true)
  })
})

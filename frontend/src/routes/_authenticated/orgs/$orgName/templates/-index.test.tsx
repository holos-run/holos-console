import { render, screen, fireEvent, within } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => ({ orgName: 'test-org' }),
    }),
    Link: ({
      children,
      to,
      params,
      title,
      className,
    }: {
      children: React.ReactNode
      to: string
      params?: Record<string, string>
      title?: string
      className?: string
    }) => {
      let href = to
      if (params) {
        for (const [k, v] of Object.entries(params)) {
          href = href.replace(`$${k}`, v)
        }
      }
      return (
        <a href={href} title={title} className={className}>
          {children}
        </a>
      )
    },
  }
})

vi.mock('@/queries/templates', () => ({
  useSearchTemplates: vi.fn(),
}))

import { useSearchTemplates } from '@/queries/templates'
import { OrgTemplatesIndexPage } from './index'

type Template = {
  name: string
  namespace: string
  displayName: string
}

function setup(templates: Template[] = []) {
  ;(useSearchTemplates as Mock).mockReturnValue({
    data: templates,
    isLoading: false,
    error: null,
  })
}

describe('OrgTemplatesIndexPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders loading skeletons while the query is pending', () => {
    ;(useSearchTemplates as Mock).mockReturnValue({
      data: undefined,
      isLoading: true,
      error: null,
    })
    render(<OrgTemplatesIndexPage />)
    expect(screen.getByTestId('templates-loading')).toBeInTheDocument()
  })

  it('renders the error alert when the query fails', () => {
    ;(useSearchTemplates as Mock).mockReturnValue({
      data: undefined,
      isLoading: false,
      error: new Error('boom'),
    })
    render(<OrgTemplatesIndexPage />)
    expect(screen.getByText('boom')).toBeInTheDocument()
  })

  it('renders the empty state when no templates exist', () => {
    setup([])
    render(<OrgTemplatesIndexPage />)
    expect(screen.getByText(/no templates yet/i)).toBeInTheDocument()
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
  })

  it('renders a row for each template across scopes', () => {
    setup([
      { name: 'gateway', namespace: 'org-test-org', displayName: 'Gateway' },
      { name: 'backend', namespace: 'fld-team-a', displayName: 'Backend' },
      { name: 'web', namespace: 'prj-billing', displayName: 'Web Service' },
    ])
    render(<OrgTemplatesIndexPage />)

    const rows = screen.getAllByRole('row')
    expect(rows).toHaveLength(4)

    expect(within(rows[1]).getByText('Gateway')).toBeInTheDocument()
    expect(within(rows[1]).getByText('org-test-org')).toBeInTheDocument()
    expect(within(rows[2]).getByText('Backend')).toBeInTheDocument()
    expect(within(rows[2]).getByText('fld-team-a')).toBeInTheDocument()
    expect(within(rows[3]).getByText('Web Service')).toBeInTheDocument()
    expect(within(rows[3]).getByText('prj-billing')).toBeInTheDocument()
  })

  it('rows link to the consolidated editor route with namespace and name', () => {
    setup([
      { name: 'web', namespace: 'prj-billing', displayName: 'Web Service' },
    ])
    render(<OrgTemplatesIndexPage />)

    const link = screen.getByRole('link', { name: 'Web Service' })
    expect(link).toHaveAttribute(
      'href',
      '/orgs/test-org/templates/prj-billing/web',
    )
    expect(link).toHaveAttribute('title', 'web')
  })

  it('falls back to the slug when displayName is empty', () => {
    setup([{ name: 'ops', namespace: 'org-test-org', displayName: '' }])
    render(<OrgTemplatesIndexPage />)

    const links = screen.getAllByRole('link', { name: 'ops' })
    expect(
      links.some(
        (l) =>
          l.getAttribute('href') === '/orgs/test-org/templates/org-test-org/ops',
      ),
    ).toBe(true)
  })

  it('filters rows via the global search input by display name', () => {
    setup([
      { name: 'gateway', namespace: 'org-test-org', displayName: 'Gateway' },
      { name: 'backend', namespace: 'fld-team-a', displayName: 'Backend' },
    ])
    render(<OrgTemplatesIndexPage />)

    expect(screen.getAllByRole('row')).toHaveLength(3)

    const search = screen.getByLabelText(/search templates/i)
    fireEvent.change(search, { target: { value: 'Gate' } })

    const rowsAfter = screen.getAllByRole('row')
    expect(rowsAfter).toHaveLength(2)
    expect(within(rowsAfter[1]).getByText('Gateway')).toBeInTheDocument()
  })

  it('filters rows by namespace', () => {
    setup([
      { name: 'gateway', namespace: 'org-test-org', displayName: 'Gateway' },
      { name: 'web', namespace: 'prj-billing', displayName: 'Web Service' },
    ])
    render(<OrgTemplatesIndexPage />)

    const search = screen.getByLabelText(/search templates/i)
    fireEvent.change(search, { target: { value: 'prj-billing' } })

    const rows = screen.getAllByRole('row')
    expect(rows).toHaveLength(2)
    expect(within(rows[1]).getByText('Web Service')).toBeInTheDocument()
  })

  it('filters rows by slug name', () => {
    setup([
      { name: 'gateway', namespace: 'org-test-org', displayName: 'Gateway' },
      { name: 'backend', namespace: 'fld-team-a', displayName: 'Backend' },
    ])
    render(<OrgTemplatesIndexPage />)

    const search = screen.getByLabelText(/search templates/i)
    fireEvent.change(search, { target: { value: 'back' } })

    const rows = screen.getAllByRole('row')
    expect(rows).toHaveLength(2)
    expect(within(rows[1]).getByText('Backend')).toBeInTheDocument()
  })
})

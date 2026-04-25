import { render, screen, fireEvent } from '@testing-library/react'
import { vi } from 'vitest'
import React from 'react'

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    Link: ({
      children,
      to,
    }: {
      children: React.ReactNode
      to?: string
    }) => <a href={to ?? '#'}>{children}</a>,
  }
})

import { Toolbar } from './Toolbar'
import type { Kind } from './types'

const SECRET_KIND: Kind = {
  id: 'secret',
  label: 'Secret',
  newHref: '/secrets/new',
  canCreate: true,
}

const DEPLOYMENT_KIND: Kind = {
  id: 'deployment',
  label: 'Deployment',
  newHref: '/deployments/new',
  canCreate: true,
}

function renderToolbar(
  overrides: Partial<React.ComponentProps<typeof Toolbar>> = {},
) {
  const props: React.ComponentProps<typeof Toolbar> = {
    title: 'Test Resources',
    kinds: [SECRET_KIND, DEPLOYMENT_KIND],
    selectedKindIds: [],
    globalFilter: '',
    onGlobalFilterChange: vi.fn(),
    onKindIdsChange: vi.fn(),
    ...overrides,
  }

  render(<Toolbar {...props} />)
  return props
}

describe('Toolbar', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('wires search input changes to the global filter handler', () => {
    const onGlobalFilterChange = vi.fn()
    renderToolbar({ onGlobalFilterChange })

    fireEvent.change(screen.getByRole('textbox', { name: /search/i }), {
      target: { value: 'alpha' },
    })

    expect(onGlobalFilterChange).toHaveBeenCalledWith('alpha')
  })

  it('renders the kind filter only when multiple kinds exist', () => {
    renderToolbar({ kinds: [SECRET_KIND] })

    expect(screen.queryByTestId('kind-filter')).not.toBeInTheDocument()
  })

  it('wires kind checkbox interaction to the kind filter handler', () => {
    const onKindIdsChange = vi.fn()
    renderToolbar({
      selectedKindIds: ['secret'],
      onKindIdsChange,
    })

    fireEvent.click(screen.getByLabelText('Filter Deployment'))

    expect(onKindIdsChange).toHaveBeenCalledWith(['secret', 'deployment'])
  })
})

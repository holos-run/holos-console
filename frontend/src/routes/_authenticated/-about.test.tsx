import { render, screen } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({}),
  }
})

vi.mock('@/queries/version', () => ({
  useVersion: vi.fn(),
}))

import { useVersion } from '@/queries/version'
import { AboutPage } from './about'

describe('AboutPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders Server Version card heading', () => {
    ;(useVersion as Mock).mockReturnValue({
      data: { version: 'v1.2.3', gitCommit: 'abc1234', gitTreeState: 'clean', buildDate: '2024-01-01' },
      isLoading: false,
      error: null,
    })
    render(<AboutPage />)
    expect(screen.getByText('Server Version')).toBeInTheDocument()
  })

  it('renders version value from server', () => {
    ;(useVersion as Mock).mockReturnValue({
      data: { version: 'v1.2.3', gitCommit: 'abc1234', gitTreeState: 'clean', buildDate: '2024-01-01' },
      isLoading: false,
      error: null,
    })
    render(<AboutPage />)
    expect(screen.getByText('v1.2.3')).toBeInTheDocument()
    expect(screen.getByText('abc1234')).toBeInTheDocument()
  })

  it('renders loading state', () => {
    ;(useVersion as Mock).mockReturnValue({ data: undefined, isLoading: true, error: null })
    render(<AboutPage />)
    expect(screen.getByText(/loading version/i)).toBeInTheDocument()
  })

  it('renders error state when version fetch fails', () => {
    ;(useVersion as Mock).mockReturnValue({
      data: undefined,
      isLoading: false,
      error: new Error('connection refused'),
    })
    render(<AboutPage />)
    expect(screen.getByText(/failed to load/i)).toBeInTheDocument()
    expect(screen.getByText(/connection refused/)).toBeInTheDocument()
  })

  it('renders copyright text', () => {
    ;(useVersion as Mock).mockReturnValue({
      data: { version: 'v1.0.0', gitCommit: '', gitTreeState: '', buildDate: '' },
      isLoading: false,
      error: null,
    })
    render(<AboutPage />)
    expect(screen.getByText(/copyright/i)).toBeInTheDocument()
  })

  it('renders Apache 2.0 license link', () => {
    ;(useVersion as Mock).mockReturnValue({
      data: { version: 'v1.0.0', gitCommit: '', gitTreeState: '', buildDate: '' },
      isLoading: false,
      error: null,
    })
    render(<AboutPage />)
    expect(screen.getByText(/Apache/)).toBeInTheDocument()
  })

  it('renders About Holos Console card', () => {
    ;(useVersion as Mock).mockReturnValue({
      data: { version: 'v1.0.0', gitCommit: '', gitTreeState: '', buildDate: '' },
      isLoading: false,
      error: null,
    })
    render(<AboutPage />)
    expect(screen.getByText('About Holos Console')).toBeInTheDocument()
  })
})

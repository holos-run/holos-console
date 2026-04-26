/**
 * Tests for SharedDependencyBadge (HOL-963).
 *
 * Covers:
 *  - isSharedDependency detection for -shared suffix
 *  - Badge renders for shared deployments, not for user-named ones
 *  - Link target when linkHref is provided
 *  - stopPropagation so row navigation is not triggered
 */

import { render, screen, fireEvent } from '@testing-library/react'
import { vi } from 'vitest'
import React from 'react'
import { isSharedDependency, SharedDependencyBadge } from './SharedDependencyBadge'

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    Link: ({ children, to, className }: { children: React.ReactNode; to: string; className?: string }) => (
      <a href={to} className={className}>{children}</a>
    ),
  }
})

describe('isSharedDependency', () => {
  it('returns true for names ending in -shared', () => {
    expect(isSharedDependency('waypoint-shared')).toBe(true)
    expect(isSharedDependency('waypoint-v1-shared')).toBe(true)
    expect(isSharedDependency('waypoint-v1-2-3-shared')).toBe(true)
    expect(isSharedDependency('my-template--v1-2-3--shared')).toBe(true)
  })

  it('returns false for user-named deployments', () => {
    expect(isSharedDependency('my-api')).toBe(false)
    expect(isSharedDependency('api')).toBe(false)
    expect(isSharedDependency('shared-something')).toBe(false) // prefix, not suffix
    expect(isSharedDependency('')).toBe(false)
  })
})

describe('SharedDependencyBadge', () => {
  it('renders the badge for a shared dependency deployment', () => {
    render(<SharedDependencyBadge name="waypoint-shared" />)
    expect(screen.getByTestId('shared-dependency-badge')).toBeInTheDocument()
    expect(screen.getByTestId('shared-dependency-badge')).toHaveTextContent('Shared Dep')
  })

  it('returns null for a user-named deployment', () => {
    const { container } = render(<SharedDependencyBadge name="my-api" />)
    expect(container.firstChild).toBeNull()
  })

  it('renders a link when linkHref is provided', () => {
    render(
      <SharedDependencyBadge
        name="waypoint-shared"
        linkHref="/projects/test-project/templates/waypoint-dep"
      />
    )
    const badge = screen.getByTestId('shared-dependency-badge')
    expect(badge).toBeInTheDocument()
    const link = badge.closest('a')
    expect(link).not.toBeNull()
    expect(link?.getAttribute('href')).toBe('/projects/test-project/templates/waypoint-dep')
  })

  it('renders without a link when linkHref is not provided', () => {
    render(<SharedDependencyBadge name="waypoint-shared" />)
    const badge = screen.getByTestId('shared-dependency-badge')
    expect(badge.closest('a')).toBeNull()
  })

  it('stopPropagation prevents row-level click from firing', () => {
    const rowClick = vi.fn()
    render(
      <div onClick={rowClick} data-testid="row">
        <SharedDependencyBadge name="waypoint-shared" />
      </div>
    )
    const badge = screen.getByTestId('shared-dependency-badge')
    fireEvent.click(badge)
    expect(rowClick).not.toHaveBeenCalled()
  })
})

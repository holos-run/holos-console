/**
 * Tests for SharedDependencyBadge (HOL-963).
 *
 * Covers:
 *  - isSharedDependency suffix detection (kept as a fallback signal)
 *  - Badge renders for shared deployments via -shared suffix and via
 *    non-empty dependencies prop
 *  - Tooltip surfaces originating CRD info from the dependencies prop
 *  - Link target when linkHref is provided
 *  - stopPropagation so row navigation is not triggered
 */

import { render, screen, fireEvent } from '@testing-library/react'
import { vi } from 'vitest'
import React from 'react'
import { create } from '@bufbuild/protobuf'
import { isSharedDependency, SharedDependencyBadge } from './SharedDependencyBadge'
import {
  DeploymentDependencySchema,
  OriginatingObjectSchema,
} from '@/gen/holos/console/v1/deployments_pb'

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    Link: ({ children, to, className }: { children: React.ReactNode; to: string; className?: string }) => (
      <a href={to} className={className}>{children}</a>
    ),
  }
})

function depFixture(opts: { kind: string; ns: string; name: string }) {
  return create(DeploymentDependencySchema, {
    originatingObject: create(OriginatingObjectSchema, {
      kind: opts.kind,
      namespace: opts.ns,
      name: opts.name,
    }),
  })
}

describe('isSharedDependency', () => {
  it('returns true for names ending in -shared', () => {
    expect(isSharedDependency('waypoint-shared')).toBe(true)
    expect(isSharedDependency('waypoint-v1-shared')).toBe(true)
  })

  it('returns false for user-named deployments', () => {
    expect(isSharedDependency('my-api')).toBe(false)
    expect(isSharedDependency('shared-something')).toBe(false)
    expect(isSharedDependency('')).toBe(false)
  })
})

describe('SharedDependencyBadge', () => {
  it('renders the badge for a -shared suffix deployment with no dependencies prop', () => {
    render(<SharedDependencyBadge name="waypoint-shared" />)
    expect(screen.getByTestId('shared-dependency-badge')).toBeInTheDocument()
  })

  it('renders the badge when dependencies are provided even on a user-named deployment', () => {
    render(
      <SharedDependencyBadge
        name="my-api"
        dependencies={[depFixture({ kind: 'TemplateDependency', ns: 'prj-x', name: 'edge-a' })]}
      />,
    )
    expect(screen.getByTestId('shared-dependency-badge')).toBeInTheDocument()
  })

  it('returns null when neither suffix nor dependencies indicate a shared singleton', () => {
    const { container } = render(<SharedDependencyBadge name="my-api" />)
    expect(container.firstChild).toBeNull()
  })

  it('exposes originating CRD objects via the title attribute when dependencies are provided', () => {
    render(
      <SharedDependencyBadge
        name="waypoint-shared"
        dependencies={[
          depFixture({ kind: 'TemplateDependency', ns: 'prj-flowers', name: 'api-needs-waypoint' }),
          depFixture({ kind: 'TemplateRequirement', ns: 'fld-eng', name: 'require-waypoint' }),
        ]}
      />,
    )
    const badge = screen.getByTestId('shared-dependency-badge')
    const title = badge.getAttribute('title') ?? ''
    expect(title).toContain('TemplateDependency: prj-flowers/api-needs-waypoint')
    expect(title).toContain('TemplateRequirement: fld-eng/require-waypoint')
  })

  it('renders a link to linkHref when provided', () => {
    render(
      <SharedDependencyBadge
        name="waypoint-shared"
        linkHref="/projects/test/deployments/waypoint-shared"
      />,
    )
    const badge = screen.getByTestId('shared-dependency-badge')
    expect(badge.closest('a')?.getAttribute('href')).toBe('/projects/test/deployments/waypoint-shared')
  })

  it('renders without a link when linkHref is omitted', () => {
    render(<SharedDependencyBadge name="waypoint-shared" />)
    expect(screen.getByTestId('shared-dependency-badge').closest('a')).toBeNull()
  })

  it('stopPropagation prevents row-level click from firing', () => {
    const rowClick = vi.fn()
    render(
      <div onClick={rowClick} data-testid="row">
        <SharedDependencyBadge name="waypoint-shared" />
      </div>,
    )
    fireEvent.click(screen.getByTestId('shared-dependency-badge'))
    expect(rowClick).not.toHaveBeenCalled()
  })
})

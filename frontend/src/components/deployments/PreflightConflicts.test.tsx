/**
 * Tests for PreflightConflicts (HOL-963).
 *
 * Covers:
 *  - Returns null when no conflicts
 *  - Renders collision details
 *  - Renders version-conflict details
 *  - hasConflicts utility function
 *  - aria-label for screen readers
 */

import { render, screen } from '@testing-library/react'
import React from 'react'
import { PreflightConflicts, hasConflicts } from './PreflightConflicts'
import type { CollisionDetail, VersionConflictDetail } from '@/gen/holos/console/v1/deployments_pb'

function makeCollision(plannedName: string, conflictingName: string, advice = ''): CollisionDetail {
  return {
    $typeName: 'holos.console.v1.CollisionDetail',
    plannedName,
    conflictingName,
    advice,
  }
}

function makeVersionConflict(
  templateName: string,
  templateNamespace: string,
  constraints: string[],
  dependentNames: string[],
): VersionConflictDetail {
  return {
    $typeName: 'holos.console.v1.VersionConflictDetail',
    templateName,
    templateNamespace,
    constraints,
    dependentNames,
  }
}

describe('hasConflicts', () => {
  it('returns false when both lists are empty/undefined', () => {
    expect(hasConflicts([], [])).toBe(false)
    expect(hasConflicts(undefined, undefined)).toBe(false)
    expect(hasConflicts([], undefined)).toBe(false)
  })

  it('returns true when collisions are present', () => {
    expect(hasConflicts([makeCollision('a', 'b')], [])).toBe(true)
  })

  it('returns true when version conflicts are present', () => {
    expect(hasConflicts([], [makeVersionConflict('tmpl', 'ns', ['>=1.0.0', '>=2.0.0'], ['dep-a'])])).toBe(true)
  })

  it('returns true when both are present', () => {
    expect(
      hasConflicts(
        [makeCollision('a', 'b')],
        [makeVersionConflict('tmpl', 'ns', ['>=1.0.0'], ['dep-a'])],
      ),
    ).toBe(true)
  })
})

describe('PreflightConflicts', () => {
  it('renders nothing when no conflicts exist', () => {
    const { container } = render(<PreflightConflicts collisions={[]} versionConflicts={[]} />)
    expect(container.firstChild).toBeNull()
  })

  it('renders nothing when props are omitted', () => {
    const { container } = render(<PreflightConflicts />)
    expect(container.firstChild).toBeNull()
  })

  it('renders the conflicts alert when collisions are present', () => {
    render(
      <PreflightConflicts
        collisions={[makeCollision('waypoint-shared', 'waypoint', 'Rename your deployment.')]}
      />
    )
    expect(screen.getByRole('alert', { name: /preflight conflicts/i })).toBeInTheDocument()
    expect(screen.getByTestId('preflight-conflicts')).toBeInTheDocument()
  })

  it('renders collision planned name and conflicting name', () => {
    render(
      <PreflightConflicts
        collisions={[makeCollision('waypoint-shared', 'waypoint', 'Choose a different name.')]}
      />
    )
    expect(screen.getByText(/waypoint-shared/)).toBeInTheDocument()
    expect(screen.getByText('waypoint')).toBeInTheDocument()
    expect(screen.getByText(/Choose a different name\./i)).toBeInTheDocument()
  })

  it('renders version conflict template and constraints', () => {
    render(
      <PreflightConflicts
        versionConflicts={[
          makeVersionConflict('waypoint', 'platform-ns', ['>=1.0.0 <2.0.0', '>=2.0.0'], ['api', 'worker']),
        ]}
      />
    )
    expect(screen.getByTestId('preflight-version-conflict-waypoint')).toBeInTheDocument()
    expect(screen.getByText(/platform-ns\/waypoint/)).toBeInTheDocument()
    expect(screen.getByText(/>=1.0.0 <2.0.0/)).toBeInTheDocument()
    expect(screen.getByText(/api, worker/)).toBeInTheDocument()
  })

  it('renders both collisions and version conflicts together', () => {
    render(
      <PreflightConflicts
        collisions={[makeCollision('a-shared', 'a', '')]}
        versionConflicts={[makeVersionConflict('tmpl', 'ns', ['>=1.0.0'], ['dep'])]}
      />
    )
    expect(screen.getByText(/Name collisions/i)).toBeInTheDocument()
    expect(screen.getByText(/Version constraint conflicts/i)).toBeInTheDocument()
  })

  it('renders Apply button is disabled message', () => {
    render(
      <PreflightConflicts
        collisions={[makeCollision('a-shared', 'a', '')]}
      />
    )
    expect(screen.getByText(/Apply button is disabled/i)).toBeInTheDocument()
  })
})

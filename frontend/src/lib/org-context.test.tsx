import { render, screen, act } from '@testing-library/react'
import { vi } from 'vitest'
import React from 'react'

vi.mock('@/queries/organizations', () => ({
  useListOrganizations: () => ({ data: undefined, isLoading: false }),
}))

import { OrgProvider, useOrg } from './org-context'

function OrgDisplay() {
  const { selectedOrg } = useOrg()
  return <div data-testid="selected-org">{selectedOrg ?? 'none'}</div>
}

function OrgSetter({ name }: { name: string }) {
  const { setSelectedOrg } = useOrg()
  return <button onClick={() => setSelectedOrg(name)}>select</button>
}

describe('OrgProvider — localStorage persistence', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('reads initial selectedOrg from localStorage on mount', () => {
    localStorage.setItem('holos-selected-org', 'my-org')
    render(
      <OrgProvider>
        <OrgDisplay />
      </OrgProvider>,
    )
    expect(screen.getByTestId('selected-org').textContent).toBe('my-org')
  })

  it('writes selectedOrg to localStorage when setSelectedOrg is called', async () => {
    render(
      <OrgProvider>
        <OrgSetter name="acme" />
      </OrgProvider>,
    )
    await act(async () => {
      screen.getByRole('button', { name: 'select' }).click()
    })
    expect(localStorage.getItem('holos-selected-org')).toBe('acme')
  })

  it('does not read from sessionStorage', () => {
    sessionStorage.setItem('holos-selected-org', 'session-org')
    render(
      <OrgProvider>
        <OrgDisplay />
      </OrgProvider>,
    )
    // Should show 'none' because localStorage is empty, not sessionStorage
    expect(screen.getByTestId('selected-org').textContent).toBe('none')
  })
})

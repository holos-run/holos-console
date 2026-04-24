import { render, waitFor } from '@testing-library/react'
import { vi } from 'vitest'
import React from 'react'

const mockSetSelectedOrg = vi.fn()

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({}),
    Outlet: () => null,
  }
})

vi.mock('@/lib/org-context', () => ({
  useOrg: () => ({
    selectedOrg: null,
    setSelectedOrg: mockSetSelectedOrg,
    organizations: [],
    isLoading: false,
  }),
}))

import { OrgLayout } from './$orgName'

describe('OrgLayout — URL sync', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('calls setSelectedOrg with the orgName param on mount', async () => {
    render(<OrgLayout orgName="my-org" />)
    await waitFor(() => {
      expect(mockSetSelectedOrg).toHaveBeenCalledWith('my-org')
    })
  })

  it('calls setSelectedOrg again when orgName param changes', async () => {
    const { rerender } = render(<OrgLayout orgName="org-a" />)
    await waitFor(() => {
      expect(mockSetSelectedOrg).toHaveBeenCalledWith('org-a')
    })

    rerender(<OrgLayout orgName="org-b" />)
    await waitFor(() => {
      expect(mockSetSelectedOrg).toHaveBeenCalledWith('org-b')
    })
  })
})

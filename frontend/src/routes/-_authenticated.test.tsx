import { render, waitFor } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({}),
    Outlet: () => null,
  }
})

vi.mock('@/components/ui/sidebar', () => ({
  SidebarInset: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  SidebarProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  SidebarTrigger: () => null,
}))

vi.mock('@/components/app-sidebar', () => ({ AppSidebar: () => null }))

vi.mock('@/lib/org-context', () => ({
  OrgProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  useOrg: () => ({ organizations: [], selectedOrg: null, setSelectedOrg: vi.fn(), isLoading: false }),
}))

vi.mock('@/lib/project-context', () => ({
  ProjectProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}))

vi.mock('@/components/ui/separator', () => ({ Separator: () => null }))

vi.mock('@/lib/auth', () => ({ useAuth: vi.fn() }))

import { useAuth } from '@/lib/auth'
import { AuthenticatedLayout } from './_authenticated'

const mockLogin = vi.fn()
const mockRefreshTokens = vi.fn()

function setAuthState({
  isAuthenticated = false,
  isLoading = false,
}: {
  isAuthenticated?: boolean
  isLoading?: boolean
} = {}) {
  ;(useAuth as Mock).mockReturnValue({
    isAuthenticated,
    isLoading,
    login: mockLogin,
    refreshTokens: mockRefreshTokens,
  })
}

describe('AuthenticatedLayout silent renewal', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('attempts silent renewal before redirecting when not authenticated', async () => {
    mockRefreshTokens.mockResolvedValue(undefined)
    setAuthState({ isAuthenticated: false, isLoading: false })

    render(<AuthenticatedLayout />)

    await waitFor(() => {
      expect(mockRefreshTokens).toHaveBeenCalled()
    })
    expect(mockLogin).not.toHaveBeenCalled()
  })

  it('does not redirect when silent renewal fails (child routes handle auth)', async () => {
    mockRefreshTokens.mockRejectedValue(new Error('silent renew failed'))
    setAuthState({ isAuthenticated: false, isLoading: false })

    render(<AuthenticatedLayout />)

    await waitFor(() => {
      expect(mockRefreshTokens).toHaveBeenCalled()
    })
    expect(mockLogin).not.toHaveBeenCalled()
  })

  it('does not attempt auth when still loading', async () => {
    setAuthState({ isAuthenticated: false, isLoading: true })

    render(<AuthenticatedLayout />)

    await new Promise((r) => setTimeout(r, 10))
    expect(mockRefreshTokens).not.toHaveBeenCalled()
    expect(mockLogin).not.toHaveBeenCalled()
  })

  it('does not attempt auth when already authenticated', async () => {
    setAuthState({ isAuthenticated: true, isLoading: false })

    render(<AuthenticatedLayout />)

    await new Promise((r) => setTimeout(r, 10))
    expect(mockRefreshTokens).not.toHaveBeenCalled()
    expect(mockLogin).not.toHaveBeenCalled()
  })
})

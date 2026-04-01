import { render, waitFor } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

const mockSetSelectedProject = vi.fn()
const mockSetSelectedOrg = vi.fn()

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({}),
    Navigate: () => null,
    Outlet: () => null,
    useMatchRoute: () => () => false,
  }
})

vi.mock('@/lib/project-context', () => ({
  useProject: () => ({
    selectedProject: null,
    setSelectedProject: mockSetSelectedProject,
    projects: [],
    isLoading: false,
  }),
}))

vi.mock('@/lib/org-context', () => ({
  useOrg: () => ({
    selectedOrg: null,
    setSelectedOrg: mockSetSelectedOrg,
    organizations: [],
    isLoading: false,
  }),
}))

vi.mock('@/queries/projects', () => ({
  useGetProject: vi.fn(),
}))

import { useGetProject } from '@/queries/projects'
import { ProjectLayout } from './$projectName'

describe('ProjectLayout — URL sync', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    ;(useGetProject as Mock).mockReturnValue({ data: undefined, isLoading: true })
  })

  it('calls setSelectedProject with the projectName param on mount', async () => {
    render(<ProjectLayout projectName="my-project" />)
    await waitFor(() => {
      expect(mockSetSelectedProject).toHaveBeenCalledWith('my-project')
    })
  })

  it('calls setSelectedProject again when projectName param changes', async () => {
    const { rerender } = render(<ProjectLayout projectName="project-a" />)
    await waitFor(() => {
      expect(mockSetSelectedProject).toHaveBeenCalledWith('project-a')
    })

    rerender(<ProjectLayout projectName="project-b" />)
    await waitFor(() => {
      expect(mockSetSelectedProject).toHaveBeenCalledWith('project-b')
    })
  })

  it('calls setSelectedOrg with the project organization when project data loads', async () => {
    ;(useGetProject as Mock).mockReturnValue({
      data: { name: 'my-project', organization: 'my-org' },
      isLoading: false,
    })
    render(<ProjectLayout projectName="my-project" />)
    await waitFor(() => {
      expect(mockSetSelectedOrg).toHaveBeenCalledWith('my-org')
    })
  })

  it('does not call setSelectedOrg when project data is still loading', async () => {
    ;(useGetProject as Mock).mockReturnValue({ data: undefined, isLoading: true })
    render(<ProjectLayout projectName="my-project" />)
    await waitFor(() => {
      expect(mockSetSelectedProject).toHaveBeenCalledWith('my-project')
    })
    expect(mockSetSelectedOrg).not.toHaveBeenCalled()
  })
})

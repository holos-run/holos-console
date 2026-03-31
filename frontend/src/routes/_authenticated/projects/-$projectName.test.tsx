import { render, waitFor } from '@testing-library/react'
import { vi } from 'vitest'
import React from 'react'

const mockSetSelectedProject = vi.fn()

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

import { ProjectLayout } from './$projectName'

describe('ProjectLayout — URL sync', () => {
  beforeEach(() => {
    vi.clearAllMocks()
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
})

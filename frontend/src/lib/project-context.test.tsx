import { render, screen, act } from '@testing-library/react'
import { vi } from 'vitest'
import React from 'react'

vi.mock('@/queries/projects', () => ({
  useListProjects: () => ({ data: undefined, isLoading: false }),
}))

vi.mock('@/lib/org-context', () => ({
  useOrg: () => ({ selectedOrg: 'test-org', setSelectedOrg: vi.fn(), organizations: [], isLoading: false }),
}))

import { ProjectProvider, useProject } from './project-context'

function ProjectDisplay() {
  const { selectedProject } = useProject()
  return <div data-testid="selected-project">{selectedProject ?? 'none'}</div>
}

function ProjectSetter({ name }: { name: string }) {
  const { setSelectedProject } = useProject()
  return <button onClick={() => setSelectedProject(name)}>select</button>
}

describe('ProjectProvider — localStorage persistence', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
  })

  it('reads initial selectedProject from localStorage on mount', () => {
    localStorage.setItem('holos-selected-project', 'my-project')
    render(
      <ProjectProvider>
        <ProjectDisplay />
      </ProjectProvider>,
    )
    expect(screen.getByTestId('selected-project').textContent).toBe('my-project')
  })

  it('writes selectedProject to localStorage when setSelectedProject is called', async () => {
    render(
      <ProjectProvider>
        <ProjectSetter name="api" />
      </ProjectProvider>,
    )
    await act(async () => {
      screen.getByRole('button', { name: 'select' }).click()
    })
    expect(localStorage.getItem('holos-selected-project')).toBe('api')
  })

  it('does not read from sessionStorage', () => {
    sessionStorage.setItem('holos-selected-project', 'session-project')
    render(
      <ProjectProvider>
        <ProjectDisplay />
      </ProjectProvider>,
    )
    // Should show 'none' because localStorage is empty, not sessionStorage
    expect(screen.getByTestId('selected-project').textContent).toBe('none')
  })
})

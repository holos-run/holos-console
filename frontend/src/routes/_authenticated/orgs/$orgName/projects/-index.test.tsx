import { render, screen, fireEvent } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

const mockNavigate = vi.fn()
const mockSetSelectedProject = vi.fn()

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => ({ orgName: 'test-org' }),
    }),
    Link: ({
      children,
      className,
      to,
      params,
    }: {
      children: React.ReactNode
      className?: string
      to?: string
      params?: Record<string, string>
    }) => (
      <a href={to} data-params={JSON.stringify(params)} className={className}>
        {children}
      </a>
    ),
    useNavigate: () => mockNavigate,
    useRouter: () => ({ state: { location: { pathname: '/orgs/test-org/projects' } } }),
  }
})

vi.mock('@/queries/projects', () => ({
  useListProjects: vi.fn(),
}))

vi.mock('@/lib/project-context', () => ({
  useProject: vi.fn(),
}))

vi.mock('@/lib/org-context', () => ({
  useOrg: vi.fn().mockReturnValue({
    selectedOrg: 'test-org',
    setSelectedOrg: vi.fn(),
    organizations: [],
    isLoading: false,
  }),
}))

vi.mock('@/components/create-project-dialog', () => ({
  CreateProjectDialog: ({ open }: { open: boolean }) =>
    open ? <div data-testid="create-project-dialog" /> : null,
}))

import { useListProjects } from '@/queries/projects'
import { useProject } from '@/lib/project-context'
import { ProjectsIndexPage } from './index'

function makeProject(name: string, displayName = '', description = '') {
  return {
    name,
    displayName,
    description,
    userRole: 3,
    userGrants: [],
    roleGrants: [],
    organization: 'test-org',
  }
}

function setupMocks(projects = [makeProject('test-project', 'Test Project')]) {
  ;(useListProjects as Mock).mockReturnValue({
    data: { projects },
    isLoading: false,
    error: null,
  })
  ;(useProject as Mock).mockReturnValue({
    setSelectedProject: mockSetSelectedProject,
    selectedProject: null,
    projects,
    isLoading: false,
  })
}

describe('ProjectsIndexPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders loading skeletons while query is pending', () => {
    ;(useListProjects as Mock).mockReturnValue({
      data: undefined,
      isLoading: true,
      error: null,
    })
    ;(useProject as Mock).mockReturnValue({
      setSelectedProject: mockSetSelectedProject,
      selectedProject: null,
      projects: [],
      isLoading: true,
    })
    render(<ProjectsIndexPage />)
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
  })

  it('renders empty-state prompt when project list is empty', () => {
    setupMocks([])
    render(<ProjectsIndexPage />)
    expect(screen.getByText(/no projects/i)).toBeInTheDocument()
  })

  it('renders a table row for each project returned by the mock query', () => {
    setupMocks([
      makeProject('alpha', 'Alpha Project'),
      makeProject('beta', 'Beta Project'),
    ])
    render(<ProjectsIndexPage />)
    expect(screen.getByText('Alpha Project')).toBeInTheDocument()
    expect(screen.getByText('Beta Project')).toBeInTheDocument()
  })

  it('shows slug in name column', () => {
    setupMocks([makeProject('my-slug', 'My Project')])
    render(<ProjectsIndexPage />)
    expect(screen.getByText('my-slug')).toBeInTheDocument()
  })

  it('search input filters visible rows by display name', () => {
    setupMocks([
      makeProject('alpha', 'Alpha Project'),
      makeProject('beta', 'Beta Project'),
    ])
    render(<ProjectsIndexPage />)
    const searchInput = screen.getByPlaceholderText(/search/i)
    fireEvent.change(searchInput, { target: { value: 'alpha' } })
    expect(screen.getByText('Alpha Project')).toBeInTheDocument()
    expect(screen.queryByText('Beta Project')).not.toBeInTheDocument()
  })

  it('search input filters visible rows by slug', () => {
    setupMocks([
      makeProject('alpha-slug', 'Alpha Project'),
      makeProject('beta-slug', 'Beta Project'),
    ])
    render(<ProjectsIndexPage />)
    const searchInput = screen.getByPlaceholderText(/search/i)
    fireEvent.change(searchInput, { target: { value: 'beta-slug' } })
    expect(screen.queryByText('Alpha Project')).not.toBeInTheDocument()
    expect(screen.getByText('Beta Project')).toBeInTheDocument()
  })

  it('clicking a project row navigates to /projects/$projectName/secrets and sets selectedProject', () => {
    setupMocks([makeProject('my-project', 'My Project')])
    render(<ProjectsIndexPage />)
    const row = screen.getByText('My Project').closest('tr')!
    fireEvent.click(row)
    expect(mockSetSelectedProject).toHaveBeenCalledWith('my-project')
    expect(mockNavigate).toHaveBeenCalledWith({
      to: '/projects/$projectName/secrets',
      params: { projectName: 'my-project' },
    })
  })

  it('Create Project button is visible', () => {
    setupMocks([])
    render(<ProjectsIndexPage />)
    const buttons = screen.getAllByRole('button', { name: /create project/i })
    expect(buttons.length).toBeGreaterThanOrEqual(1)
  })

  it('renders error alert when query fails', () => {
    ;(useListProjects as Mock).mockReturnValue({
      data: undefined,
      isLoading: false,
      error: new Error('failed to load projects'),
    })
    ;(useProject as Mock).mockReturnValue({
      setSelectedProject: mockSetSelectedProject,
      selectedProject: null,
      projects: [],
      isLoading: false,
    })
    render(<ProjectsIndexPage />)
    expect(screen.getByText(/failed to load projects/i)).toBeInTheDocument()
  })

  it('pagination controls appear when projects exceed page size', () => {
    const manyProjects = Array.from({ length: 30 }, (_, i) =>
      makeProject(`project-${i}`, `Project ${i}`),
    )
    setupMocks(manyProjects)
    render(<ProjectsIndexPage />)
    expect(screen.getByRole('button', { name: /next/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /previous/i })).toBeInTheDocument()
  })

  it('pagination next button advances to second page', () => {
    const manyProjects = Array.from({ length: 30 }, (_, i) =>
      makeProject(`project-${i.toString().padStart(2, '0')}`, `Project ${i.toString().padStart(2, '0')}`),
    )
    setupMocks(manyProjects)
    render(<ProjectsIndexPage />)
    // First page: projects 0-24
    expect(screen.getByText('Project 00')).toBeInTheDocument()
    expect(screen.queryByText('Project 25')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /next/i }))
    // Second page: projects 25-29
    expect(screen.queryByText('Project 00')).not.toBeInTheDocument()
    expect(screen.getByText('Project 25')).toBeInTheDocument()
  })
})

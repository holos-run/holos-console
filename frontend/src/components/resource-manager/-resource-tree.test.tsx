/**
 * Unit tests for ResourceTree and TreeNode components.
 *
 * Vitest + RTL, with vi.mock() for router, queries, and sonner.
 * The tree is seeded with a three-level hierarchy:
 *   org "my-org"
 *   ├── folder "folder-a" (child of org)
 *   │   └── project "project-1" (child of folder-a)
 *   ├── folder "folder-b" (child of org)
 *   └── project "project-2" (child of org)
 */

import { render, screen, fireEvent } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    Link: ({
      children,
      to,
      params,
      ...rest
    }: {
      children: React.ReactNode
      to?: string
      params?: Record<string, string>
      [key: string]: unknown
    }) => {
      // Build a simple href from the route pattern + params for assertion
      let href = to ?? '#'
      if (params) {
        for (const [k, v] of Object.entries(params)) {
          href = href.replace(new RegExp(`\\$${k}`), v)
        }
      }
      return <a href={href} {...rest}>{children}</a>
    },
    createFileRoute: () => () => ({}),
    useNavigate: () => vi.fn(),
    useRouter: () => ({ state: { location: { pathname: '/resource-manager' } } }),
  }
})

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

vi.mock('@/queries/folders', () => ({
  useDeleteFolder: vi.fn(() => ({
    mutateAsync: vi.fn(),
    isPending: false,
  })),
}))

vi.mock('@/queries/projects', () => ({
  useDeleteProject: vi.fn(() => ({
    mutateAsync: vi.fn(),
    isPending: false,
  })),
}))

// ---------------------------------------------------------------------------
// Imports after mocks
// ---------------------------------------------------------------------------

import { ResourceType } from '@/gen/holos/console/v1/resources_pb'
import type { Resource, PathElement } from '@/gen/holos/console/v1/resources_pb'
import { ResourceTree } from './ResourceTree'

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

function makePathElement(
  name: string,
  displayName = '',
  type = ResourceType.UNSPECIFIED,
): PathElement {
  return { name, displayName, type } as PathElement
}

function makeResource(
  type: ResourceType,
  name: string,
  displayName: string,
  pathElements: PathElement[],
): Resource {
  return { type, name, displayName, path: pathElements } as Resource
}

const ORG_NAME = 'my-org'
const ORG_PATH_ELEMENT = makePathElement(ORG_NAME, 'My Org')

// Resources seeded into the three-level tree
const RESOURCES: Resource[] = [
  // folder-a: direct child of org
  makeResource(ResourceType.FOLDER, 'folder-a', 'Folder A', [ORG_PATH_ELEMENT]),
  // folder-b: direct child of org
  makeResource(ResourceType.FOLDER, 'folder-b', 'Folder B', [ORG_PATH_ELEMENT]),
  // project-1: child of folder-a
  makeResource(ResourceType.PROJECT, 'project-1', 'Project 1', [
    ORG_PATH_ELEMENT,
    makePathElement('folder-a', 'Folder A', ResourceType.FOLDER),
  ]),
  // project-2: direct child of org
  makeResource(ResourceType.PROJECT, 'project-2', 'Project 2', [ORG_PATH_ELEMENT]),
]

// ---------------------------------------------------------------------------
// Helper
// ---------------------------------------------------------------------------

function renderTree(expanded: Set<string> = new Set(), onToggle = vi.fn()) {
  return render(
    <ResourceTree
      orgName={ORG_NAME}
      resources={RESOURCES}
      expanded={expanded}
      onToggle={onToggle}
      organization={ORG_NAME}
    />,
  )
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('ResourceTree', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders the root org row', () => {
    renderTree()
    expect(screen.getByTestId(`tree-row-${ORG_NAME}`)).toBeInTheDocument()
  })

  it('org row is expanded by default (children always visible)', () => {
    renderTree()
    // Folder A and Folder B should be visible as direct children of org
    expect(screen.getByTestId('tree-row-folder-a')).toBeInTheDocument()
    expect(screen.getByTestId('tree-row-folder-b')).toBeInTheDocument()
    // project-2 is a direct child of org
    expect(screen.getByTestId('tree-row-project-2')).toBeInTheDocument()
  })

  it('project-1 is hidden when folder-a is collapsed', () => {
    // Default: folder-a not in expanded set
    renderTree()
    expect(screen.queryByTestId('tree-row-project-1')).not.toBeInTheDocument()
  })

  it('project-1 is visible when folder-a is expanded', () => {
    renderTree(new Set(['folder-a']))
    expect(screen.getByTestId('tree-row-project-1')).toBeInTheDocument()
  })

  it('clicking the folder toggle calls onToggle with the folder name', () => {
    const onToggle = vi.fn()
    renderTree(new Set(), onToggle)
    const toggleBtn = screen.getByTestId('tree-toggle-folder-a')
    fireEvent.click(toggleBtn)
    expect(onToggle).toHaveBeenCalledWith('folder-a')
  })

  it('org row has a disclosure toggle', () => {
    renderTree()
    // Org toggle exists because org is expandable
    expect(screen.getByTestId(`tree-toggle-${ORG_NAME}`)).toBeInTheDocument()
  })

  it('project rows do NOT have a disclosure toggle', () => {
    renderTree(new Set(['folder-a']))
    expect(
      screen.queryByTestId('tree-toggle-project-1'),
    ).not.toBeInTheDocument()
    expect(
      screen.queryByTestId('tree-toggle-project-2'),
    ).not.toBeInTheDocument()
  })

  it('org row has a settings link to org settings', () => {
    renderTree()
    const settingsWrapper = screen.getByTestId(`tree-settings-${ORG_NAME}`)
    const link = settingsWrapper.querySelector('a')
    expect(link?.getAttribute('href')).toContain(ORG_NAME)
  })

  it('folder-a row has a settings link', () => {
    renderTree()
    const settingsWrapper = screen.getByTestId('tree-settings-folder-a')
    expect(settingsWrapper).toBeInTheDocument()
    const link = settingsWrapper.querySelector('a')
    expect(link?.getAttribute('href')).toContain('folder-a')
  })

  it('project-2 row has a settings link', () => {
    renderTree()
    const settingsWrapper = screen.getByTestId('tree-settings-project-2')
    expect(settingsWrapper).toBeInTheDocument()
    const link = settingsWrapper.querySelector('a')
    expect(link?.getAttribute('href')).toContain('project-2')
  })

  it('org row has NO delete button', () => {
    renderTree()
    expect(
      screen.queryByTestId(`tree-delete-${ORG_NAME}`),
    ).not.toBeInTheDocument()
  })

  it('folder row has a delete button', () => {
    renderTree()
    expect(screen.getByTestId('tree-delete-folder-a')).toBeInTheDocument()
  })

  it('project row has a delete button', () => {
    renderTree()
    expect(screen.getByTestId('tree-delete-project-2')).toBeInTheDocument()
  })

  it('org row display name links to /orgs/$orgName', () => {
    renderTree()
    const orgLink = screen.getByTestId(`tree-link-org-${ORG_NAME}`)
    expect(orgLink.getAttribute('href')).toContain(ORG_NAME)
  })

  it('folder-a display name links to /folders/folder-a', () => {
    renderTree()
    const folderLink = screen.getByTestId('tree-link-folder-folder-a')
    expect(folderLink.getAttribute('href')).toContain('folder-a')
  })

  it('project-2 display name links to /projects/project-2', () => {
    renderTree()
    const projectLink = screen.getByTestId('tree-link-project-project-2')
    expect(projectLink.getAttribute('href')).toContain('project-2')
  })

  it('renders an empty tree (org only) when resources is empty', () => {
    render(
      <ResourceTree
        orgName={ORG_NAME}
        resources={[]}
        expanded={new Set()}
        onToggle={vi.fn()}
        organization={ORG_NAME}
      />,
    )
    expect(screen.getByTestId(`tree-row-${ORG_NAME}`)).toBeInTheDocument()
    // No children
    expect(screen.queryByTestId('tree-row-folder-a')).not.toBeInTheDocument()
  })

  it('clicking delete button opens ConfirmDeleteDialog for a folder', () => {
    renderTree()
    const deleteBtn = screen.getByTestId('tree-delete-folder-a')
    fireEvent.click(deleteBtn)
    // Dialog should be open (asserting on "Delete Resource" heading)
    expect(screen.getByText(/delete resource/i)).toBeInTheDocument()
  })
})

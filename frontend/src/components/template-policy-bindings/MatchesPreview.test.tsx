import { render, screen, waitFor } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import type { Mock } from 'vitest'

// jsdom polyfills (match the other binding-form test files).
if (!Element.prototype.hasPointerCapture) {
  Element.prototype.hasPointerCapture = () => false
}
if (!Element.prototype.setPointerCapture) {
  Element.prototype.setPointerCapture = () => {}
}
if (!Element.prototype.releasePointerCapture) {
  Element.prototype.releasePointerCapture = () => {}
}

vi.mock('@/queries/projects', () => ({
  useListProjects: vi.fn(),
  useListProjectsByParent: vi.fn(),
}))

vi.mock('@/queries/deployments', () => ({
  useListDeployments: vi.fn(),
}))

vi.mock('@/queries/templates', async () => {
  const actual = await vi.importActual<typeof import('@/queries/templates')>(
    '@/queries/templates',
  )
  return {
    ...actual,
    useListTemplates: vi.fn(),
  }
})

import { MatchesPreview } from './MatchesPreview'
import { useListProjects, useListProjectsByParent } from '@/queries/projects'
import { useListDeployments } from '@/queries/deployments'
import { useListTemplates } from '@/queries/templates'
import { namespaceForProject } from '@/lib/scope-labels'
import { TemplatePolicyBindingTargetKind } from '@/queries/templatePolicyBindings'

type Template = { name: string; displayName: string; namespace: string }
type Deployment = { name: string; displayName: string }
type Project = { name: string; displayName: string }

function stubLists({
  projects = [],
  perProjectTemplates = {},
  perProjectDeployments = {},
}: {
  projects?: Project[]
  perProjectTemplates?: Record<string, Template[]>
  perProjectDeployments?: Record<string, Deployment[]>
}) {
  ;(useListProjects as Mock).mockImplementation((org: string) => ({
    data: org ? { projects } : undefined,
    isLoading: false,
    isPending: false,
    error: null,
  }))
  ;(useListProjectsByParent as Mock).mockImplementation(
    (org: string, _pt: unknown, parent: string | undefined) => ({
      data: org && parent ? projects : undefined,
      isLoading: false,
      isPending: false,
      error: null,
    }),
  )
  ;(useListTemplates as Mock).mockImplementation((namespace: string) => {
    if (!namespace) return { data: [], isLoading: false, error: null }
    // namespace-to-project-name reverse lookup: the fixture keys by project
    // name so the test controls per-project shape.
    const entry = Object.entries(perProjectTemplates).find(
      ([p]) => namespaceForProject(p) === namespace,
    )
    return {
      data: entry ? entry[1] : [],
      isLoading: false,
      error: null,
    }
  })
  ;(useListDeployments as Mock).mockImplementation((project: string) => {
    return {
      data: project ? (perProjectDeployments[project] ?? []) : [],
      isLoading: false,
      error: null,
    }
  })
}

describe('MatchesPreview', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('expands {project:"*", name:"*"} PROJECT_TEMPLATE across every project', async () => {
    // Fixture: two projects, each with a project-scope template. Wildcard
    // expansion must yield exactly those two entries, dedup'd.
    stubLists({
      projects: [
        { name: 'proj-a', displayName: 'Project A' },
        { name: 'proj-b', displayName: 'Project B' },
      ],
      perProjectTemplates: {
        'proj-a': [
          {
            name: 'ingress',
            displayName: 'Ingress',
            namespace: namespaceForProject('proj-a'),
          },
        ],
        'proj-b': [
          {
            name: 'web',
            displayName: 'Web',
            namespace: namespaceForProject('proj-b'),
          },
        ],
      },
    })

    render(
      <MatchesPreview
        organization="test-org"
        parentScope={{ kind: 'organization' }}
        targets={[
          {
            kind: TemplatePolicyBindingTargetKind.PROJECT_TEMPLATE,
            projectName: '*',
            name: '*',
          },
        ]}
      />,
    )

    await waitFor(() => {
      expect(screen.getByTestId('matches-preview-toggle')).toHaveTextContent(
        /Matches 2 targets/i,
      )
    })
    const list = screen.getByTestId('matches-preview-list')
    expect(list).toHaveTextContent('proj-a/ingress')
    expect(list).toHaveTextContent('proj-b/web')
  })

  it('renders the empty-state warning when zero matches', async () => {
    // No projects at all — wildcard expansion yields nothing.
    stubLists({ projects: [], perProjectTemplates: {} })
    render(
      <MatchesPreview
        organization="test-org"
        parentScope={{ kind: 'organization' }}
        targets={[
          {
            kind: TemplatePolicyBindingTargetKind.PROJECT_TEMPLATE,
            projectName: '*',
            name: '*',
          },
        ]}
      />,
    )
    await waitFor(() => {
      expect(
        screen.getByTestId('matches-preview-empty'),
      ).toBeInTheDocument()
    })
    expect(screen.getByTestId('matches-preview-empty')).toHaveTextContent(
      /No targets match/i,
    )
  })

  it('dedupes overlapping rows (e.g. literal + wildcard both pointing at same target)', async () => {
    stubLists({
      projects: [{ name: 'proj-a', displayName: 'Project A' }],
      perProjectTemplates: {
        'proj-a': [
          {
            name: 'ingress',
            displayName: 'Ingress',
            namespace: namespaceForProject('proj-a'),
          },
        ],
      },
    })
    render(
      <MatchesPreview
        organization="test-org"
        parentScope={{ kind: 'organization' }}
        targets={[
          {
            kind: TemplatePolicyBindingTargetKind.PROJECT_TEMPLATE,
            projectName: 'proj-a',
            name: 'ingress',
          },
          {
            kind: TemplatePolicyBindingTargetKind.PROJECT_TEMPLATE,
            projectName: '*',
            name: '*',
          },
        ]}
      />,
    )
    await waitFor(() => {
      expect(screen.getByTestId('matches-preview-toggle')).toHaveTextContent(
        /Matches 1 target/i,
      )
    })
  })

  it('enumerates DEPLOYMENT wildcards via useListDeployments', async () => {
    stubLists({
      projects: [
        { name: 'proj-a', displayName: 'Project A' },
        { name: 'proj-b', displayName: 'Project B' },
      ],
      perProjectDeployments: {
        'proj-a': [{ name: 'web', displayName: 'Web' }],
        'proj-b': [{ name: 'api', displayName: 'API' }],
      },
    })
    render(
      <MatchesPreview
        organization="test-org"
        parentScope={{ kind: 'organization' }}
        targets={[
          {
            kind: TemplatePolicyBindingTargetKind.DEPLOYMENT,
            projectName: '*',
            name: '*',
          },
        ]}
      />,
    )
    await waitFor(() => {
      expect(screen.getByTestId('matches-preview-toggle')).toHaveTextContent(
        /Matches 2 targets/i,
      )
    })
    expect(screen.getByTestId('matches-preview-list')).toHaveTextContent(
      'proj-a/web',
    )
    expect(screen.getByTestId('matches-preview-list')).toHaveTextContent(
      'proj-b/api',
    )
  })

  it('literal/literal row is verified against the project list (no over-reporting)', async () => {
    // HOL-773 codex review on PR #1084: the preview used to short-circuit
    // literal/literal pairs without checking that the resource actually
    // exists in the chosen project. Now every row goes through the probe,
    // so a typo yields the empty-state warning instead of a phantom match.
    stubLists({
      projects: [{ name: 'proj-a', displayName: 'Project A' }],
      perProjectTemplates: {
        'proj-a': [
          {
            name: 'ingress',
            displayName: 'Ingress',
            namespace: namespaceForProject('proj-a'),
          },
        ],
      },
    })
    render(
      <MatchesPreview
        organization="test-org"
        parentScope={{ kind: 'organization' }}
        targets={[
          {
            kind: TemplatePolicyBindingTargetKind.PROJECT_TEMPLATE,
            projectName: 'proj-a',
            name: 'does-not-exist',
          },
        ]}
      />,
    )
    await waitFor(() => {
      expect(
        screen.getByTestId('matches-preview-empty'),
      ).toBeInTheDocument()
    })
  })

  it('literal/literal row that DOES exist still renders a match (happy path after P2 fix)', async () => {
    stubLists({
      projects: [{ name: 'proj-a', displayName: 'Project A' }],
      perProjectTemplates: {
        'proj-a': [
          {
            name: 'ingress',
            displayName: 'Ingress',
            namespace: namespaceForProject('proj-a'),
          },
        ],
      },
    })
    render(
      <MatchesPreview
        organization="test-org"
        parentScope={{ kind: 'organization' }}
        targets={[
          {
            kind: TemplatePolicyBindingTargetKind.PROJECT_TEMPLATE,
            projectName: 'proj-a',
            name: 'ingress',
          },
        ]}
      />,
    )
    await waitFor(() => {
      expect(screen.getByTestId('matches-preview-toggle')).toHaveTextContent(
        /Matches 1 target/i,
      )
    })
    expect(screen.getByTestId('matches-preview-list')).toHaveTextContent(
      'proj-a/ingress',
    )
  })

  it('folder-scoped preview enumerates via useListProjectsByParent', async () => {
    stubLists({
      projects: [{ name: 'proj-folder-a', displayName: 'Project A' }],
      perProjectTemplates: {
        'proj-folder-a': [
          {
            name: 'ingress',
            displayName: 'Ingress',
            namespace: namespaceForProject('proj-folder-a'),
          },
        ],
      },
    })
    render(
      <MatchesPreview
        organization="test-org"
        parentScope={{ kind: 'folder', folderName: 'team' }}
        targets={[
          {
            kind: TemplatePolicyBindingTargetKind.PROJECT_TEMPLATE,
            projectName: '*',
            name: '*',
          },
        ]}
      />,
    )
    await waitFor(() => {
      expect(useListProjectsByParent).toHaveBeenCalled()
      expect(screen.getByTestId('matches-preview-toggle')).toHaveTextContent(
        /Matches 1 target/i,
      )
    })
  })
})

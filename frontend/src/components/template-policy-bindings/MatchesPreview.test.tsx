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
import { useListProjects } from '@/queries/projects'
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
  perProjectTemplatesError = {},
  perProjectDeploymentsError = {},
  listProjectsError = null,
}: {
  projects?: Project[]
  perProjectTemplates?: Record<string, Template[]>
  perProjectDeployments?: Record<string, Deployment[]>
  perProjectTemplatesError?: Record<string, unknown>
  perProjectDeploymentsError?: Record<string, unknown>
  listProjectsError?: unknown
}) {
  ;(useListProjects as Mock).mockImplementation((org: string) => ({
    data: org && !listProjectsError ? { projects } : undefined,
    isLoading: false,
    isPending: false,
    error: org ? listProjectsError : null,
  }))
  ;(useListTemplates as Mock).mockImplementation((namespace: string) => {
    if (!namespace) return { data: [], isLoading: false, error: null }
    const entry = Object.entries(perProjectTemplates).find(
      ([p]) => namespaceForProject(p) === namespace,
    )
    const errEntry = Object.entries(perProjectTemplatesError).find(
      ([p]) => namespaceForProject(p) === namespace,
    )
    return {
      data: errEntry ? undefined : entry ? entry[1] : [],
      isLoading: false,
      error: errEntry ? errEntry[1] : null,
    }
  })
  ;(useListDeployments as Mock).mockImplementation((project: string) => {
    if (!project) return { data: [], isLoading: false, error: null }
    return {
      data: perProjectDeploymentsError[project]
        ? undefined
        : (perProjectDeployments[project] ?? []),
      isLoading: false,
      error: perProjectDeploymentsError[project] ?? null,
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

  it('folder-scoped binding: literal project is excluded from preview', async () => {
    // Projects are organization-parented only. Folder-scoped bindings no
    // longer enumerate projects by folder, so project targets under folder
    // scope should preview as empty even if a same-named project exists.
    stubLists({
      projects: [{ name: 'in-folder', displayName: 'In Folder' }],
      perProjectTemplates: {
        'in-folder': [
          {
            name: 'ingress',
            displayName: 'Ingress',
            namespace: namespaceForProject('in-folder'),
          },
        ],
        // "out-of-folder" has a template, but the folder's project list
        // doesn't include it — the preview should ignore the data.
        'out-of-folder': [
          {
            name: 'ingress',
            displayName: 'Ingress',
            namespace: namespaceForProject('out-of-folder'),
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
            projectName: 'out-of-folder',
            name: 'ingress',
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

  it('folder-scoped binding: literal formerly in-folder project is empty', async () => {
    stubLists({
      projects: [{ name: 'in-folder', displayName: 'In Folder' }],
      perProjectTemplates: {
        'in-folder': [
          {
            name: 'ingress',
            displayName: 'Ingress',
            namespace: namespaceForProject('in-folder'),
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
            projectName: 'in-folder',
            name: 'ingress',
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

  it('renders the probe error banner when a per-project probe fails', async () => {
    // HOL-773 codex follow-up on PR #1084: if useListTemplates fails for
    // a project the preview must NOT collapse to the empty-state warning.
    // It must surface an explicit error banner so the author knows the
    // match count is incomplete.
    stubLists({
      projects: [{ name: 'proj-a', displayName: 'Project A' }],
      perProjectTemplates: {},
      perProjectTemplatesError: {
        'proj-a': new Error('transient connect error'),
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
      expect(
        screen.getByTestId('matches-preview-error'),
      ).toBeInTheDocument()
    })
    // Empty-state warning must NOT appear when a probe errored —
    // "No targets match" is reserved for a complete, zero-result preview.
    expect(
      screen.queryByTestId('matches-preview-empty'),
    ).not.toBeInTheDocument()
  })

  it('renders the scope-enumeration error banner when useListProjects fails', async () => {
    stubLists({
      projects: [],
      listProjectsError: new Error('transient connect error'),
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
      expect(
        screen.getByTestId('matches-preview-error'),
      ).toBeInTheDocument()
    })
  })

  it('folder-scoped wildcard project preview is empty', async () => {
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
      expect(
        screen.getByTestId('matches-preview-empty'),
      ).toBeInTheDocument()
    })
  })
})

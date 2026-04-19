/**
 * Regression tests for the "Linked Platform Templates" UI section.
 *
 * This file exists solely to prevent the linking section from being removed or
 * conditionally hidden on the create and edit pages.  It covers:
 *   - Empty linkable state (no ancestor templates)
 *   - Populated linkable state (org + folder templates)
 *   - OWNER / EDITOR / VIEWER roles
 *   - Preview integration (linked templates passed to useRenderTemplate)
 *
 * See docs/agents/guardrail-linking-ui.md for the guardrail this enforces.
 */
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

const mockNavigate = vi.fn()

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => ({ projectName: 'test-project', templateName: 'web-app' }),
    }),
    useNavigate: () => mockNavigate,
    Link: ({ children, className, to, params }: { children: React.ReactNode; className?: string; to?: string; params?: Record<string, string> }) => (
      <a href={to} data-params={JSON.stringify(params)} className={className}>{children}</a>
    ),
  }
})

vi.mock('@/queries/templates', () => ({
  useCreateTemplate: vi.fn(),
  useGetTemplate: vi.fn(),
  useUpdateTemplate: vi.fn(),
  useDeleteTemplate: vi.fn(),
  useCloneTemplate: vi.fn(),
  useRenderTemplate: vi.fn(),
  useListLinkableTemplates: vi.fn().mockReturnValue({ data: [], isPending: false }),
  useCheckUpdates: vi.fn().mockReturnValue({ data: [], isPending: false, error: null }),
  useGetProjectTemplatePolicyState: vi.fn().mockReturnValue({ data: undefined, isPending: false, error: null }),
  makeProjectScope: vi.fn().mockReturnValue({ scope: 3, scopeName: 'test-project' }),
  TemplateScope: { UNSPECIFIED: 0, ORGANIZATION: 1, FOLDER: 2, PROJECT: 3 },
  linkableKey: (scope: number | undefined, scopeName: string | undefined, name: string) =>
    `${scope ?? 0}/${scopeName ?? ''}/${name}`,
  parseLinkableKey: (key: string) => {
    const parts = key.split('/')
    return { scope: Number(parts[0]), scopeName: parts[1] ?? '', name: parts.slice(2).join('/') }
  },
}))

vi.mock('@/components/template-updates', () => ({
  UpgradeDialog: () => null,
}))

vi.mock('@/queries/projects', () => ({
  useGetProject: vi.fn(),
}))

vi.mock('@/queries/organizations', () => ({
  useGetOrganization: vi.fn(),
}))

vi.mock('@/hooks/use-debounced-value', () => ({
  useDebouncedValue: vi.fn((value: unknown) => value),
}))

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

import {
  useCreateTemplate,
  useGetTemplate,
  useUpdateTemplate,
  useDeleteTemplate,
  useCloneTemplate,
  useRenderTemplate,
  useListLinkableTemplates,
} from '@/queries/templates'
import { useGetProject } from '@/queries/projects'
import { useGetOrganization } from '@/queries/organizations'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { CreateTemplatePage } from './new'
import { DeploymentTemplateDetailPage } from './$templateName'

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

const mockOrgTemplates = [
  { name: 'reference-grant', displayName: 'Reference Grant', description: 'Default ReferenceGrant for cross-namespace gateway routing', forced: true, scopeRef: { scope: 1, scopeName: 'default' } },
  { name: 'httpbin-platform', displayName: 'HTTPbin Platform', description: 'Platform HTTPRoute for go-httpbin', forced: false, scopeRef: { scope: 1, scopeName: 'default' } },
]
const mockFolderTemplates = [
  { name: 'team-network-policy', displayName: 'Team Network Policy', description: 'Standard NetworkPolicy for team namespaces', forced: false, scopeRef: { scope: 2, scopeName: 'team-a' } },
]
const allLinkable = [...mockOrgTemplates, ...mockFolderTemplates]

const mockTemplate = {
  name: 'web-app',
  project: 'test-project',
  displayName: 'Web App',
  description: 'Standard web application',
  cueTemplate: '// cue template content',
  mandatory: true,
  enabled: true,
  linkedTemplates: [] as Array<{ name: string; scope: number; scopeName: string; versionConstraint?: string }>,
}

// ---------------------------------------------------------------------------
// Setup helpers
// ---------------------------------------------------------------------------

function setupCreateMocks(userRole = Role.OWNER) {
  ;(useCreateTemplate as Mock).mockReturnValue({
    mutateAsync: vi.fn().mockResolvedValue({}),
    isPending: false,
    reset: vi.fn(),
  })
  ;(useRenderTemplate as Mock).mockReturnValue({
    data: undefined,
    error: null,
    isLoading: false,
    isError: false,
  })
  ;(useGetProject as Mock).mockReturnValue({
    data: { name: 'test-project', userRole, organization: 'test-org' },
    isLoading: false,
  })
  ;(useGetOrganization as Mock).mockReturnValue({
    data: { name: 'test-org', gatewayNamespace: '' },
    isPending: false,
    error: null,
  })
}

function setupDetailMocks(userRole = Role.OWNER, templateOverrides?: Partial<typeof mockTemplate>) {
  const template = { ...mockTemplate, ...templateOverrides }
  ;(useGetTemplate as Mock).mockReturnValue({ data: template, isPending: false, error: null })
  ;(useUpdateTemplate as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({}), isPending: false })
  ;(useDeleteTemplate as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({}), isPending: false, error: null, reset: vi.fn() })
  ;(useCloneTemplate as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({ name: 'new-template' }), isPending: false })
  ;(useGetProject as Mock).mockReturnValue({ data: { name: 'test-project', userRole, organization: 'test-org' }, isLoading: false })
  ;(useGetOrganization as Mock).mockReturnValue({ data: { name: 'test-org', gatewayNamespace: '' }, isPending: false, error: null })
  ;(useRenderTemplate as Mock).mockReturnValue({ data: { renderedYaml: '', renderedJson: '' }, error: null, isFetching: false })
}

// ---------------------------------------------------------------------------
// Create page regression tests
// ---------------------------------------------------------------------------

describe('Linking UI regression — CreateTemplatePage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe.each([
    ['OWNER', Role.OWNER],
    ['EDITOR', Role.EDITOR],
    ['VIEWER', Role.VIEWER],
  ] as const)('role=%s', (_label, role) => {
    describe('empty linkable templates', () => {
      beforeEach(() => {
        ;(useListLinkableTemplates as Mock).mockReturnValue({ data: [], isPending: false })
        setupCreateMocks(role)
      })

      it('always renders the "Linked Platform Templates" section', () => {
        render(<CreateTemplatePage />)
        expect(screen.getByText(/linked platform templates/i)).toBeInTheDocument()
      })

      it('shows empty state message when no linkable templates exist', () => {
        render(<CreateTemplatePage />)
        expect(screen.getByText(/no platform templates available to link/i)).toBeInTheDocument()
      })
    })

    describe('populated linkable templates', () => {
      beforeEach(() => {
        ;(useListLinkableTemplates as Mock).mockReturnValue({ data: allLinkable, isPending: false })
        setupCreateMocks(role)
      })

      it('always renders the "Linked Platform Templates" section', () => {
        render(<CreateTemplatePage />)
        expect(screen.getByText(/linked platform templates/i)).toBeInTheDocument()
      })
    })
  })

  describe('OWNER with populated linkable templates', () => {
    beforeEach(() => {
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: allLinkable, isPending: false })
      setupCreateMocks(Role.OWNER)
    })

    it('renders checkboxes for linkable templates', () => {
      render(<CreateTemplatePage />)
      const checkboxes = screen.getAllByRole('checkbox')
      expect(checkboxes.length).toBe(allLinkable.length)
    })

    it('passes selected linked templates to useRenderTemplate for preview', async () => {
      const user = userEvent.setup()
      render(<CreateTemplatePage />)

      // Select a non-mandatory template
      await user.click(screen.getByRole('checkbox', { name: /httpbin platform/i }))

      const calls = (useRenderTemplate as Mock).mock.calls
      const lastCall = calls[calls.length - 1]
      // arg[5] is linkedTemplates
      expect(lastCall[5]).toEqual(
        expect.arrayContaining([
          expect.objectContaining({ name: 'httpbin-platform', scope: 1, scopeName: 'default' }),
        ]),
      )
    })
  })
})

// ---------------------------------------------------------------------------
// Create page inline preview tests
// ---------------------------------------------------------------------------

describe('Create page inline preview — empty state and headings', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    ;(useListLinkableTemplates as Mock).mockReturnValue({ data: [], isPending: false })
    setupCreateMocks(Role.OWNER)
  })

  it('shows "Platform Resources" heading with empty-state message when platformResourcesJson is empty but projectResourcesJson is non-empty', async () => {
    const user = userEvent.setup()
    ;(useRenderTemplate as Mock).mockReturnValue({
      data: {
        platformResourcesJson: '',
        projectResourcesJson: '{"kind":"Deployment"}',
        renderedJson: '',
      },
      error: null,
      isLoading: false,
      isError: false,
    })
    render(<CreateTemplatePage />)

    // Open preview
    await user.click(screen.getByRole('button', { name: /preview/i }))

    // Both headings should be present
    expect(screen.getByText('Platform Resources')).toBeInTheDocument()
    expect(screen.getByText('Project Resources')).toBeInTheDocument()
    // Empty-state message for platform
    expect(screen.getByText('No platform resources rendered by this template.')).toBeInTheDocument()
    // Project resources should be displayed
    expect(screen.getByLabelText('Project Resources JSON')).toHaveTextContent('Deployment')
  })

  it('always shows "Project Resources" heading when per-collection fields are present (not "Rendered JSON")', async () => {
    const user = userEvent.setup()
    ;(useRenderTemplate as Mock).mockReturnValue({
      data: {
        platformResourcesJson: '{"kind":"ReferenceGrant"}',
        projectResourcesJson: '{"kind":"Deployment"}',
        renderedJson: '',
      },
      error: null,
      isLoading: false,
      isError: false,
    })
    render(<CreateTemplatePage />)

    await user.click(screen.getByRole('button', { name: /preview/i }))

    expect(screen.getByText('Platform Resources')).toBeInTheDocument()
    expect(screen.getByText('Project Resources')).toBeInTheDocument()
    expect(screen.queryByText('Rendered JSON')).not.toBeInTheDocument()
    expect(screen.getByLabelText('Platform Resources JSON')).toHaveTextContent('ReferenceGrant')
    expect(screen.getByLabelText('Project Resources JSON')).toHaveTextContent('Deployment')
  })

  it('shows "Project Resources" heading (not "Rendered JSON") when only project resources exist', async () => {
    const user = userEvent.setup()
    ;(useRenderTemplate as Mock).mockReturnValue({
      data: {
        platformResourcesJson: '',
        projectResourcesJson: '{"kind":"ConfigMap"}',
        renderedJson: '',
      },
      error: null,
      isLoading: false,
      isError: false,
    })
    render(<CreateTemplatePage />)

    await user.click(screen.getByRole('button', { name: /preview/i }))

    expect(screen.getByText('Platform Resources')).toBeInTheDocument()
    expect(screen.getByText('Project Resources')).toBeInTheDocument()
    expect(screen.queryByText('Rendered JSON')).not.toBeInTheDocument()
    expect(screen.getByText('No platform resources rendered by this template.')).toBeInTheDocument()
    expect(screen.getByLabelText('Project Resources JSON')).toHaveTextContent('ConfigMap')
  })
})

// ---------------------------------------------------------------------------
// Detail / edit page regression tests
// ---------------------------------------------------------------------------

describe('Linking UI regression — DeploymentTemplateDetailPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe.each([
    ['OWNER', Role.OWNER],
    ['EDITOR', Role.EDITOR],
    ['VIEWER', Role.VIEWER],
  ] as const)('role=%s', (_label, role) => {
    describe('empty linkable templates', () => {
      beforeEach(() => {
        ;(useListLinkableTemplates as Mock).mockReturnValue({ data: [], isPending: false })
        setupDetailMocks(role)
      })

      it('always renders the "Linked Platform Templates" section', () => {
        render(<DeploymentTemplateDetailPage />)
        expect(screen.getByText(/linked platform templates/i)).toBeInTheDocument()
      })

      it('shows empty state message when no linkable templates exist', () => {
        render(<DeploymentTemplateDetailPage />)
        expect(screen.getByText(/no platform templates available to link/i)).toBeInTheDocument()
      })
    })

    describe('populated linkable templates', () => {
      beforeEach(() => {
        ;(useListLinkableTemplates as Mock).mockReturnValue({ data: allLinkable, isPending: false })
        setupDetailMocks(role)
      })

      it('always renders the "Linked Platform Templates" section', () => {
        render(<DeploymentTemplateDetailPage />)
        expect(screen.getByText(/linked platform templates/i)).toBeInTheDocument()
      })
    })
  })

  describe('OWNER with populated linkable templates', () => {
    beforeEach(() => {
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: allLinkable, isPending: false })
      setupDetailMocks(Role.OWNER)
    })

    it('renders checkboxes when edit linked templates dialog is opened', async () => {
      const user = userEvent.setup()
      render(<DeploymentTemplateDetailPage />)
      await user.click(screen.getByRole('button', { name: /edit linked platform templates/i }))
      const dialog = screen.getByRole('dialog')
      const checkboxes = within(dialog).getAllByRole('checkbox')
      expect(checkboxes.length).toBeGreaterThanOrEqual(allLinkable.length)
    })

    it('preview receives linked templates argument via useRenderTemplate', () => {
      setupDetailMocks(Role.OWNER, { linkedTemplates: [{ name: 'reference-grant', scope: 1, scopeName: 'default' }] })
      render(<DeploymentTemplateDetailPage />)

      const calls = (useRenderTemplate as Mock).mock.calls
      const lastCall = calls[calls.length - 1]
      // arg[5] is linkedTemplates
      expect(lastCall[5]).toEqual(
        expect.arrayContaining([
          expect.objectContaining({ name: 'reference-grant', scope: 1, scopeName: 'default' }),
        ]),
      )
    })
  })
})

// ---------------------------------------------------------------------------
// Partial-update regression tests
//
// These tests ensure that each save handler passes ALL current field values to
// the mutation, preventing any field from being silently zeroed out.
// See https://github.com/holos-run/holos-console/issues/895
// ---------------------------------------------------------------------------

describe('Partial-update regression — DeploymentTemplateDetailPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  // HOL-555 removed `mandatory` from the update payload. These tests now
  // only assert the remaining fields.
  describe('handleSaveLinkedTemplates preserves all fields', () => {
    it('passes cueTemplate, displayName, description, and enabled alongside linkedTemplates', async () => {
      const user = userEvent.setup()
      const mutateAsync = vi.fn().mockResolvedValue({})
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: allLinkable, isPending: false })
      setupDetailMocks(Role.OWNER, {
        cueTemplate: '// existing content',
        displayName: 'My Template',
        description: 'My description',
        enabled: true,
      })
      ;(useUpdateTemplate as Mock).mockReturnValue({ mutateAsync, isPending: false })

      render(<DeploymentTemplateDetailPage />)

      // Open the linked templates dialog
      await user.click(screen.getByRole('button', { name: /edit linked platform templates/i }))
      const dialog = screen.getByRole('dialog')

      // Toggle httpbin-platform
      const httpbinCheckbox = within(dialog).getByRole('checkbox', { name: /httpbin platform/i })
      await user.click(httpbinCheckbox)

      // Click Save
      await user.click(within(dialog).getByRole('button', { name: /save/i }))

      expect(mutateAsync).toHaveBeenCalledTimes(1)
      const callArgs = mutateAsync.mock.calls[0][0]
      expect(callArgs.cueTemplate).toBe('// existing content')
      expect(callArgs.displayName).toBe('My Template')
      expect(callArgs.description).toBe('My description')
      expect(callArgs.enabled).toBe(true)
      // Must still include linkedTemplates and updateLinkedTemplates
      expect(callArgs.updateLinkedTemplates).toBe(true)
      expect(callArgs.linkedTemplates).toBeDefined()
    })
  })

  describe('handleSaveDescription preserves all fields', () => {
    it('passes cueTemplate, displayName, and enabled alongside description', async () => {
      const user = userEvent.setup()
      const mutateAsync = vi.fn().mockResolvedValue({})
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: allLinkable, isPending: false })
      setupDetailMocks(Role.OWNER, {
        cueTemplate: '// existing content',
        displayName: 'My Template',
        description: 'Original description',
        enabled: true,
      })
      ;(useUpdateTemplate as Mock).mockReturnValue({ mutateAsync, isPending: false })

      render(<DeploymentTemplateDetailPage />)

      // Open the description edit dialog
      await user.click(screen.getByRole('button', { name: /edit description/i }))
      const dialog = screen.getByRole('dialog')

      // Change description text
      const textarea = within(dialog).getByRole('textbox', { name: /description/i })
      await user.clear(textarea)
      await user.type(textarea, 'Updated description')

      // Click Save
      await user.click(within(dialog).getByRole('button', { name: /save/i }))

      expect(mutateAsync).toHaveBeenCalledTimes(1)
      const callArgs = mutateAsync.mock.calls[0][0]
      expect(callArgs.cueTemplate).toBe('// existing content')
      expect(callArgs.displayName).toBe('My Template')
      expect(callArgs.description).toBe('Updated description')
      expect(callArgs.enabled).toBe(true)
    })
  })

  describe('handleSave preserves all fields', () => {
    it('passes enabled alongside cueTemplate, displayName, and description', async () => {
      const user = userEvent.setup()
      const mutateAsync = vi.fn().mockResolvedValue({})
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: allLinkable, isPending: false })
      setupDetailMocks(Role.OWNER, {
        cueTemplate: '// existing content',
        displayName: 'My Template',
        description: 'My description',
        enabled: true,
      })
      ;(useUpdateTemplate as Mock).mockReturnValue({ mutateAsync, isPending: false })

      render(<DeploymentTemplateDetailPage />)

      // The Save button is inside the CueTemplateEditor — click it
      const saveButton = screen.getByRole('button', { name: /^save$/i })
      await user.click(saveButton)

      expect(mutateAsync).toHaveBeenCalledTimes(1)
      const callArgs = mutateAsync.mock.calls[0][0]
      expect(callArgs.cueTemplate).toBe('// existing content')
      expect(callArgs.displayName).toBe('My Template')
      expect(callArgs.description).toBe('My description')
      expect(callArgs.enabled).toBe(true)
    })
  })
})

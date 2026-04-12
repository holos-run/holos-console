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
  useListLinkableTemplates: vi.fn().mockReturnValue({ data: [], isSuccess: true }),
  useCheckUpdates: vi.fn().mockReturnValue({ data: [], isPending: false, error: null }),
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
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { CreateTemplatePage } from './new'
import { DeploymentTemplateDetailPage } from './$templateName'

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

const mockOrgTemplates = [
  { name: 'reference-grant', displayName: 'Reference Grant', description: 'Default ReferenceGrant for cross-namespace gateway routing', mandatory: true, scopeRef: { scope: 1, scopeName: 'default' } },
  { name: 'httpbin-platform', displayName: 'HTTPbin Platform', description: 'Platform HTTPRoute for go-httpbin', mandatory: false, scopeRef: { scope: 1, scopeName: 'default' } },
]
const mockFolderTemplates = [
  { name: 'team-network-policy', displayName: 'Team Network Policy', description: 'Standard NetworkPolicy for team namespaces', mandatory: false, scopeRef: { scope: 2, scopeName: 'team-a' } },
]
const allLinkable = [...mockOrgTemplates, ...mockFolderTemplates]

const mockTemplate = {
  name: 'web-app',
  project: 'test-project',
  displayName: 'Web App',
  description: 'Standard web application',
  cueTemplate: '// cue template content',
  linkedTemplates: [] as Array<{ name: string; scope: number; scopeName: string }>,
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
    data: { name: 'test-project', userRole },
    isLoading: false,
  })
}

function setupDetailMocks(userRole = Role.OWNER, templateOverrides?: Partial<typeof mockTemplate>) {
  const template = { ...mockTemplate, ...templateOverrides }
  ;(useGetTemplate as Mock).mockReturnValue({ data: template, isPending: false, error: null })
  ;(useUpdateTemplate as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({}), isPending: false })
  ;(useDeleteTemplate as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({}), isPending: false, error: null, reset: vi.fn() })
  ;(useCloneTemplate as Mock).mockReturnValue({ mutateAsync: vi.fn().mockResolvedValue({ name: 'new-template' }), isPending: false })
  ;(useGetProject as Mock).mockReturnValue({ data: { name: 'test-project', userRole }, isLoading: false })
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
        ;(useListLinkableTemplates as Mock).mockReturnValue({ data: [], isSuccess: true })
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
        ;(useListLinkableTemplates as Mock).mockReturnValue({ data: allLinkable, isSuccess: true })
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
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: allLinkable, isSuccess: true })
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
        ;(useListLinkableTemplates as Mock).mockReturnValue({ data: [], isSuccess: true })
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
        ;(useListLinkableTemplates as Mock).mockReturnValue({ data: allLinkable, isSuccess: true })
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
      ;(useListLinkableTemplates as Mock).mockReturnValue({ data: allLinkable, isSuccess: true })
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

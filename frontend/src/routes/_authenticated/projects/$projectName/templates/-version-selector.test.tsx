/**
 * Tests for the version-aware linking dialog (issue #840).
 *
 * Validates that the linking UI shows version selectors for linkable templates
 * that have releases, and correctly sends version_constraint values when saving.
 */
import { render, screen, within, waitFor, fireEvent } from '@testing-library/react'
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

// Mock the Select component to use native HTML elements for testability in jsdom.
vi.mock('@/components/ui/select', () => ({
  Select: ({ value, onValueChange, disabled, children }: { value?: string; onValueChange?: (v: string) => void; disabled?: boolean; children: React.ReactNode }) => (
    <select
      data-testid="version-select"
      value={value ?? ''}
      disabled={disabled}
      onChange={(e) => onValueChange?.(e.target.value)}
    >
      {children}
    </select>
  ),
  SelectTrigger: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  SelectContent: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  SelectItem: ({ value, children }: { value: string; children: React.ReactNode }) => (
    <option value={value}>{children}</option>
  ),
  SelectValue: ({ placeholder }: { placeholder?: string }) => <span>{placeholder}</span>,
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

const releasesForHttpbin = [
  { version: '2.0.0', changelog: 'Breaking change', templateName: 'httpbin-platform' },
  { version: '1.1.0', changelog: 'Minor improvement', templateName: 'httpbin-platform' },
  { version: '1.0.0', changelog: 'Initial release', templateName: 'httpbin-platform' },
]

const linkableWithReleases = [
  {
    name: 'reference-grant',
    displayName: 'Reference Grant',
    description: 'Default ReferenceGrant',
    forced: true,
    scopeRef: { scope: 1, scopeName: 'default' },
    releases: [
      { version: '1.0.0', changelog: 'Initial', templateName: 'reference-grant' },
    ],
  },
  {
    name: 'httpbin-platform',
    displayName: 'HTTPbin Platform',
    description: 'Platform HTTPRoute for go-httpbin',
    forced: false,
    scopeRef: { scope: 1, scopeName: 'default' },
    releases: releasesForHttpbin,
  },
  {
    name: 'team-network-policy',
    displayName: 'Team Network Policy',
    description: 'Standard NetworkPolicy',
    forced: false,
    scopeRef: { scope: 2, scopeName: 'team-a' },
    releases: [], // No releases
  },
]

const mockTemplate = {
  name: 'web-app',
  project: 'test-project',
  displayName: 'Web App',
  description: 'Standard web application',
  cueTemplate: '// cue template content',
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
// Create page version selector tests
// ---------------------------------------------------------------------------

describe('Version selector — CreateTemplatePage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    ;(useListLinkableTemplates as Mock).mockReturnValue({ data: linkableWithReleases, isPending: false })
    setupCreateMocks(Role.OWNER)
  })

  it('renders version selectors for templates with releases', () => {
    render(<CreateTemplatePage />)
    // reference-grant (mandatory, has releases) + httpbin-platform (has releases) = 2
    const selects = screen.getAllByTestId('version-select')
    expect(selects.length).toBe(2)
  })

  it('does not render version selector for templates without releases', () => {
    render(<CreateTemplatePage />)
    // team-network-policy has no releases, so no select for it.
    expect(screen.getByText('Team Network Policy')).toBeInTheDocument()
    // Only 2 selects total (for templates with releases)
    const selects = screen.getAllByTestId('version-select')
    expect(selects.length).toBe(2)
  })

  it('defaults version selector to "Latest (auto-update)"', () => {
    render(<CreateTemplatePage />)
    const selects = screen.getAllByTestId('version-select') as HTMLSelectElement[]
    // When value is empty string, native <select> falls back to first option (__latest__).
    // Both "" and "__latest__" represent "Latest (auto-update)" — this is correct.
    selects.forEach((select) => {
      expect(select.value === '' || select.value === '__latest__').toBe(true)
    })
  })

  it('sends empty version_constraint when "Latest (auto-update)" is selected', async () => {
    const mutateAsync = vi.fn().mockResolvedValue({})
    ;(useCreateTemplate as Mock).mockReturnValue({
      mutateAsync,
      isPending: false,
      reset: vi.fn(),
    })
    const user = userEvent.setup()
    render(<CreateTemplatePage />)

    // Fill required fields
    const displayNameInput = screen.getByLabelText(/display name/i)
    await user.type(displayNameInput, 'My Template')

    // Check httpbin-platform (non-mandatory, has releases)
    await user.click(screen.getByRole('checkbox', { name: /httpbin platform/i }))

    // Leave version as default (Latest)
    await user.click(screen.getByRole('button', { name: /create template/i }))

    await waitFor(() => {
      expect(mutateAsync).toHaveBeenCalledWith(
        expect.objectContaining({
          linkedTemplates: expect.arrayContaining([
            expect.objectContaining({
              name: 'httpbin-platform',
              versionConstraint: '',
            }),
          ]),
        }),
      )
    })
  })

  it('sends exact version when a specific version is selected', async () => {
    const mutateAsync = vi.fn().mockResolvedValue({})
    ;(useCreateTemplate as Mock).mockReturnValue({
      mutateAsync,
      isPending: false,
      reset: vi.fn(),
    })
    const user = userEvent.setup()
    render(<CreateTemplatePage />)

    // Fill required fields
    const displayNameInput = screen.getByLabelText(/display name/i)
    await user.type(displayNameInput, 'My Template')

    // Check httpbin-platform
    await user.click(screen.getByRole('checkbox', { name: /httpbin platform/i }))

    // Select version 1.1.0 using native select change
    const selects = screen.getAllByTestId('version-select') as HTMLSelectElement[]
    // The second select is for httpbin-platform (first is for mandatory reference-grant)
    fireEvent.change(selects[1], { target: { value: '1.1.0' } })

    await user.click(screen.getByRole('button', { name: /create template/i }))

    await waitFor(() => {
      expect(mutateAsync).toHaveBeenCalledWith(
        expect.objectContaining({
          linkedTemplates: expect.arrayContaining([
            expect.objectContaining({
              name: 'httpbin-platform',
              versionConstraint: '1.1.0',
            }),
          ]),
        }),
      )
    })
  })

  // HOL-555: even when a template is `forced` (checkbox locked), its version
  // selector remains enabled so operators can still pin / bump the forced
  // template's version. Only the selection toggle is locked.
  it('version selectors remain enabled for forced templates (HOL-555)', () => {
    render(<CreateTemplatePage />)
    const selects = screen.getAllByTestId('version-select') as HTMLSelectElement[]
    expect(selects[0]).not.toBeDisabled()
    expect(selects[1]).not.toBeDisabled()
  })

  it('version options include all releases in descending order plus Latest', () => {
    render(<CreateTemplatePage />)
    const selects = screen.getAllByTestId('version-select') as HTMLSelectElement[]
    // httpbin-platform select (second) should have: __latest__, 2.0.0, 1.1.0, 1.0.0
    const options = Array.from(selects[1].querySelectorAll('option'))
    expect(options.map((o) => o.value)).toEqual(['__latest__', '2.0.0', '1.1.0', '1.0.0'])
    expect(options[0].textContent).toBe('Latest (auto-update)')
  })
})

// ---------------------------------------------------------------------------
// Detail/edit page version selector tests
// ---------------------------------------------------------------------------

describe('Version selector — DeploymentTemplateDetailPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    ;(useListLinkableTemplates as Mock).mockReturnValue({ data: linkableWithReleases, isPending: false })
  })

  it('renders version selectors in the linked templates edit dialog', async () => {
    setupDetailMocks(Role.OWNER)
    const user = userEvent.setup()
    render(<DeploymentTemplateDetailPage />)

    await user.click(screen.getByRole('button', { name: /edit linked platform templates/i }))
    const dialog = screen.getByRole('dialog')
    // Should have selects for templates with releases
    const selects = within(dialog).getAllByTestId('version-select')
    expect(selects.length).toBe(2) // reference-grant + httpbin-platform
  })

  it('does not render version selector for templates without releases in edit dialog', async () => {
    setupDetailMocks(Role.OWNER)
    const user = userEvent.setup()
    render(<DeploymentTemplateDetailPage />)

    await user.click(screen.getByRole('button', { name: /edit linked platform templates/i }))
    const dialog = screen.getByRole('dialog')
    // team-network-policy has no releases => no select
    const selects = within(dialog).getAllByTestId('version-select')
    expect(selects.length).toBe(2)
  })

  it('pre-populates version selector with existing version constraint when opening edit dialog', async () => {
    setupDetailMocks(Role.OWNER, {
      linkedTemplates: [
        { name: 'httpbin-platform', scope: 1, scopeName: 'default', versionConstraint: '1.1.0' },
      ],
    })
    const user = userEvent.setup()
    render(<DeploymentTemplateDetailPage />)

    await user.click(screen.getByRole('button', { name: /edit linked platform templates/i }))
    const dialog = screen.getByRole('dialog')
    const selects = within(dialog).getAllByTestId('version-select') as HTMLSelectElement[]
    // httpbin-platform select (second) should show 1.1.0
    expect(selects[1].value).toBe('1.1.0')
  })

  it('saves version_constraint when a specific version is selected', async () => {
    const mutateAsync = vi.fn().mockResolvedValue({})
    setupDetailMocks(Role.OWNER, {
      linkedTemplates: [
        { name: 'httpbin-platform', scope: 1, scopeName: 'default', versionConstraint: '' },
      ],
    })
    // Re-set the update mock after setupDetailMocks
    ;(useUpdateTemplate as Mock).mockReturnValue({ mutateAsync, isPending: false })
    const user = userEvent.setup()
    render(<DeploymentTemplateDetailPage />)

    await user.click(screen.getByRole('button', { name: /edit linked platform templates/i }))
    const dialog = screen.getByRole('dialog')

    // Select version 2.0.0 for httpbin-platform
    const selects = within(dialog).getAllByTestId('version-select') as HTMLSelectElement[]
    fireEvent.change(selects[1], { target: { value: '2.0.0' } })

    // Save
    await user.click(within(dialog).getByRole('button', { name: /save/i }))

    await waitFor(() => {
      expect(mutateAsync).toHaveBeenCalledWith(
        expect.objectContaining({
          linkedTemplates: expect.arrayContaining([
            expect.objectContaining({
              name: 'httpbin-platform',
              versionConstraint: '2.0.0',
            }),
          ]),
          updateLinkedTemplates: true,
        }),
      )
    })
  })

  it('defaults to Latest for newly checked templates', async () => {
    setupDetailMocks(Role.OWNER)
    const user = userEvent.setup()
    render(<DeploymentTemplateDetailPage />)

    await user.click(screen.getByRole('button', { name: /edit linked platform templates/i }))
    const dialog = screen.getByRole('dialog')

    // Check httpbin-platform
    await user.click(within(dialog).getByRole('checkbox', { name: /httpbin platform/i }))

    // The version selector should default to Latest (auto-update).
    // With the native select mock, "" maps to __latest__ (first option).
    const selects = within(dialog).getAllByTestId('version-select') as HTMLSelectElement[]
    expect(selects[1].value === '' || selects[1].value === '__latest__').toBe(true)
  })

  // HOL-555: in the edit dialog, version selectors remain enabled even when
  // the template is `forced`, so operators can still pin / bump the forced
  // template's version. Only the selection toggle is locked.
  it('version selectors in edit dialog remain enabled for forced templates (HOL-555)', async () => {
    setupDetailMocks(Role.OWNER)
    const user = userEvent.setup()
    render(<DeploymentTemplateDetailPage />)

    await user.click(screen.getByRole('button', { name: /edit linked platform templates/i }))
    const dialog = screen.getByRole('dialog')
    const selects = within(dialog).getAllByTestId('version-select') as HTMLSelectElement[]
    expect(selects[0]).not.toBeDisabled()
  })

  it('switching from a pinned version to Latest sends empty version_constraint', async () => {
    const mutateAsync = vi.fn().mockResolvedValue({})
    setupDetailMocks(Role.OWNER, {
      linkedTemplates: [
        { name: 'httpbin-platform', scope: 1, scopeName: 'default', versionConstraint: '1.1.0' },
      ],
    })
    ;(useUpdateTemplate as Mock).mockReturnValue({ mutateAsync, isPending: false })
    const user = userEvent.setup()
    render(<DeploymentTemplateDetailPage />)

    await user.click(screen.getByRole('button', { name: /edit linked platform templates/i }))
    const dialog = screen.getByRole('dialog')

    // Change from 1.1.0 back to Latest
    const selects = within(dialog).getAllByTestId('version-select') as HTMLSelectElement[]
    fireEvent.change(selects[1], { target: { value: '__latest__' } })

    // Save
    await user.click(within(dialog).getByRole('button', { name: /save/i }))

    await waitFor(() => {
      expect(mutateAsync).toHaveBeenCalledWith(
        expect.objectContaining({
          linkedTemplates: expect.arrayContaining([
            expect.objectContaining({
              name: 'httpbin-platform',
              versionConstraint: '',
            }),
          ]),
          updateLinkedTemplates: true,
        }),
      )
    })
  })
})

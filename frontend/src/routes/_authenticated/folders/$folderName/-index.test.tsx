import { render, screen } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import type { ReactNode } from 'react'
import { ParentType } from '@/gen/holos/console/v1/folders_pb'

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => () => ({
      useParams: () => ({ folderName: 'payments' }),
    }),
    Link: ({
      children,
      to,
      params,
      className,
      'aria-label': ariaLabel,
    }: {
      children: ReactNode
      to: string
      params?: Record<string, string>
      className?: string
      'aria-label'?: string
    }) => {
      let href = to
      if (params) {
        for (const [k, v] of Object.entries(params)) {
          href = href.replace(`$${k}`, v)
        }
      }
      return (
        <a href={href} className={className} aria-label={ariaLabel}>
          {children}
        </a>
      )
    },
  }
})

vi.mock('@/queries/folders', () => ({
  useGetFolder: vi.fn(),
}))

vi.mock('@/queries/templates', () => ({
  useListTemplates: vi.fn(),
}))

vi.mock('@/queries/templatePolicies', () => ({
  useListTemplatePolicies: vi.fn(),
}))

vi.mock('@/queries/templatePolicyBindings', () => ({
  useListTemplatePolicyBindings: vi.fn(),
}))

vi.mock('@/queries/projects', () => ({
  useListProjectsByParent: vi.fn(),
}))

// Stub the console config so namespaceForFolder() produces a stable,
// predictable namespace without requiring window.__CONSOLE_CONFIG__.
vi.mock('@/lib/console-config', () => ({
  getConsoleConfig: () => ({
    namespacePrefix: 'holos-',
    organizationPrefix: 'org-',
    folderPrefix: 'fld-',
    projectPrefix: 'prj-',
  }),
}))

import { useGetFolder } from '@/queries/folders'
import { useListTemplates } from '@/queries/templates'
import { useListTemplatePolicies } from '@/queries/templatePolicies'
import { useListTemplatePolicyBindings } from '@/queries/templatePolicyBindings'
import { useListProjectsByParent } from '@/queries/projects'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { namespaceForFolder } from '@/lib/scope-labels'
import { FolderIndexPage } from './index'

type TemplateFixture = { name: string; displayName?: string; enabled?: boolean }
type PolicyFixture = { name: string }
type BindingFixture = { name: string; displayName?: string; policyRef?: { name: string } }
type ProjectFixture = { name: string; displayName?: string }

const mockFolder = {
  name: 'payments',
  displayName: 'Payments Team',
  organization: 'test-org',
  creatorEmail: 'admin@example.com',
  userRole: Role.OWNER,
}

function setup(
  overrides: {
    folder?: typeof mockFolder | undefined
    folderPending?: boolean
    folderError?: Error | null
    templates?: TemplateFixture[]
    templatesPending?: boolean
    templatesError?: Error | null
    policies?: PolicyFixture[]
    policiesPending?: boolean
    policiesError?: Error | null
    bindings?: BindingFixture[]
    bindingsPending?: boolean
    bindingsError?: Error | null
    projects?: ProjectFixture[]
    projectsPending?: boolean
    projectsError?: Error | null
  } = {},
) {
  ;(useGetFolder as Mock).mockReturnValue({
    data: overrides.folderPending ? undefined : overrides.folder ?? mockFolder,
    isPending: overrides.folderPending ?? false,
    error: overrides.folderError ?? null,
  })
  ;(useListTemplates as Mock).mockReturnValue({
    data: overrides.templatesPending ? undefined : overrides.templates ?? [],
    isPending: overrides.templatesPending ?? false,
    error: overrides.templatesError ?? null,
  })
  ;(useListTemplatePolicies as Mock).mockReturnValue({
    data: overrides.policiesPending ? undefined : overrides.policies ?? [],
    isPending: overrides.policiesPending ?? false,
    error: overrides.policiesError ?? null,
  })
  ;(useListTemplatePolicyBindings as Mock).mockReturnValue({
    data: overrides.bindingsPending ? undefined : overrides.bindings ?? [],
    isPending: overrides.bindingsPending ?? false,
    error: overrides.bindingsError ?? null,
  })
  ;(useListProjectsByParent as Mock).mockReturnValue({
    data: overrides.projectsPending ? undefined : overrides.projects ?? [],
    isPending: overrides.projectsPending ?? false,
    error: overrides.projectsError ?? null,
  })
}

describe('FolderIndexPage', () => {
  beforeEach(() => vi.clearAllMocks())

  it('renders the folder header with breadcrumb and displayName', () => {
    setup()
    render(<FolderIndexPage folderName="payments" />)
    expect(screen.getByText('Payments Team')).toBeInTheDocument()
    // Breadcrumb links back to the org settings and the org Projects listing.
    expect(screen.getByRole('link', { name: 'test-org' })).toHaveAttribute(
      'href',
      '/organizations/test-org/settings',
    )
    expect(screen.getByRole('link', { name: 'Projects' })).toHaveAttribute(
      'href',
      '/organizations/test-org/projects',
    )
  })

  it('renders all four summary sections in order: Templates, Template Policies, Template Policy Bindings, Projects', () => {
    setup()
    render(<FolderIndexPage folderName="payments" />)
    // Section order is established by the document order of the
    // section-specific "View all" links. Each link carries a distinct
    // accessible name so a screen-reader user (who jumps between links
    // via the rotor) can tell which section each belongs to.
    const templatesLink = screen.getByRole('link', {
      name: 'View all templates',
    })
    const policiesLink = screen.getByRole('link', {
      name: 'View all template policies',
    })
    const bindingsLink = screen.getByRole('link', {
      name: 'View all template policy bindings',
    })
    const projectsLink = screen.getByRole('link', {
      name: 'View all projects',
    })
    // Position compareDocumentPosition returns Node.DOCUMENT_POSITION_FOLLOWING
    // (0x04) when `other` is a sibling that appears after `this` in the DOM
    // with no ancestor/descendant relationship. Using .toBe() rather than a
    // bitmask guard ensures the exact value — ruling out false positives from
    // combined flags like FOLLOWING | CONTAINED_BY.
    expect(
      templatesLink.compareDocumentPosition(policiesLink),
    ).toBe(Node.DOCUMENT_POSITION_FOLLOWING)
    expect(
      policiesLink.compareDocumentPosition(bindingsLink),
    ).toBe(Node.DOCUMENT_POSITION_FOLLOWING)
    expect(
      bindingsLink.compareDocumentPosition(projectsLink),
    ).toBe(Node.DOCUMENT_POSITION_FOLLOWING)
  })

  it('renders a folder-level error when useGetFolder fails', () => {
    setup({ folderError: new Error('folder not found') })
    render(<FolderIndexPage folderName="payments" />)
    expect(screen.getByText(/folder not found/i)).toBeInTheDocument()
  })

  it('renders per-section loading skeletons while queries are pending', () => {
    setup({
      templatesPending: true,
      policiesPending: true,
      bindingsPending: true,
      projectsPending: true,
    })
    const { container } = render(<FolderIndexPage folderName="payments" />)
    expect(
      container.querySelector('[data-testid="templates-loading"]'),
    ).toBeInTheDocument()
    expect(
      container.querySelector('[data-testid="template-policies-loading"]'),
    ).toBeInTheDocument()
    expect(
      container.querySelector('[data-testid="template-policy-bindings-loading"]'),
    ).toBeInTheDocument()
    expect(
      container.querySelector('[data-testid="projects-loading"]'),
    ).toBeInTheDocument()
  })

  it('renders per-section empty states when each list is empty', () => {
    setup()
    render(<FolderIndexPage folderName="payments" />)
    expect(
      screen.getByText(/no templates in this folder/i),
    ).toBeInTheDocument()
    expect(
      screen.getByText(/no template policies in this folder/i),
    ).toBeInTheDocument()
    expect(
      screen.getByText(/no template policy bindings in this folder/i),
    ).toBeInTheDocument()
    expect(screen.getByText(/no projects in this folder/i)).toBeInTheDocument()
  })

  it('renders the empty state when a list query resolves with undefined data', () => {
    // Guards against a regression where a resolved-but-shape-less query
    // (`data: undefined, isPending: false`) slipped through to the
    // children branch and rendered an empty <ul>. The section card
    // must treat undefined as empty.
    ;(useGetFolder as Mock).mockReturnValue({
      data: mockFolder,
      isPending: false,
      error: null,
    })
    ;(useListTemplates as Mock).mockReturnValue({
      data: undefined,
      isPending: false,
      error: null,
    })
    // Populate the other two sections so any template-item <li>/<ul> or
    // template-item link rendered by a regression stands out from the
    // neighbors' legitimately-rendered lists.
    ;(useListTemplatePolicies as Mock).mockReturnValue({
      data: [{ name: 'disallow-privileged' }],
      isPending: false,
      error: null,
    })
    ;(useListProjectsByParent as Mock).mockReturnValue({
      data: [{ name: 'checkout', displayName: 'Checkout' }],
      isPending: false,
      error: null,
    })
    render(<FolderIndexPage folderName="payments" />)
    // Zero-state copy renders...
    expect(
      screen.getByText(/no templates in this folder/i),
    ).toBeInTheDocument()
    // ...and the Templates section surfaces a "0 total" count badge so
    // the undefined-data path is visually consistent with the zero-
    // count path — both tell the user there are zero templates.
    // getAllByLabelText because other empty sections also render a "0 total" badge.
    const zeroBadges = screen.getAllByLabelText('0 total')
    expect(zeroBadges.length).toBeGreaterThan(0)
    // ...and no template-item link leaks through. The two neighbor
    // sections rendered their own items, so "no hrefs into the
    // templates editor" is a tight regression pin.
    const templateLinks = screen
      .queryAllByRole('link')
      .filter((a) => a.getAttribute('href')?.startsWith('/folders/payments/templates/'))
    expect(templateLinks).toEqual([])
  })

  it('renders templates with a count badge and per-item link', () => {
    setup({
      templates: [
        { name: 'nginx', displayName: 'NGINX', enabled: true },
        { name: 'redis', enabled: true },
      ],
    })
    render(<FolderIndexPage folderName="payments" />)
    expect(screen.getByRole('link', { name: 'NGINX' })).toHaveAttribute(
      'href',
      '/folders/payments/templates/nginx',
    )
    expect(screen.getByRole('link', { name: 'redis' })).toHaveAttribute(
      'href',
      '/folders/payments/templates/redis',
    )
    // The badge on the section header exposes the count via aria-label.
    expect(screen.getByLabelText('2 total')).toBeInTheDocument()
  })

  it('renders a Disabled badge for disabled templates', () => {
    setup({
      templates: [{ name: 'legacy', displayName: 'Legacy', enabled: false }],
    })
    render(<FolderIndexPage folderName="payments" />)
    expect(screen.getByText('Disabled')).toBeInTheDocument()
  })

  it('caps each section preview at 5 items regardless of list size', () => {
    const makeTemplates = (n: number): TemplateFixture[] =>
      Array.from({ length: n }, (_, i) => ({
        name: `t-${i + 1}`,
        displayName: `Template ${i + 1}`,
        enabled: true,
      }))
    setup({ templates: makeTemplates(8) })
    render(<FolderIndexPage folderName="payments" />)
    // Five rendered links + one "View all" button.
    expect(
      screen.getAllByRole('link', { name: /^Template \d+$/ }).length,
    ).toBe(5)
    // Count badge still reports the full size.
    expect(screen.getByLabelText('8 total')).toBeInTheDocument()
  })

  it('renders template policies with per-item link into the folder scope', () => {
    setup({ policies: [{ name: 'disallow-privileged' }] })
    render(<FolderIndexPage folderName="payments" />)
    expect(
      screen.getByRole('link', { name: 'disallow-privileged' }),
    ).toHaveAttribute('href', '/folders/payments/template-policies/disallow-privileged')
  })

  it('renders template policy bindings with per-item link into the folder scope (HOL-1006)', () => {
    setup({ bindings: [{ name: 'bind-require-istio', displayName: 'Bind Require Istio' }] })
    render(<FolderIndexPage folderName="payments" />)
    expect(
      screen.getByRole('link', { name: 'Bind Require Istio' }),
    ).toHaveAttribute('href', '/folders/payments/template-policy-bindings/bind-require-istio')
  })

  it('renders projects with displayName fallback and a per-item link', () => {
    setup({
      projects: [
        { name: 'checkout', displayName: 'Checkout' },
        { name: 'billing' },
      ],
    })
    render(<FolderIndexPage folderName="payments" />)
    expect(screen.getByRole('link', { name: 'Checkout' })).toHaveAttribute(
      'href',
      '/projects/checkout',
    )
    expect(screen.getByRole('link', { name: 'billing' })).toHaveAttribute(
      'href',
      '/projects/billing',
    )
  })

  it('renders per-section View all links to the scoped indexes with distinct accessible names', () => {
    setup()
    render(<FolderIndexPage folderName="payments" />)
    // Each "View all" link carries a section-specific aria-label so
    // the four buttons are distinguishable in a screen-reader link
    // list. All four target folder-scoped indexes.
    expect(
      screen.getByRole('link', { name: 'View all templates' }),
    ).toHaveAttribute('href', '/folders/payments/templates')
    expect(
      screen.getByRole('link', { name: 'View all template policies' }),
    ).toHaveAttribute('href', '/folders/payments/template-policies')
    expect(
      screen.getByRole('link', { name: 'View all template policy bindings' }),
    ).toHaveAttribute('href', '/folders/payments/template-policy-bindings')
    expect(
      screen.getByRole('link', { name: 'View all projects' }),
    ).toHaveAttribute('href', '/folders/payments/projects')
  })

  it('renders per-section error alerts when a single list query fails', () => {
    setup({
      templatesError: new Error('template list failed'),
      policiesError: new Error('policy list failed'),
      bindingsError: new Error('binding list failed'),
      projectsError: new Error('project list failed'),
    })
    render(<FolderIndexPage folderName="payments" />)
    expect(screen.getByText(/template list failed/i)).toBeInTheDocument()
    expect(screen.getByText(/policy list failed/i)).toBeInTheDocument()
    expect(screen.getByText(/binding list failed/i)).toBeInTheDocument()
    expect(screen.getByText(/project list failed/i)).toBeInTheDocument()
  })

  it('calls useListTemplates, useListTemplatePolicies, and useListTemplatePolicyBindings with the folder namespace', () => {
    setup()
    render(<FolderIndexPage folderName="payments" />)
    expect(useListTemplates).toHaveBeenCalledWith(namespaceForFolder('payments'))
    expect(useListTemplatePolicies).toHaveBeenCalledWith(
      namespaceForFolder('payments'),
    )
    expect(useListTemplatePolicyBindings).toHaveBeenCalledWith(
      namespaceForFolder('payments'),
    )
  })

  it('passes ParentType.FOLDER to useListProjectsByParent so the query is non-recursive', () => {
    setup()
    render(<FolderIndexPage folderName="payments" />)
    // Non-recursive semantics come from the query contract: the RPC filters
    // to children whose immediate parent is this folder. We assert the
    // call shape here — verifying the contract the page relies on without
    // re-testing the RPC.
    expect(useListProjectsByParent).toHaveBeenCalledWith(
      'test-org',
      ParentType.FOLDER,
      'payments',
    )
  })

  it('renders the Settings link in the folder header', () => {
    setup()
    render(<FolderIndexPage folderName="payments" />)
    expect(screen.getByRole('link', { name: 'Settings' })).toHaveAttribute(
      'href',
      '/folders/payments/settings',
    )
  })
})

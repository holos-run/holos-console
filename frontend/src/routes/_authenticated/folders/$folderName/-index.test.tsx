import { render, screen } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import type React from 'react'

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
      children: React.ReactNode
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
import { useListProjectsByParent } from '@/queries/projects'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { namespaceForFolder } from '@/lib/scope-labels'
import { FolderIndexPage } from './index'

type TemplateFixture = { name: string; displayName?: string; enabled?: boolean }
type PolicyFixture = { name: string }
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
    // Breadcrumb links back to the org and the folders index.
    expect(screen.getByRole('link', { name: 'test-org' })).toHaveAttribute(
      'href',
      '/orgs/test-org/settings',
    )
    expect(screen.getByRole('link', { name: 'Folders' })).toHaveAttribute(
      'href',
      '/orgs/test-org/folders',
    )
  })

  it('renders all three summary sections in order: Templates, Template Policies, Projects', () => {
    setup()
    render(<FolderIndexPage folderName="payments" />)
    // Section order is established by the order of the per-section "View
    // all" links, which target stable, section-specific hrefs.
    const hrefs = screen
      .getAllByRole('link', { name: 'View all' })
      .map((a) => a.getAttribute('href'))
    expect(hrefs).toEqual([
      '/folders/payments/templates',
      '/folders/payments/template-policies',
      '/orgs/test-org/projects',
    ])
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
    expect(screen.getByText(/no projects in this folder/i)).toBeInTheDocument()
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

  it('renders per-section View all links to the scoped indexes', () => {
    setup()
    render(<FolderIndexPage folderName="payments" />)
    const viewAllLinks = screen.getAllByRole('link', { name: 'View all' })
    const hrefs = viewAllLinks.map((a) => a.getAttribute('href'))
    // Template-scope and policy-scope indexes live under /folders/.
    expect(hrefs).toContain('/folders/payments/templates')
    expect(hrefs).toContain('/folders/payments/template-policies')
    // No folder-scoped projects index exists yet; View all for projects
    // falls back to the org-wide projects page. Revisit when a folder-
    // scoped projects index lands.
    expect(hrefs).toContain('/orgs/test-org/projects')
  })

  it('renders per-section error alerts when a single list query fails', () => {
    setup({
      templatesError: new Error('template list failed'),
      policiesError: new Error('policy list failed'),
      projectsError: new Error('project list failed'),
    })
    render(<FolderIndexPage folderName="payments" />)
    expect(screen.getByText(/template list failed/i)).toBeInTheDocument()
    expect(screen.getByText(/policy list failed/i)).toBeInTheDocument()
    expect(screen.getByText(/project list failed/i)).toBeInTheDocument()
  })

  it('calls useListTemplates with the folder namespace, not the folder name', () => {
    setup()
    render(<FolderIndexPage folderName="payments" />)
    expect(useListTemplates).toHaveBeenCalledWith(namespaceForFolder('payments'))
    expect(useListTemplatePolicies).toHaveBeenCalledWith(
      namespaceForFolder('payments'),
    )
  })

  it('passes ParentType.FOLDER to useListProjectsByParent so the query is non-recursive', async () => {
    setup()
    render(<FolderIndexPage folderName="payments" />)
    // Non-recursive semantics come from the query contract: the RPC filters
    // to children whose immediate parent is this folder. We assert the
    // call shape here — verifying the contract the page relies on without
    // re-testing the RPC.
    const { ParentType } = await import('@/gen/holos/console/v1/folders_pb')
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

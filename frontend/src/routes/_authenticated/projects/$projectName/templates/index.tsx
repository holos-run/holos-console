/**
 * Project-scoped unified Templates index — reimplemented on ResourceGrid v1
 * (HOL-859).
 *
 * Shows three template-family kinds together:
 *   Template, TemplatePolicy, TemplatePolicyBinding
 *
 * The grid fans out across the whole org tree via the three useAll*ForOrg
 * hooks so ancestor-scope templates/policies/bindings are discoverable.
 * Default URL state: kind=Template — only Template rows visible. The user
 * can widen to other kinds by toggling the kind filter checkboxes.
 *
 * orgName is derived from the OrgContext (useOrg()), not the URL, because
 * this route is project-scoped.
 */

import { useCallback, useMemo } from 'react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { createClient } from '@connectrpc/connect'
import { useTransport } from '@connectrpc/connect-query'
import { useQueryClient } from '@tanstack/react-query'
import { HelpCircle } from 'lucide-react'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { TemplateService } from '@/gen/holos/console/v1/templates_pb.js'
import { TemplatePolicyService } from '@/gen/holos/console/v1/template_policies_pb.js'
import { TemplatePolicyBindingService } from '@/gen/holos/console/v1/template_policy_bindings_pb.js'
import { ResourceGrid } from '@/components/resource-grid/ResourceGrid'
import type { Row } from '@/components/resource-grid/types'
import { parseGridSearch } from '@/components/resource-grid/url-state'
import type { ResourceGridSearch } from '@/components/resource-grid/types'
import { Button } from '@/components/ui/button'
import { TemplatesHelpPane } from '@/components/templates/TemplatesHelpPane'
import { useGetProject } from '@/queries/projects'
import { useGetOrganization } from '@/queries/organizations'
import { keys } from '@/queries/keys'
import { useAllTemplatesForOrg } from '@/queries/templates'
import { useAllTemplatePoliciesForOrg } from '@/queries/templatePolicies'
import { useAllTemplatePolicyBindingsForOrg } from '@/queries/templatePolicyBindings'
import { useOrg } from '@/lib/org-context'
import {
  resolveTemplateRowHref,
  parentLabelFromNamespace,
  type TemplateKind,
} from '@/lib/template-row-link'
import type { Timestamp } from '@bufbuild/protobuf/wkt'

// ---------------------------------------------------------------------------
// timestampToISOString converts a google.protobuf.Timestamp to an ISO-8601
// string. Returns '' when ts is undefined so callers can unconditionally
// assign the result to createdAt without a separate null-check.
// ---------------------------------------------------------------------------
function timestampToISOString(ts: Timestamp | undefined): string {
  if (!ts) return ''
  return new Date(Number(ts.seconds) * 1000).toISOString()
}

// ---------------------------------------------------------------------------
// Route search — extends ResourceGridSearch with the help pane state
// ---------------------------------------------------------------------------

export interface TemplatesSearch extends ResourceGridSearch {
  /** "1" = help pane open, absent = closed. */
  help?: '1'
}

function parseTemplatesSearch(raw: Record<string, unknown>): TemplatesSearch {
  const base = parseGridSearch(raw)
  const result: TemplatesSearch = { ...base }
  if (raw['help'] === '1') {
    result.help = '1'
  }
  return result
}

// ---------------------------------------------------------------------------
// Route definition
// ---------------------------------------------------------------------------

export const Route = createFileRoute('/_authenticated/projects/$projectName/templates/')({
  validateSearch: parseTemplatesSearch,
  component: ProjectTemplatesIndexRoute,
})

function ProjectTemplatesIndexRoute() {
  const { projectName } = Route.useParams()
  return <ProjectTemplatesIndexPage projectName={projectName} />
}

// ---------------------------------------------------------------------------
// Page component (exported for tests)
// ---------------------------------------------------------------------------

export function ProjectTemplatesIndexPage({
  projectName,
}: {
  projectName: string
}) {
  const search = Route.useSearch() as TemplatesSearch
  const navigate = useNavigate({ from: Route.fullPath })

  // Help pane state — persisted in URL as ?help=1
  const helpOpen = search.help === '1'

  const handleHelpOpenChange = useCallback(
    (open: boolean) => {
      navigate({
        search: (prev) => {
          const next = { ...(prev as TemplatesSearch) }
          if (open) {
            next.help = '1'
          } else {
            delete next.help
          }
          return next
        },
      })
    },
    [navigate],
  )

  // Derive orgName from the OrgContext — the route is project-scoped so there
  // is no $orgName in the URL.
  const { selectedOrg: orgName } = useOrg()

  // Transport and query-client for direct delete calls (multi-namespace).
  const transport = useTransport()
  const queryClient = useQueryClient()

  // Project data — used to determine the user's role for create permissions.
  const { data: project } = useGetProject(projectName)
  // Org data — used to determine org-level ownership for policy/binding creation.
  const { data: org } = useGetOrganization(orgName ?? '')

  const projectRole = project?.userRole ?? Role.VIEWER
  const orgRole = org?.userRole ?? Role.VIEWER

  const canCreateTemplate = projectRole === Role.OWNER || projectRole === Role.EDITOR
  const canCreateOrgResources = orgRole === Role.OWNER

  // Fan-out hooks — all three enabled only when orgName is known.
  const {
    data: templates = [],
    isPending: templatesPending,
    error: templatesError,
  } = useAllTemplatesForOrg(orgName ?? '')

  const {
    data: policies = [],
    isPending: policiesPending,
    error: policiesError,
  } = useAllTemplatePoliciesForOrg(orgName ?? '')

  const {
    data: bindings = [],
    isPending: bindingsPending,
    error: bindingsError,
  } = useAllTemplatePolicyBindingsForOrg(orgName ?? '')

  // Combined loading / error states
  const isLoading = !orgName || templatesPending || policiesPending || bindingsPending
  const firstError = templatesError ?? policiesError ?? bindingsError

  // ---------------------------------------------------------------------------
  // Build rows from all three kinds
  // ---------------------------------------------------------------------------

  const rows: Row[] = useMemo(() => {
    const result: Row[] = []

    // HOL-990 AC1.1: Resource ID shown in the grid is the bare metadata.name.
    // Cross-namespace uniqueness is preserved by parent + name + kind in
    // detailHref; the ID column itself is short, scannable, and matches the
    // resource's metadata.name exactly.
    for (const t of templates) {
      result.push({
        kind: 'Template',
        name: t.name,
        namespace: t.namespace,
        id: t.name,
        parentId: t.namespace,
        parentLabel: parentLabelFromNamespace(t.namespace),
        displayName: t.displayName || t.name,
        description: t.description ?? '',
        createdAt: t.createdAt,
        // Template proto does not yet expose creator_email, but we set the
        // bag so the Creator search field stays consistent across kinds —
        // Template rows simply will not match a creator query.
        extraSearch: { creator: '' },
        detailHref: resolveTemplateRowHref('Template', t.namespace, t.name),
      })
    }

    for (const p of policies) {
      result.push({
        kind: 'TemplatePolicy',
        name: p.name,
        namespace: p.namespace,
        id: p.name,
        parentId: p.namespace,
        parentLabel: parentLabelFromNamespace(p.namespace),
        displayName: p.displayName || p.name,
        description: p.description ?? '',
        createdAt: timestampToISOString(p.createdAt),
        extraSearch: { creator: p.creatorEmail ?? '' },
        detailHref: resolveTemplateRowHref('TemplatePolicy', p.namespace, p.name),
      })
    }

    for (const b of bindings) {
      result.push({
        kind: 'TemplatePolicyBinding',
        name: b.name,
        namespace: b.namespace,
        id: b.name,
        parentId: b.namespace,
        parentLabel: parentLabelFromNamespace(b.namespace),
        displayName: b.displayName || b.name,
        description: b.description ?? '',
        createdAt: timestampToISOString(b.createdAt),
        extraSearch: { creator: b.creatorEmail ?? '' },
        detailHref: resolveTemplateRowHref('TemplatePolicyBinding', b.namespace, b.name),
      })
    }

    return result
  }, [templates, policies, bindings])

  // ---------------------------------------------------------------------------
  // Kind definitions with default URL state: kind=Template
  // ---------------------------------------------------------------------------

  const kinds = useMemo(
    () => [
      {
        id: 'Template',
        label: 'Template',
        newHref: `/projects/${projectName}/templates/new`,
        canCreate: canCreateTemplate,
      },
      {
        id: 'TemplatePolicy',
        label: 'Template Policy',
        newHref: orgName ? `/organizations/${orgName}/template-policies/new` : undefined,
        canCreate: canCreateOrgResources,
      },
      {
        id: 'TemplatePolicyBinding',
        label: 'Template Policy Binding',
        newHref: orgName ? `/organizations/${orgName}/template-bindings/new` : undefined,
        canCreate: canCreateOrgResources,
      },
    ],
    [projectName, orgName, canCreateTemplate, canCreateOrgResources],
  )

  // ---------------------------------------------------------------------------
  // Default URL state: kind=Template
  // Apply default when URL omits it so the initial view shows only
  // the current project's Template rows.
  // ---------------------------------------------------------------------------

  const searchWithDefaults: ResourceGridSearch = useMemo(
    () => ({
      kind: search.kind ?? 'Template',
      search: search.search,
      sort: search.sort,
      sortDir: search.sortDir,
      fields: search.fields,
    }),
    [search],
  )

  // ---------------------------------------------------------------------------
  // Delete handler — dispatches to the correct service per kind/namespace.
  // We call the service directly so we can pass the exact namespace from each
  // row without calling separate per-namespace mutation hooks (which would
  // require a fixed namespace at hook-call time).
  // ---------------------------------------------------------------------------

  const handleDelete = useCallback(
    async (row: Row) => {
      const { namespace, name, kind } = row
      const templateKind = kind as TemplateKind

      switch (templateKind) {
        case 'Template': {
          const client = createClient(TemplateService, transport)
          await client.deleteTemplate({ namespace, name })
          await queryClient.invalidateQueries({
            queryKey: keys.templates.list(namespace),
          })
          break
        }
        case 'TemplatePolicy': {
          const client = createClient(TemplatePolicyService, transport)
          await client.deleteTemplatePolicy({ namespace, name })
          await queryClient.invalidateQueries({
            queryKey: keys.templatePolicies.list(namespace),
          })
          break
        }
        case 'TemplatePolicyBinding': {
          const client = createClient(TemplatePolicyBindingService, transport)
          await client.deleteTemplatePolicyBinding({ namespace, name })
          await queryClient.invalidateQueries({
            queryKey: keys.templatePolicyBindings.list(namespace),
          })
          break
        }
        default:
          throw new Error(`Unknown kind: ${kind}`)
      }
    },
    [transport, queryClient],
  )

  const handleSearchChange = useCallback(
    (updater: (prev: ResourceGridSearch) => ResourceGridSearch) => {
      navigate({
        search: (prev) => {
          const typedPrev = prev as TemplatesSearch
          const updated = updater(typedPrev)
          // Preserve the help param across grid-search updates.
          const next: TemplatesSearch = { ...updated }
          if (typedPrev.help) {
            next.help = typedPrev.help
          }
          return next
        },
      })
    },
    [navigate],
  )

  // Guard: no org selected yet
  if (!orgName) {
    return (
      <div className="flex flex-col items-center gap-3 py-12 text-center">
        <p className="text-muted-foreground">Select an organization to browse templates.</p>
      </div>
    )
  }

  const helpButton = (
    <Button
      variant="ghost"
      size="icon"
      aria-label="Help — Templates overview"
      onClick={() => handleHelpOpenChange(true)}
      data-testid="templates-help-button"
    >
      <HelpCircle className="h-5 w-5" />
    </Button>
  )

  return (
    <>
      <ResourceGrid
        title={`${projectName} / Templates`}
        kinds={kinds}
        rows={rows}
        onDelete={handleDelete}
        isLoading={isLoading}
        error={firstError}
        search={searchWithDefaults}
        onSearchChange={handleSearchChange}
        headerActions={helpButton}
        extraSearchFields={[{ id: 'creator', label: 'Creator' }]}
      />
      <TemplatesHelpPane open={helpOpen} onOpenChange={handleHelpOpenChange} />
    </>
  )
}

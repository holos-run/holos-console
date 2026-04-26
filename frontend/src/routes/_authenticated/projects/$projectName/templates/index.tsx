/**
 * Project-scoped Templates index — refactored to the authoring (clone/edit)
 * cluster (HOL-974).
 * Adopted StandardPageLayout (HOL-1002).
 *
 * Shows only Template rows scoped to the current project namespace. The
 * query key factory (keys.templates.list(namespace)) is shared with the
 * detail/edit page so mutations invalidate both the index and the detail.
 *
 * The New button routes to the clone page (/templates/new) where the user
 * selects an org platform template as the source and gives the clone a name.
 *
 * This page no longer fans out across TemplatePolicy / TemplatePolicyBinding
 * rows. Those are shown in the org-level Templates index
 * (/organizations/$orgName/templates).
 */

import { useCallback, useMemo } from 'react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { HelpCircle } from 'lucide-react'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { StandardPageLayout } from '@/components/page-layout'
import type { Row } from '@/components/resource-grid/types'
import { parseGridSearch } from '@/components/resource-grid/url-state'
import type { ResourceGridSearch } from '@/components/resource-grid/types'
import { Button } from '@/components/ui/button'
import { TemplatesHelpPane } from '@/components/templates/TemplatesHelpPane'
import { useGetProject } from '@/queries/projects'
import { useListTemplates, useDeleteTemplate } from '@/queries/templates'
import { namespaceForProject } from '@/lib/scope-labels'

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

  const namespace = namespaceForProject(projectName)

  // Project data — used to determine the user's role for create permissions.
  const { data: project } = useGetProject(projectName)
  const userRole = project?.userRole ?? Role.VIEWER
  const canCreate = userRole === Role.OWNER || userRole === Role.EDITOR

  // Project-scoped templates list — key shared with detail/edit page.
  const {
    data: templates = [],
    isPending,
    error,
  } = useListTemplates(namespace)

  const deleteMutation = useDeleteTemplate(namespace)

  // ---------------------------------------------------------------------------
  // Build rows
  // ---------------------------------------------------------------------------

  const rows: Row[] = useMemo(
    () =>
      templates.map((t) => ({
        kind: 'Template',
        name: t.name,
        namespace,
        id: t.name,
        parentId: projectName,
        parentLabel: projectName,
        displayName: t.displayName || t.name,
        description: t.description ?? '',
        createdAt: t.createdAt,
        // Template proto does not expose creator_email yet; set the bag so the
        // Creator search field stays consistent across kinds (HOL-990).
        extraSearch: { creator: '' },
        detailHref: `/projects/${projectName}/templates/${t.name}`,
      })),
    [templates, namespace, projectName],
  )

  // ---------------------------------------------------------------------------
  // Kind definitions — single "Template" kind, new = clone page
  // ---------------------------------------------------------------------------

  const kinds = useMemo(
    () => [
      {
        id: 'Template',
        label: 'Template',
        newHref: `/projects/${projectName}/templates/new`,
        canCreate,
      },
    ],
    [projectName, canCreate],
  )

  // ---------------------------------------------------------------------------
  // Handlers
  // ---------------------------------------------------------------------------

  const handleDelete = useCallback(
    async (row: Row) => {
      await deleteMutation.mutateAsync({ name: row.name })
    },
    [deleteMutation],
  )

  // onSearchChange is generic over TemplatesSearch so that the help param is
  // preserved across grid-search updates.
  const handleSearchChange = useCallback(
    (updater: (prev: TemplatesSearch) => TemplatesSearch) => {
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
    <StandardPageLayout<TemplatesSearch>
      titleParts={[projectName, 'Templates']}
      headerActions={helpButton}
      grid={{
        kinds,
        rows,
        onDelete: handleDelete,
        isLoading: isPending,
        error,
        search,
        onSearchChange: handleSearchChange,
        extraSearchFields: [{ id: 'creator', label: 'Creator' }],
      }}
    >
      <TemplatesHelpPane open={helpOpen} onOpenChange={handleHelpOpenChange} />
    </StandardPageLayout>
  )
}

/**
 * Resource Manager — /resource-manager
 *
 * Renders an expand/collapse tree of the currently selected organization's
 * container hierarchy: Organization → Folders → Projects.
 *
 * Tree expansion state is stored in the `expanded` URL search param so links
 * are shareable (e.g. ?expanded=folder-a,folder-b). The org root is always
 * rendered expanded and is not included in the param.
 *
 * Top-right "New" dropdown provides one-click navigation to create
 * Organization, Folder, or Project resources.
 */

import { useCallback, useMemo } from 'react'
import { createFileRoute, Link, useRouter } from '@tanstack/react-router'
import { ChevronDown, Plus } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'

import { useOrg } from '@/lib/org-context'
import { useListResources } from '@/queries/resources'
import { ResourceTree } from '@/components/resource-manager/ResourceTree'
import { buildReturnTo } from '@/lib/return-to'

// ---------------------------------------------------------------------------
// Route search params schema
// ---------------------------------------------------------------------------

export interface ResourceManagerSearch {
  /** Comma-separated list of expanded folder names. */
  expanded?: string
}

function parseResourceManagerSearch(
  raw: Record<string, unknown>,
): ResourceManagerSearch {
  const result: ResourceManagerSearch = {}
  if (typeof raw['expanded'] === 'string' && raw['expanded']) {
    result.expanded = raw['expanded']
  }
  return result
}

export const Route = createFileRoute('/_authenticated/resource-manager/')({
  validateSearch: parseResourceManagerSearch,
  component: ResourceManagerPage,
})

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function parseExpanded(raw: string | undefined): Set<string> {
  if (!raw) return new Set()
  return new Set(
    raw
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean),
  )
}

function serialiseExpanded(expanded: Set<string>): string | undefined {
  if (expanded.size === 0) return undefined
  return Array.from(expanded).join(',')
}

// ---------------------------------------------------------------------------
// Page
// ---------------------------------------------------------------------------

export function ResourceManagerPage() {
  const { selectedOrg } = useOrg()
  const search = Route.useSearch()
  const navigate = Route.useNavigate()

  const expanded = useMemo(() => parseExpanded(search.expanded), [search.expanded])

  const handleToggle = useCallback(
    (folderName: string) => {
      navigate({
        search: (prev) => {
          const next = new Set(expanded)
          if (next.has(folderName)) {
            next.delete(folderName)
          } else {
            next.add(folderName)
          }
          return { ...prev, expanded: serialiseExpanded(next) }
        },
        replace: true,
      })
    },
    [navigate, expanded],
  )

  const { data, isLoading, error } = useListResources(selectedOrg ?? '')

  const resources = data?.resources ?? []

  // --- Loading skeleton ---------------------------------------------------

  if (isLoading && selectedOrg) {
    return (
      <Card>
        <CardHeader className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-2">
          <CardTitle>Resource Manager</CardTitle>
          <NewDropdown orgName={selectedOrg} />
        </CardHeader>
        <CardContent>
          <div className="space-y-2" data-testid="resource-manager-loading">
            {[...Array(3)].map((_, i) => (
              <Skeleton key={i} className="h-10 w-full" />
            ))}
          </div>
        </CardContent>
      </Card>
    )
  }

  // --- No org selected empty state ----------------------------------------

  if (!selectedOrg) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Resource Manager</CardTitle>
        </CardHeader>
        <CardContent>
          <div
            className="flex flex-col items-center gap-3 py-8 text-center"
            data-testid="resource-manager-empty-org"
          >
            <p className="text-muted-foreground">
              Select an organization to view its resources.
            </p>
            <Link to="/organizations">
              <Button size="sm">Go to Organizations</Button>
            </Link>
          </div>
        </CardContent>
      </Card>
    )
  }

  // --- Error state --------------------------------------------------------

  if (error) {
    return (
      <Card>
        <CardHeader className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-2">
          <CardTitle>Resource Manager</CardTitle>
          <NewDropdown orgName={selectedOrg} />
        </CardHeader>
        <CardContent className="pt-6">
          <Alert variant="destructive">
            <AlertDescription>{error.message}</AlertDescription>
          </Alert>
        </CardContent>
      </Card>
    )
  }

  // --- Tree ---------------------------------------------------------------

  return (
    <Card>
      <CardHeader className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-2">
        <CardTitle>Resource Manager</CardTitle>
        <NewDropdown orgName={selectedOrg} />
      </CardHeader>
      <CardContent>
        {/* Column header row */}
        <div className="flex items-center gap-2 pb-1 border-b border-border text-xs text-muted-foreground font-medium">
          <div className="w-5 flex-shrink-0" />
          <div className="flex-1">Name</div>
          <div className="hidden sm:block w-28 text-right">Created At</div>
          <div className="hidden sm:block w-28 text-right">Updated At</div>
          {/* Spacer for icon buttons (Settings + Delete = ~72px) */}
          <div className="w-[72px]" />
        </div>

        <ResourceTree
          orgName={selectedOrg}
          resources={resources}
          expanded={expanded}
          onToggle={handleToggle}
          organization={selectedOrg}
        />
      </CardContent>
    </Card>
  )
}

// ---------------------------------------------------------------------------
// New dropdown
// ---------------------------------------------------------------------------

/**
 * NewDropdown — top-right dropdown for creating new resources.
 *
 * Each item links to the dedicated creation route for that resource type.
 * A `returnTo` search param is included so the creation page can redirect
 * the user back to Resource Manager (preserving the current `?expanded=…`
 * state) after the resource is created.
 */
function NewDropdown({ orgName }: { orgName: string }) {
  const router = useRouter()
  const returnTo = buildReturnTo(router.state.location)

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button size="sm" data-testid="resource-manager-new-button">
          <Plus className="mr-1 h-4 w-4" />
          New
          <ChevronDown className="ml-1 h-4 w-4" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" data-testid="resource-manager-new-menu">
        <DropdownMenuItem asChild data-testid="new-menu-organization">
          <Link to="/organization/new" search={{ returnTo }}>
            Organization
          </Link>
        </DropdownMenuItem>
        <DropdownMenuItem asChild data-testid="new-menu-folder">
          <Link
            to="/folder/new"
            search={orgName ? { orgName, returnTo } : { returnTo }}
          >
            Folder
          </Link>
        </DropdownMenuItem>
        <DropdownMenuItem asChild data-testid="new-menu-project">
          <Link
            to="/project/new"
            search={orgName ? { orgName, returnTo } : { returnTo }}
          >
            Project
          </Link>
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

/**
 * StandardPageLayout — canonical shell for top-level resource list pages
 * (HOL-1002).
 *
 * Owns:
 *  - Title shape (string or title parts joined with " / ")
 *  - Optional breadcrumb items
 *  - Header actions slot (e.g. Templates help-pane toggle)
 *  - Wiring between ResourceGrid v1 and the route's URL search state
 *
 * Design goals:
 *  - Thin abstraction — do NOT reinvent ResourceGrid.
 *  - Generic over `S extends ResourceGridSearch` so callers that extend the
 *    search type (e.g. TemplatesSearch with `?help=1`) keep full type safety.
 *  - ResourceGrid configuration is passed as a typed prop bag.
 *
 * Placement: `frontend/src/components/page-layout/` because the component is
 * a page-shell primitive shared across multiple routes, not a table-specific
 * primitive. It wraps ResourceGrid rather than living alongside it.
 */

import type { ReactNode } from 'react'
import { ResourceGrid } from '@/components/resource-grid/ResourceGrid'
import type { ResourceGridProps } from '@/components/resource-grid/ResourceGrid'
import type { ResourceGridSearch } from '@/components/resource-grid/types'

// ---------------------------------------------------------------------------
// BreadcrumbItem
// ---------------------------------------------------------------------------

/**
 * A single breadcrumb entry. `href` is optional — if absent the item renders
 * as plain text (useful for the current-page crumb).
 */
export interface BreadcrumbItem {
  label: string
  href?: string
}

// ---------------------------------------------------------------------------
// ResourceGridConfig
// ---------------------------------------------------------------------------

/**
 * All ResourceGrid props that the layout manages on behalf of the caller.
 * `title` is deliberately excluded — it is derived from `titleParts`.
 *
 * Generic over `S extends ResourceGridSearch` so callers with extended search
 * types (e.g. `TemplatesSearch`) are not forced to downcast.
 */
export type ResourceGridConfig<S extends ResourceGridSearch = ResourceGridSearch> = Omit<
  ResourceGridProps,
  'title' | 'headerActions' | 'search' | 'onSearchChange'
> & {
  /** The currently active search params from the route's useSearch(). */
  search?: S
  /**
   * Called when the grid needs to update the URL. Signature matches
   * TanStack Router's navigate({ search: updater }) pattern but is generic
   * over S so callers preserve route-specific params (e.g. ?help=1).
   */
  onSearchChange?: (updater: (prev: S) => S) => void
}

// ---------------------------------------------------------------------------
// StandardPageLayoutProps
// ---------------------------------------------------------------------------

/**
 * Props for StandardPageLayout.
 *
 * @template S - The route's search type, must extend ResourceGridSearch.
 *              Defaults to ResourceGridSearch for callers that do not extend it.
 */
export interface StandardPageLayoutProps<S extends ResourceGridSearch = ResourceGridSearch> {
  /**
   * Page title. Provide either a plain `title` string OR `titleParts`.
   * `titleParts` are joined with " / " (e.g. ["projectName", "Secrets"]
   * produces "projectName / Secrets").
   */
  title?: string
  /** Mutually exclusive with `title`. Joined with " / ". */
  titleParts?: string[]
  /**
   * Optional breadcrumb items rendered above the grid card. The last item
   * typically represents the current page and should omit `href`.
   */
  breadcrumbs?: BreadcrumbItem[]
  /**
   * Optional node rendered in the Card header to the left of the New button.
   * Use for icon buttons such as the Templates help-pane toggle.
   */
  headerActions?: ReactNode
  /**
   * Optional content rendered below the breadcrumbs and above the grid card.
   * Use for help panes, banners, or other contextual UI that lives outside
   * the grid card itself.
   */
  children?: ReactNode
  /** ResourceGrid configuration — all props except title and headerActions. */
  grid: ResourceGridConfig<S>
}

// ---------------------------------------------------------------------------
// StandardPageLayout
// ---------------------------------------------------------------------------

/**
 * Renders a standard page shell used by Secrets, Deployments, and Templates
 * list routes. Computes the grid title from `title` or `titleParts`, passes
 * `headerActions` down to ResourceGrid, and bridges the generic search-state
 * updater to the narrower ResourceGridSearch signature required by ResourceGrid.
 */
export function StandardPageLayout<S extends ResourceGridSearch = ResourceGridSearch>({
  title,
  titleParts,
  breadcrumbs,
  headerActions,
  children,
  grid,
}: StandardPageLayoutProps<S>) {
  // Resolve the title string.
  const resolvedTitle =
    title ?? (titleParts ? titleParts.join(' / ') : '')

  // Bridge the generic onSearchChange to the ResourceGridProps signature.
  // ResourceGrid expects (updater: (prev: ResourceGridSearch) => ResourceGridSearch).
  // The caller may have S = TemplatesSearch which extends ResourceGridSearch.
  // The cast through unknown is safe: S extends ResourceGridSearch, and the
  // updater produced by ResourceGrid only reads/writes the ResourceGridSearch
  // subset. TypeScript requires the unknown hop because the two function types
  // are not structurally compatible at the call-site constraint level.
  const onSearchChangeBridged:
    | ((updater: (prev: ResourceGridSearch) => ResourceGridSearch) => void)
    | undefined = grid.onSearchChange
    ? (updater) => {
        grid.onSearchChange!(updater as unknown as (prev: S) => S)
      }
    : undefined

  return (
    <>
      {breadcrumbs && breadcrumbs.length > 0 && (
        <nav aria-label="Breadcrumb" className="mb-4 flex items-center gap-1 text-sm text-muted-foreground">
          {breadcrumbs.map((crumb, idx) => (
            <span key={idx} className="flex items-center gap-1">
              {idx > 0 && <span aria-hidden="true">/</span>}
              {crumb.href ? (
                <a href={crumb.href} className="hover:underline hover:text-foreground transition-colors">
                  {crumb.label}
                </a>
              ) : (
                <span className="text-foreground">{crumb.label}</span>
              )}
            </span>
          ))}
        </nav>
      )}

      <ResourceGrid
        {...grid}
        title={resolvedTitle}
        headerActions={headerActions}
        search={grid.search as ResourceGridSearch | undefined}
        onSearchChange={onSearchChangeBridged}
      />

      {children}
    </>
  )
}

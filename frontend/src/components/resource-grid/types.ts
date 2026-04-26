/**
 * Types for the ResourceGrid v1 component and its consumers.
 *
 * These types are exported so that later phases (Secrets, Deployments,
 * Templates) can import them directly without re-declaring their own shapes.
 */

/**
 * Describes a single resource kind the grid can display. When `canCreate` is
 * true and `newHref` is provided, the grid renders a "New" button (or dropdown
 * entry) that navigates to that href.
 */
export interface Kind {
  /** Stable identifier used in URL ?kind= params and as a React key. */
  id: string
  /** Human-readable label shown in filter chips and dropdown entries. */
  label: string
  /** If provided, the "New" button links here. */
  newHref?: string
  /**
   * Optional search params attached to the New button's link. Lets callers
   * forward context (e.g. `{ orgName, returnTo }`) without owning the link
   * markup themselves.
   */
  newSearch?: Record<string, unknown>
  /** Whether the current user may create resources of this kind. */
  canCreate?: boolean
}

/**
 * A single row in the ResourceGrid. The generic version lets callers attach
 * extra data in `extra` for row-level actions without altering the grid API.
 */
export interface Row {
  /** Must match a `Kind.id` entry passed to `kinds`. */
  kind: string
  /**
   * Kubernetes resource `metadata.name`. Per HOL-990 this MUST be the bare
   * resource name (not a composite "Kind/namespace/name" string), since the
   * grid renders `id` in the Resource ID column.
   */
  name: string
  /** Kubernetes namespace the resource lives in. */
  namespace: string
  /**
   * Identifier displayed in the "Resource ID" column. Per HOL-990 this MUST
   * equal the resource `metadata.name`. The field exists separately from
   * `name` so the grid can keep its existing accessor while callers
   * unambiguously communicate "this is what should appear in the ID cell".
   */
  id: string
  /** Parent resource name / namespace. */
  parentId: string
  /** Human-readable label for the parent (e.g. project or folder name). */
  parentLabel: string
  /** User-facing display name (falls back to `name` if empty). */
  displayName: string
  /** Short description shown in the truncated Description column. */
  description: string
  /** ISO-8601 creation timestamp string. */
  createdAt: string
  /** If provided, the resource ID cell links here, the display name links here, and the full row is clickable. */
  detailHref?: string
  /**
   * Optional bag of search-only string values for hidden fields (e.g. creator
   * email). Keys must match `ExtraSearchField.id` entries passed to
   * `ResourceGrid.extraSearchFields`. These values are never rendered as
   * columns; they only contribute to the global search when their field id is
   * checked in the search-fields filter.
   */
  extraSearch?: Record<string, string>
}

/**
 * Built-in identifier for the always-available key search fields. The grid
 * renders one checkbox per id in the search-fields filter popover.
 */
export type KeySearchFieldId = 'parent' | 'name' | 'displayName'

/** Default search field IDs applied when the URL omits `?fields=`. */
export const DEFAULT_SEARCH_FIELD_IDS: KeySearchFieldId[] = [
  'parent',
  'name',
  'displayName',
]

/**
 * Caller-supplied hidden search field. `id` must match the corresponding key
 * in any `Row.extraSearch` map and is also persisted in the URL as part of
 * `?fields=`. `label` is shown in the search-fields filter popover.
 */
export interface ExtraSearchField {
  id: string
  label: string
}

/**
 * The shape of the URL search params owned by the ResourceGrid.  Consumers
 * extend this type in their route's `validateSearch` to merge grid params with
 * any route-specific params.
 */
export interface ResourceGridSearch {
  /** Comma-separated list of kind IDs to display. Empty = all. */
  kind?: string
  /** Global search string. */
  search?: string
  /** Column ID to sort by. */
  sort?: string
  /** Sort direction: 'asc' or 'desc'. */
  sortDir?: 'asc' | 'desc'
  /**
   * Comma-separated list of search field IDs the global search input applies
   * to. Empty / undefined means "use defaults" (Parent + Name + Display Name).
   * Hidden search fields (like Creator) are opt-in via this param.
   */
  fields?: string
}

/** Props for the multi-kind checkbox filter. */
export interface KindFilterProps {
  kinds: Kind[]
  selectedKindIds: string[]
  onChange: (ids: string[]) => void
}

/** Props for the ResourceGrid search and filter toolbar. */
export interface ResourceGridToolbarProps {
  title: string
  kinds: Kind[]
  selectedKindIds: string[]
  globalFilter: string
  onGlobalFilterChange: (value: string) => void
  onKindIdsChange: (ids: string[]) => void
}

/** Props for the ResourceGrid create button/dropdown. */
export interface NewButtonProps {
  kinds: Kind[]
}

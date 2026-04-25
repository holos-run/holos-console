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
  /** Kubernetes resource name. */
  name: string
  /** Kubernetes namespace the resource lives in. */
  namespace: string
  /** Stable identifier for the resource (e.g. UID or compound key). */
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
}

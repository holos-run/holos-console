/**
 * SharedDependencyBadge renders a visual indicator for Deployments that were
 * materialised by a TemplateDependency or TemplateRequirement reconciler
 * (Phase 5 / Phase 6, HOL-959 / HOL-960).
 *
 * Detection strategy: the singleton naming convention in
 * console/deployments/dependency_reconciler.go deterministically appends a
 * "-shared" suffix to every auto-provisioned singleton Deployment name (e.g.
 * "waypoint-shared", "waypoint-v1-2-3-shared"). No backend changes are
 * required to detect this — the suffix is an invariant of the reconciler.
 *
 * The badge is independently clickable via the `linkHref` prop to navigate to
 * the deployment detail page. The surrounding e.stopPropagation() wrapper
 * prevents the row-level click handler from also firing, per the data-grid
 * conventions in docs/agents/data-grid-conventions.md.
 *
 * HOL-963 (Phase 9).
 */

import { Badge } from '@/components/ui/badge'
import { Link } from '@tanstack/react-router'

export interface SharedDependencyBadgeProps {
  /** Deployment name to inspect for the "-shared" suffix. */
  name: string
  /**
   * Optional href for the originating CRD object. When provided, the badge
   * renders as a TanStack Router Link.
   *
   * NOTE: Phase 9 cannot link to the originating TemplateDependency or
   * TemplateRequirement object because the backend API does not yet expose
   * the `RenderState.spec.dependencies[]` slice over gRPC. The link target is
   * reserved for a future patch that adds a `dependencies` field to the
   * Deployment proto message (deferred AC).
   */
  linkHref?: string
}

/**
 * isSharedDependency returns true when the deployment name ends with "-shared",
 * matching the singleton naming convention from
 * console/deployments/dependency_reconciler.go.
 */
export function isSharedDependency(name: string): boolean {
  return name.endsWith('-shared')
}

/**
 * SharedDependencyBadge renders a "Shared Dep" badge when the deployment name
 * matches the singleton suffix convention. Returns null for non-singleton
 * deployments so callers can use it unconditionally.
 *
 * The badge is wrapped in an e.stopPropagation() handler so it can be placed
 * inside a clickable ResourceGrid row without triggering row navigation.
 */
export function SharedDependencyBadge({ name, linkHref }: SharedDependencyBadgeProps) {
  if (!isSharedDependency(name)) return null

  const badge = (
    <Badge
      data-testid="shared-dependency-badge"
      variant="outline"
      className="text-xs border-blue-300 text-blue-700 dark:border-blue-600 dark:text-blue-300 whitespace-nowrap"
    >
      Shared Dep
    </Badge>
  )

  if (linkHref) {
    return (
      <span onClick={(e) => e.stopPropagation()}>
        <Link to={linkHref} className="hover:opacity-80">
          {badge}
        </Link>
      </span>
    )
  }

  return (
    <span
      onClick={(e) => e.stopPropagation()}
    >
      {badge}
    </span>
  )
}

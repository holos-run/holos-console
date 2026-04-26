/**
 * SharedDependencyBadge renders a visual indicator for Deployments that were
 * materialised by a TemplateDependency or TemplateRequirement reconciler
 * (Phase 5 / Phase 6, HOL-959 / HOL-960).
 *
 * Detection:
 *   - Primary: the `dependencies` array on the proto-side Deployment is
 *     non-empty (HOL-963 deferred AC; populated only on singleton rows by the
 *     handler from the project's RenderState aggregate).
 *   - Fallback: the deterministic "-shared" suffix from
 *     console/deployments/dependency_reconciler.go.
 *
 * The fallback exists so the badge still renders during the brief window
 * between a singleton's creation and the first RenderState write that records
 * its originating edges.
 *
 * When dependency edges are available the badge shows a tooltip naming each
 * originating TemplateDependency / TemplateRequirement (kind, namespace, name).
 * When `linkHref` is provided the badge becomes a TanStack Router Link to the
 * surrounding row's detail page; the link wrapper calls e.stopPropagation() so
 * the row-level click handler does not also fire.
 */

import { Badge } from '@/components/ui/badge'
import { Link } from '@tanstack/react-router'
import type { DeploymentDependency } from '@/gen/holos/console/v1/deployments_pb'

export interface SharedDependencyBadgeProps {
  /** Deployment name; used as a fallback shared-dependency signal. */
  name: string
  /**
   * Resolved dependency edges from the backend (Deployment.dependencies).
   * Drives both visibility (non-empty implies shared) and the tooltip
   * contents listing each originating CRD object.
   */
  dependencies?: DeploymentDependency[]
  /** Optional href for the surrounding row's detail page. */
  linkHref?: string
}

/**
 * isSharedDependency returns true when the deployment name ends with "-shared".
 * Kept exported for callers that still rely on the suffix-based check during
 * the transition.
 */
export function isSharedDependency(name: string): boolean {
  return name.endsWith('-shared')
}

/**
 * formatOriginatingObject returns "Kind: namespace/name" for a dependency edge.
 * Empty fields are tolerated — partial info is still useful to surface.
 */
function formatOriginatingObject(d: DeploymentDependency): string {
  const o = d.originatingObject
  if (!o) return ''
  const ref = [o.namespace, o.name].filter(Boolean).join('/')
  return o.kind ? `${o.kind}: ${ref}` : ref
}

export function SharedDependencyBadge({ name, dependencies, linkHref }: SharedDependencyBadgeProps) {
  const hasDeps = (dependencies?.length ?? 0) > 0
  if (!hasDeps && !isSharedDependency(name)) return null

  // Originating CRD references as a single tooltip string. Using the native
  // `title` attribute keeps the badge testable in jsdom and gives screen
  // readers the info without depending on a hover-driven tooltip.
  const lines = (dependencies ?? [])
    .map(formatOriginatingObject)
    .filter((s) => s.length > 0)
  const title = lines.length > 0 ? `Required by:\n${lines.join('\n')}` : undefined

  const badge = (
    <Badge
      data-testid="shared-dependency-badge"
      variant="outline"
      title={title}
      className="text-xs border-blue-300 text-blue-700 dark:border-blue-600 dark:text-blue-300 whitespace-nowrap"
    >
      Shared Dep
    </Badge>
  )

  const linked = linkHref ? (
    <Link to={linkHref} className="hover:opacity-80">
      {badge}
    </Link>
  ) : (
    badge
  )

  return <span onClick={(e) => e.stopPropagation()}>{linked}</span>
}

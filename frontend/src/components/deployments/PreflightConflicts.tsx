/**
 * PreflightConflicts renders collision and version-conflict details returned
 * by the PreflightCheck RPC (Phase 8, HOL-962) inline on the deployment form.
 * When conflicts are present the Apply button should be disabled; when no
 * conflicts are present this component renders nothing.
 *
 * Used by the Create Deployment form (new.tsx) and the Re-deploy dialog in the
 * Deployment detail page ($deploymentName.tsx). HOL-963 (Phase 9).
 */

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { TriangleAlert } from 'lucide-react'
import type { CollisionDetail, VersionConflictDetail } from '@/gen/holos/console/v1/deployments_pb'

export interface PreflightConflictsProps {
  /** Name collisions returned by PreflightCheck. */
  collisions?: CollisionDetail[]
  /** Version-constraint conflicts returned by PreflightCheck. */
  versionConflicts?: VersionConflictDetail[]
}

/**
 * hasConflicts returns true when the PreflightCheck response contains at least
 * one collision or version conflict.
 */
export function hasConflicts(
  collisions?: CollisionDetail[],
  versionConflicts?: VersionConflictDetail[],
): boolean {
  return (collisions?.length ?? 0) > 0 || (versionConflicts?.length ?? 0) > 0
}

/**
 * PreflightConflicts renders the collision and version-conflict details from a
 * PreflightCheck response. Returns null when both lists are empty so callers
 * can render it unconditionally.
 *
 * Each collision lists: the planned name, the conflicting existing name, and
 * the backend's advice string. Each version conflict lists: the template
 * namespace/name, the incompatible constraints, and the dependent names that
 * contributed them.
 */
export function PreflightConflicts({ collisions = [], versionConflicts = [] }: PreflightConflictsProps) {
  if (!hasConflicts(collisions, versionConflicts)) return null

  return (
    <Alert
      variant="destructive"
      role="alert"
      aria-label="Preflight conflicts"
      data-testid="preflight-conflicts"
    >
      <TriangleAlert className="h-4 w-4" aria-hidden="true" />
      <AlertTitle>Preflight conflicts detected</AlertTitle>
      <AlertDescription>
        <p className="mb-2 text-sm">
          Resolve the conflicts below before applying. The Apply button is
          disabled until all conflicts are cleared.
        </p>

        {collisions.length > 0 && (
          <div className="mb-3">
            <p className="font-medium text-sm mb-1">Name collisions</p>
            <ul className="list-disc list-inside space-y-1 text-sm">
              {collisions.map((c, idx) => (
                <li
                  key={idx}
                  data-testid={`preflight-collision-${c.plannedName}`}
                >
                  <span className="font-mono">{c.plannedName}</span>
                  {' '}conflicts with existing deployment{' '}
                  <span className="font-mono">{c.conflictingName}</span>
                  {c.advice ? (
                    <span className="text-muted-foreground"> — {c.advice}</span>
                  ) : null}
                </li>
              ))}
            </ul>
          </div>
        )}

        {versionConflicts.length > 0 && (
          <div>
            <p className="font-medium text-sm mb-1">Version constraint conflicts</p>
            <ul className="list-disc list-inside space-y-1 text-sm">
              {versionConflicts.map((vc, idx) => (
                <li
                  key={idx}
                  data-testid={`preflight-version-conflict-${vc.templateName}`}
                >
                  Template{' '}
                  <span className="font-mono">{vc.templateNamespace}/{vc.templateName}</span>
                  {' '}has incompatible constraints:{' '}
                  {vc.constraints.map((c, ci) => (
                    <span key={ci}>
                      <span className="font-mono">{c}</span>
                      {ci < vc.constraints.length - 1 ? ', ' : ''}
                    </span>
                  ))}
                  {vc.dependentNames.length > 0 && (
                    <span className="text-muted-foreground">
                      {' '}(from: {vc.dependentNames.join(', ')})
                    </span>
                  )}
                </li>
              ))}
            </ul>
          </div>
        )}
      </AlertDescription>
    </Alert>
  )
}

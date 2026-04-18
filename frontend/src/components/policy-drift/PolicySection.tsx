import { TriangleAlert } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Separator } from '@/components/ui/separator'
import { Skeleton } from '@/components/ui/skeleton'
import { TemplateScope } from '@/gen/holos/console/v1/policy_state_pb'
import type {
  LinkedTemplateRef,
  PolicyState,
} from '@/gen/holos/console/v1/policy_state_pb'

// PolicySection renders the TemplatePolicy drift snapshot for a render
// target — either a Deployment or a project-scope Template — and exposes a
// Reconcile action slot so callers can wire in the appropriate Update
// mutation (useUpdateDeployment / useUpdateTemplate).
//
// IMPORTANT: PolicyState is sourced exclusively from the
// GetDeploymentPolicyState / GetProjectTemplatePolicyState RPCs. Both RPCs
// read drift state from the folder namespace (per the HOL-554 "storage
// isolation: folder-namespace only" design). Never read drift state from a
// project-namespace resource directly — the folder-namespace rendering path
// is the single source of truth, and project-namespace annotations are
// treated as derivable output, not authoritative state.

export interface PolicySectionProps {
  /** Policy state returned by Get*PolicyState. Undefined while loading. */
  state?: PolicyState
  /** True while the policy-state RPC is in flight. */
  isPending?: boolean
  /** Error from the policy-state RPC, if any. */
  error?: Error | null
  /**
   * Optional reconcile action (button). When undefined the section renders
   * drift information but no action — this is how viewer-role users see the
   * section: they can see drift but cannot reconcile.
   */
  reconcileAction?: React.ReactNode
  /**
   * Optional heading override. Defaults to "Policy". Callers can override
   * for instance to "TemplatePolicy" or similar.
   */
  heading?: string
}

/**
 * Formats a LinkedTemplateRef as a human-readable scope-qualified name.
 * Example: "org:acme/base-app", "folder:infra/istio", "project:web/api@>=1.0.0".
 */
function formatRef(ref: LinkedTemplateRef): string {
  const scopeLabel =
    ref.scope === TemplateScope.ORGANIZATION
      ? 'org'
      : ref.scope === TemplateScope.FOLDER
        ? 'folder'
        : ref.scope === TemplateScope.PROJECT
          ? 'project'
          : 'unknown'
  const base = `${scopeLabel}:${ref.scopeName}/${ref.name}`
  return ref.versionConstraint ? `${base}@${ref.versionConstraint}` : base
}

function RefList({ refs, testid }: { refs: LinkedTemplateRef[]; testid: string }) {
  if (refs.length === 0) {
    return (
      <span className="text-sm text-muted-foreground" data-testid={`${testid}-empty`}>
        None
      </span>
    )
  }
  return (
    <ul className="flex flex-col gap-1" data-testid={testid}>
      {refs.map((ref) => (
        <li
          key={`${ref.scope}/${ref.scopeName}/${ref.name}/${ref.versionConstraint ?? ''}`}
          className="font-mono text-xs text-muted-foreground"
        >
          {formatRef(ref)}
        </li>
      ))}
    </ul>
  )
}

export function PolicySection({
  state,
  isPending = false,
  error,
  reconcileAction,
  heading = 'Policy',
}: PolicySectionProps) {
  // Error state — surface the message inline.
  if (error) {
    return (
      <div className="space-y-4" data-testid="policy-section">
        <h3 className="text-sm font-medium">{heading}</h3>
        <Separator />
        <p className="text-sm text-destructive">{error.message}</p>
      </div>
    )
  }

  if (isPending || !state) {
    return (
      <div className="space-y-4" data-testid="policy-section">
        <h3 className="text-sm font-medium">{heading}</h3>
        <Separator />
        <Skeleton className="h-5 w-48" />
        <Skeleton className="h-24 w-full" />
      </div>
    )
  }

  // "Never applied" state — the target has not been rendered through the
  // HOL-567 applied-state path yet, so drift is not meaningful. Show a note
  // so the user understands why no diff is rendered.
  if (!state.hasAppliedState) {
    return (
      <div className="space-y-4" data-testid="policy-section">
        <h3 className="text-sm font-medium">{heading}</h3>
        <Separator />
        <p className="text-sm text-muted-foreground" data-testid="policy-never-applied">
          No applied state recorded yet. Drift will be reported after the next render.
        </p>
        <div>
          <p className="text-xs uppercase tracking-wider text-muted-foreground mb-1">
            Current effective set
          </p>
          <RefList refs={state.currentSet} testid="policy-current-set" />
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-4" data-testid="policy-section">
      <div className="flex items-center justify-between gap-2">
        <h3 className="text-sm font-medium flex items-center gap-2">
          {heading}
          {state.drift ? (
            <PolicyDriftBadge />
          ) : (
            <span
              className="text-xs text-muted-foreground"
              data-testid="policy-in-sync"
            >
              In sync
            </span>
          )}
        </h3>
        {state.drift && reconcileAction ? (
          <div data-testid="policy-reconcile-slot">{reconcileAction}</div>
        ) : null}
      </div>
      <Separator />

      <div className="grid gap-4 sm:grid-cols-2">
        <div>
          <p className="text-xs uppercase tracking-wider text-muted-foreground mb-1">
            Added (policy now requires)
          </p>
          <RefList refs={state.addedRefs} testid="policy-added-refs" />
        </div>
        <div>
          <p className="text-xs uppercase tracking-wider text-muted-foreground mb-1">
            Removed (no longer required)
          </p>
          <RefList refs={state.removedRefs} testid="policy-removed-refs" />
        </div>
      </div>

      <div>
        <p className="text-xs uppercase tracking-wider text-muted-foreground mb-1">
          Current effective set
        </p>
        <RefList refs={state.currentSet} testid="policy-current-set" />
      </div>
    </div>
  )
}

/**
 * PolicyDriftBadge is the shared warning badge rendered on list rows and
 * inside PolicySection. Yellow/amber is used for "attention required" in
 * this codebase (see PhaseBadge PENDING and template update ArrowUpCircle).
 */
export function PolicyDriftBadge({ className }: { className?: string }) {
  return (
    <Badge
      data-testid="policy-drift-badge"
      aria-label="Policy drift"
      className={
        'bg-yellow-100 text-yellow-800 dark:bg-yellow-900 dark:text-yellow-200 border-transparent inline-flex items-center gap-1 ' +
        (className ?? '')
      }
    >
      <TriangleAlert className="h-3 w-3" aria-hidden="true" />
      Policy Drift
    </Badge>
  )
}

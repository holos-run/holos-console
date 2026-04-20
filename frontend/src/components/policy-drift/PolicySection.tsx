import { useEffect, useRef } from 'react'
import { ChevronRight, TriangleAlert } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Separator } from '@/components/ui/separator'
import { Skeleton } from '@/components/ui/skeleton'
import { scopeLabelFromNamespace, scopeNameFromNamespace } from '@/lib/scope-labels'
import type {
  LinkedTemplateRef,
  PolicyState,
} from '@/gen/holos/console/v1/policy_state_pb'

// PolicySection renders the TemplatePolicy drift snapshot for a render
// target — either a Deployment or a project-scope Template — inside a
// native <details>/<summary> disclosure, and exposes a caller-supplied
// Reconcile action slot so both surfaces share identical visual treatment
// and tests. HOL-559 requires the section be collapsible on both detail
// views; this component provides that via the native element so no Radix
// dependency is needed and screen readers get built-in a11y semantics.
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
  /**
   * When true the disclosure is expanded by default. Defaults to "open
   * only when drifted" so the attention signal is visible without the
   * user having to click.
   */
  defaultOpen?: boolean
}

/**
 * Formats a LinkedTemplateRef as a human-readable scope-qualified name.
 * Example: "org:acme/base-app", "folder:infra/istio", "project:web/api@>=1.0.0".
 */
function formatRef(ref: LinkedTemplateRef): string {
  const label = scopeLabelFromNamespace(ref.namespace) ?? 'unknown'
  const scopeName = scopeNameFromNamespace(ref.namespace)
  const base = `${label}:${scopeName}/${ref.name}`
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
          key={`${ref.namespace}/${ref.name}/${ref.versionConstraint ?? ''}`}
          className="font-mono text-xs text-muted-foreground"
        >
          {formatRef(ref)}
        </li>
      ))}
    </ul>
  )
}

/**
 * CollapsibleShell wraps the section in a native <details> disclosure.
 * Using the native element keeps keyboard and screen-reader behavior
 * correct without pulling in a third-party collapsible primitive.
 *
 * Open-state management has to satisfy two requirements simultaneously:
 *   1. The user's toggle must be preserved across parent re-renders.
 *      The deployment detail page polls `useGetDeploymentStatus` every
 *      five seconds; naïvely passing `open={defaultOpen}` would make
 *      React reconcile the attribute back to the initial value on every
 *      poll, stomping a user-initiated collapse/expand.
 *   2. The open default must track `defaultOpen` until the user
 *      interacts. On first mount the section is typically rendered in
 *      its loading state (defaultOpen=false), and only after the
 *      policy-state RPC resolves does `defaultOpen` flip to true for a
 *      drifted target. If the shell captured `open` exactly once on
 *      mount the section would stay collapsed on first paint even when
 *      drift is present, hiding the attention signal until the user
 *      manually expands it.
 *
 * The implementation tracks whether the user has toggled the disclosure
 * via the <details> `onToggle` event + a sentinel ref. While the user
 * has NOT toggled, a useEffect syncs `el.open` to `defaultOpen` whenever
 * `defaultOpen` changes. Once the user toggles, the sentinel flips and
 * subsequent `defaultOpen` changes are ignored — the user's choice wins
 * for the lifetime of the component instance.
 *
 * The Reconcile button is rendered outside the <summary> (absolutely
 * positioned on the right of the header row) so nested interactive
 * elements inside <summary> do not interfere with the native
 * click-to-toggle behavior.
 */
function CollapsibleShell({
  heading,
  summaryExtra,
  headerAction,
  defaultOpen,
  children,
}: {
  heading: string
  summaryExtra?: React.ReactNode
  headerAction?: React.ReactNode
  defaultOpen: boolean
  children: React.ReactNode
}) {
  const detailsRef = useRef<HTMLDetailsElement | null>(null)
  // Tracks whether the user has manually toggled the disclosure. Once
  // true, defaultOpen changes are ignored so the user's preference wins
  // over the prop-driven default.
  const userToggledRef = useRef(false)
  // Suppresses the `onToggle` handler when we set `el.open` ourselves,
  // so our programmatic sync does not count as a user toggle. <details>
  // fires `toggle` on every state change, including attribute writes.
  const suppressToggleRef = useRef(false)

  // Sync `open` to `defaultOpen` until the user interacts. The effect
  // runs every time `defaultOpen` changes, and no-ops after the user
  // toggles. This means: on first mount the disclosure follows the
  // prop; when the policy-state RPC resolves and flips `defaultOpen`
  // from false to true (drift), the disclosure opens automatically;
  // after the user clicks to collapse/expand, the prop stops driving.
  useEffect(() => {
    if (!userToggledRef.current && detailsRef.current && detailsRef.current.open !== defaultOpen) {
      suppressToggleRef.current = true
      detailsRef.current.open = defaultOpen
    }
  }, [defaultOpen])

  const handleToggle = () => {
    // Swallow the synthetic toggle event that fires when we set
    // `el.open` programmatically above; otherwise the first prop-driven
    // sync would mark the disclosure as user-toggled and lock out any
    // subsequent prop-driven auto-open on drift.
    if (suppressToggleRef.current) {
      suppressToggleRef.current = false
      return
    }
    userToggledRef.current = true
  }

  return (
    <details
      ref={detailsRef}
      onToggle={handleToggle}
      className="space-y-4 group relative"
      data-testid="policy-section"
    >
      <summary
        className="flex items-center gap-2 cursor-pointer list-none select-none pr-36"
        data-testid="policy-section-summary"
      >
        <h3 className="text-sm font-medium flex items-center gap-2">
          <ChevronRight
            aria-hidden="true"
            className="h-4 w-4 transition-transform group-open:rotate-90"
          />
          {heading}
          {summaryExtra}
        </h3>
      </summary>
      {headerAction && (
        <div className="absolute top-0 right-0">{headerAction}</div>
      )}
      <Separator />
      {children}
    </details>
  )
}

export function PolicySection({
  state,
  isPending = false,
  error,
  reconcileAction,
  heading = 'Policy',
  defaultOpen,
}: PolicySectionProps) {
  // Error state — surface the message inline. Open by default so the
  // error is visible without requiring the user to click.
  if (error) {
    return (
      <CollapsibleShell heading={heading} defaultOpen={defaultOpen ?? true}>
        <p className="text-sm text-destructive">{error.message}</p>
      </CollapsibleShell>
    )
  }

  if (isPending || !state) {
    return (
      <CollapsibleShell heading={heading} defaultOpen={defaultOpen ?? false}>
        <Skeleton className="h-5 w-48" />
        <Skeleton className="h-24 w-full" />
      </CollapsibleShell>
    )
  }

  // "Never applied" state — the target has not been rendered through the
  // HOL-567 applied-state path yet, so drift is not meaningful. Collapsed
  // by default so it stays visually quiet for the common "new target"
  // case.
  if (!state.hasAppliedState) {
    return (
      <CollapsibleShell heading={heading} defaultOpen={defaultOpen ?? false}>
        <p className="text-sm text-muted-foreground" data-testid="policy-never-applied">
          No applied state recorded yet. Drift will be reported after the next render.
        </p>
        <div>
          <p className="text-xs uppercase tracking-wider text-muted-foreground mb-1">
            Current effective set
          </p>
          <RefList refs={state.currentSet} testid="policy-current-set" />
        </div>
      </CollapsibleShell>
    )
  }

  // Drifted — expand by default so the attention signal is visible
  // without the user having to click. In-sync → collapsed by default.
  const summaryExtra = state.drift ? (
    <PolicyDriftBadge />
  ) : (
    <span className="text-xs text-muted-foreground" data-testid="policy-in-sync">
      In sync
    </span>
  )
  const headerAction =
    state.drift && reconcileAction ? (
      <div data-testid="policy-reconcile-slot">{reconcileAction}</div>
    ) : null
  return (
    <CollapsibleShell
      heading={heading}
      summaryExtra={summaryExtra}
      headerAction={headerAction}
      defaultOpen={defaultOpen ?? state.drift}
    >
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
    </CollapsibleShell>
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

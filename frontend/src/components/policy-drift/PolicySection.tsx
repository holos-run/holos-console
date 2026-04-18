import { useRef } from 'react'
import { ChevronRight, TriangleAlert } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Separator } from '@/components/ui/separator'
import { Skeleton } from '@/components/ui/skeleton'
import { TemplateScope } from '@/gen/holos/console/v1/policy_state_pb'
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

/**
 * CollapsibleShell wraps the section in a native <details> disclosure.
 * Using the native element keeps keyboard and screen-reader behavior
 * correct without pulling in a third-party collapsible primitive.
 *
 * Open state is UNCONTROLLED: we set the initial `open` attribute once on
 * mount via a ref callback and never touch it again from React. This is
 * important because the deployment detail page polls
 * `useGetDeploymentStatus` on a 5-second refetchInterval, which re-renders
 * this component; a controlled `open={defaultOpen}` prop would force the
 * disclosure back to the initial state on every poll, stomping the user's
 * toggle. The ref-callback approach lets the browser own the `open`
 * attribute after mount while still seeding the initial value from props.
 *
 * The Reconcile button is rendered outside the <summary> (absolutely
 * positioned on the right of the header row) so that nested interactive
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
  const initialisedRef = useRef(false)
  const setInitialOpen = (el: HTMLDetailsElement | null) => {
    // Set the initial open attribute exactly once, on the first mount of
    // this component instance. Subsequent re-renders must not touch
    // `el.open` so that the user's toggle is preserved across parent
    // re-renders (e.g. the deployment status poll).
    if (el && !initialisedRef.current) {
      el.open = defaultOpen
      initialisedRef.current = true
    }
  }
  return (
    <details
      ref={setInitialOpen}
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

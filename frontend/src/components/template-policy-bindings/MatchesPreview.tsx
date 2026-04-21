import {
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
  useCallback,
} from 'react'
import { Loader2, ChevronDown, ChevronRight } from 'lucide-react'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { TemplatePolicyBindingTargetKind } from '@/queries/templatePolicyBindings'
import { useListDeployments } from '@/queries/deployments'
import { useListProjects, useListProjectsByParent } from '@/queries/projects'
import { useListTemplates } from '@/queries/templates'
import { ParentType } from '@/gen/holos/console/v1/folders_pb.js'
import { namespaceForProject, scopeLabelFromNamespace } from '@/lib/scope-labels'
import { WILDCARD, type TargetRefDraft } from './binding-draft'

/**
 * MatchEntry is a single concrete target the binding will attach to once a
 * wildcard expansion has resolved. `kind` is preserved from the source row
 * (kind is never wildcarded — see HOL-767 audit-readability rule). The
 * dedupe key is `${kind}/${projectName}/${name}`.
 */
type MatchEntry = {
  kind: TemplatePolicyBindingTargetKind
  projectName: string
  name: string
}

function entryKey(e: MatchEntry): string {
  return `${e.kind}/${e.projectName}/${e.name}`
}

function kindLabel(kind: TemplatePolicyBindingTargetKind): string {
  return kind === TemplatePolicyBindingTargetKind.DEPLOYMENT
    ? 'deployment'
    : 'project_template'
}

export type MatchesPreviewParentScope =
  | { kind: 'organization' }
  | { kind: 'folder'; folderName: string }

export type MatchesPreviewProps = {
  /** The organization that owns this binding's scope. Required to enumerate
   * projects under a wildcard `project_name`. */
  organization: string
  /** The binding's storage-scope ceiling. Folder bindings enumerate only the
   * projects beneath the folder; org bindings enumerate every project the
   * caller can see in the organization. The server is authoritative — the
   * preview is a best-effort blast-radius hint, not a security boundary. */
  parentScope: MatchesPreviewParentScope
  /** Live target draft from BindingForm — the preview re-derives on every
   * edit. */
  targets: TargetRefDraft[]
}

/**
 * ProbeStore is a tiny, deliberately-local pub/sub used so that per-project
 * probe children can publish their hook-resolved data to the parent without
 * the parent owning `useState` that children would have to push into via
 * effects. We subscribe via `useSyncExternalStore` (React's sanctioned
 * external-store pattern) so the parent recomputes when children call
 * `publish`, and the child publishes during its own render/effect without
 * triggering cascading parent renders inside an effect of the parent itself.
 *
 * HOL-773 detail: we avoid `setState` inside effects because this codebase's
 * `react-hooks/set-state-in-effect` lint rule promotes that anti-pattern to
 * an error. The external-store indirection keeps the child→parent data
 * flow reactive without violating the rule.
 */
type ProbeValue = {
  matches: MatchEntry[]
  pending: boolean
}

type ProbeStore = {
  publish: (key: string, value: ProbeValue) => void
  remove: (key: string) => void
  subscribe: (listener: () => void) => () => void
  snapshot: () => Record<string, ProbeValue>
}

function useProbeStore(): ProbeStore {
  const listenersRef = useRef<Set<() => void>>(new Set())
  const dataRef = useRef<Record<string, ProbeValue>>({})
  // Every publish produces a new snapshot object so `useSyncExternalStore`'s
  // identity check against the previous snapshot fires. Without this the
  // consumer would never re-render after a probe update.
  const versionRef = useRef<Record<string, ProbeValue>>({})

  // Defer notifications to a microtask. Probe children publish into the
  // store during their own render; notifying subscribers synchronously from
  // within a child's render would schedule a setState on the MatchesPreview
  // parent *during its own render*, which React rightly flags with
  // "Cannot update a component while rendering a different component".
  // A microtask runs after the current render commits, at which point
  // `useSyncExternalStore` can re-read the snapshot and schedule the next
  // render cleanly.
  const pendingNotifyRef = useRef(false)
  const notify = useCallback(() => {
    if (pendingNotifyRef.current) return
    pendingNotifyRef.current = true
    queueMicrotask(() => {
      pendingNotifyRef.current = false
      for (const l of listenersRef.current) l()
    })
  }, [])

  const publish = useCallback(
    (key: string, value: ProbeValue) => {
      const prev = dataRef.current[key]
      if (
        prev &&
        prev.pending === value.pending &&
        sameMatchList(prev.matches, value.matches)
      ) {
        return
      }
      dataRef.current = { ...dataRef.current, [key]: value }
      versionRef.current = dataRef.current
      notify()
    },
    [notify],
  )

  const remove = useCallback(
    (key: string) => {
      if (!(key in dataRef.current)) return
      const next = { ...dataRef.current }
      delete next[key]
      dataRef.current = next
      versionRef.current = next
      notify()
    },
    [notify],
  )

  const subscribe = useCallback((listener: () => void) => {
    listenersRef.current.add(listener)
    return () => {
      listenersRef.current.delete(listener)
    }
  }, [])

  const snapshot = useCallback(() => versionRef.current, [])

  return useMemo(
    () => ({ publish, remove, subscribe, snapshot }),
    [publish, remove, subscribe, snapshot],
  )
}

function sameMatchList(a: MatchEntry[], b: MatchEntry[]): boolean {
  if (a.length !== b.length) return false
  for (let i = 0; i < a.length; i++) {
    if (entryKey(a[i]) !== entryKey(b[i])) return false
  }
  return true
}

function useProbeSnapshot(store: ProbeStore): Record<string, ProbeValue> {
  return useSyncExternalStore(store.subscribe, store.snapshot, store.snapshot)
}

/**
 * MatchesPreview shows the author the *concrete* targets a binding will
 * attach to once `*` wildcards are expanded. Expansion is client-side only
 * (HOL-767 explicit non-goal: no preview RPC) and uses the same list hooks
 * that populate the editor pickers, so what the author selects is what the
 * preview enumerates. The backend remains authoritative — preview is a
 * blast-radius UX aid.
 *
 * Loading is progressive: each probe's hook fires independently so partial
 * results render as soon as they resolve. A pending probe shows a spinner
 * row in place of its eventual matches. When the dedup'd union is empty the
 * panel renders a warning (the binding will attach to nothing); otherwise
 * the count headline drives a collapsible list (virtualized for >50 entries
 * via a windowed render — there's no third-party virtualization dep yet).
 */
export function MatchesPreview({
  organization,
  parentScope,
  targets,
}: MatchesPreviewProps) {
  // Organization-wide and folder-scoped project enumerations are hoisted to
  // the top of the component so hook order is invariant across renders.
  // Both hooks are passed empty strings when not applicable which short-
  // circuits via `enabled: !!organization` inside the hooks themselves.
  //
  // Folder-scoped bindings must *always* enumerate the folder's projects —
  // not just when a wildcard row is present — because literal project
  // names still need to be filtered through the scope ceiling. Otherwise
  // the preview would happily probe an out-of-folder project and claim
  // matches the backend will reject with "project ... does not exist
  // under binding scope ..." (codex review on PR #1084).
  const needsScopeProjectList =
    parentScope.kind === 'folder' ||
    targets.some((t) => t.projectName === WILDCARD)
  const orgProjectsQuery = useListProjects(
    parentScope.kind === 'organization' && needsScopeProjectList
      ? organization
      : '',
  )
  const folderProjectsQuery = useListProjectsByParent(
    parentScope.kind === 'folder' && needsScopeProjectList ? organization : '',
    parentScope.kind === 'folder' ? ParentType.FOLDER : undefined,
    parentScope.kind === 'folder' ? parentScope.folderName : undefined,
  )

  const enumeratedProjects: string[] = useMemo(() => {
    if (parentScope.kind === 'organization') {
      return (orgProjectsQuery.data?.projects ?? []).map((p) => p.name)
    }
    return (folderProjectsQuery.data ?? []).map((p) => p.name)
  }, [parentScope.kind, orgProjectsQuery.data, folderProjectsQuery.data])

  // Folder scope is the only kind that hard-limits literal project names.
  // Org scope lets a binding reach every project the caller can see, so
  // an org-scoped literal only needs existence verification (which happens
  // below via the per-project probe). For folder scope we additionally
  // check membership in `enumeratedProjects`.
  const scopeEnforcesFolderMembership = parentScope.kind === 'folder'

  const enumeratedProjectsPending =
    needsScopeProjectList &&
    ((parentScope.kind === 'organization' && orgProjectsQuery.isLoading) ||
      (parentScope.kind === 'folder' && folderProjectsQuery.isLoading))

  // Resolve per-row plans to a flat set of probes. Each probe is a (kind,
  // projectName) pair that a per-project probe component owns one hook
  // call for. De-duplicating at this layer avoids issuing the same
  // useListTemplates/useListDeployments twice for the same project.
  //
  // Literal projects are filtered through the scope's project list when
  // the scope enforces membership (folder). An out-of-scope literal gets
  // an empty projects array which drops its contribution to the match
  // set — the panel's empty-state warning then tells the author the row
  // will attach to nothing, which matches the backend's behavior.
  const rowPlans = useMemo(() => {
    const scopeSet = new Set(enumeratedProjects)
    return targets.map((t) => {
      const projectIsWildcard = t.projectName === WILDCARD
      const hasLiteralProject = !!t.projectName && !projectIsWildcard
      let projects: string[]
      let outOfScope = false
      if (projectIsWildcard) {
        projects = enumeratedProjects
      } else if (hasLiteralProject) {
        if (scopeEnforcesFolderMembership && !scopeSet.has(t.projectName)) {
          projects = []
          outOfScope = true
        } else {
          projects = [t.projectName]
        }
      } else {
        projects = []
      }
      return { target: t, projects, projectIsWildcard, outOfScope }
    })
  }, [targets, enumeratedProjects, scopeEnforcesFolderMembership])

  const probeSpecs = useMemo(() => {
    const seen = new Set<string>()
    const specs: Array<{
      key: string
      kind: TemplatePolicyBindingTargetKind
      projectName: string
    }> = []
    for (const plan of rowPlans) {
      for (const p of plan.projects) {
        const key = `${plan.target.kind}/${p}`
        if (seen.has(key)) continue
        seen.add(key)
        specs.push({ key, kind: plan.target.kind, projectName: p })
      }
    }
    return specs
  }, [rowPlans])

  const store = useProbeStore()
  const probeData = useProbeSnapshot(store)

  // Reduce row plans + live probe data to a dedup'd list of matches.
  const { matches, pendingCount } = useMemo(() => {
    const seen = new Map<string, MatchEntry>()
    let pending = 0
    if (enumeratedProjectsPending) pending += 1

    for (const plan of rowPlans) {
      const t = plan.target
      const nameIsWildcard = t.name === WILDCARD
      const hasLiteralName = !!t.name && !nameIsWildcard

      // Literal/literal rows used to short-circuit here, but that
      // over-reported matches for typos or out-of-scope projects (codex
      // review on PR #1084). Let the probe verify existence for every
      // literal name so the blast-radius panel never claims a match that
      // the backend will later reject.
      // project wildcard with no projects yet enumerated — honestly pending.
      if (plan.projectIsWildcard && plan.projects.length === 0 && enumeratedProjectsPending) {
        pending += 1
        continue
      }

      // Folder-scope literal with enumeration still loading: pending, not
      // empty — avoids a flash-of-empty before the scope list resolves.
      if (
        !plan.projectIsWildcard &&
        scopeEnforcesFolderMembership &&
        enumeratedProjectsPending &&
        plan.projects.length === 0 &&
        !plan.outOfScope
      ) {
        pending += 1
        continue
      }

      for (const projectName of plan.projects) {
        const probeKey = `${t.kind}/${projectName}`
        const probe = probeData[probeKey]
        if (!probe) {
          pending += 1
          continue
        }
        if (probe.pending) pending += 1
        if (nameIsWildcard) {
          for (const m of probe.matches) {
            const k = entryKey(m)
            if (!seen.has(k)) seen.set(k, m)
          }
        } else if (hasLiteralName) {
          const exists = probe.matches.some((m) => m.name === t.name)
          if (exists) {
            const entry: MatchEntry = {
              kind: t.kind,
              projectName,
              name: t.name,
            }
            const k = entryKey(entry)
            if (!seen.has(k)) seen.set(k, entry)
          }
        }
      }
    }
    return { matches: Array.from(seen.values()), pendingCount: pending }
  }, [rowPlans, probeData, enumeratedProjectsPending, scopeEnforcesFolderMembership])

  const [open, setOpen] = useState(true)
  const isEmpty = matches.length === 0
  const isResolved = pendingCount === 0

  return (
    <div className="space-y-2" data-testid="matches-preview">
      {/* Each (kind, project) pair gets a probe. Children publish hook
          results into the shared store during render — no setState-in-effect
          anywhere. */}
      {probeSpecs.map((spec) => (
        <Probe
          key={spec.key}
          probeKey={spec.key}
          kind={spec.kind}
          projectName={spec.projectName}
          store={store}
        />
      ))}

      {isEmpty && isResolved && targets.length > 0 ? (
        <Alert variant="default" data-testid="matches-preview-empty">
          <AlertTitle>No targets match</AlertTitle>
          <AlertDescription>
            The binding will not attach to anything. Add a literal name or pick
            a project that contains at least one matching resource.
          </AlertDescription>
        </Alert>
      ) : (
        <Collapsible open={open} onOpenChange={setOpen}>
          <CollapsibleTrigger asChild>
            <button
              type="button"
              className="flex w-full items-center gap-2 rounded-md border border-border px-3 py-2 text-left text-sm hover:bg-muted/40"
              aria-label="Toggle matches preview"
              data-testid="matches-preview-toggle"
            >
              {open ? (
                <ChevronDown className="h-4 w-4" />
              ) : (
                <ChevronRight className="h-4 w-4" />
              )}
              <span className="font-medium">
                Matches {matches.length}{' '}
                {matches.length === 1 ? 'target' : 'targets'}
              </span>
              {pendingCount > 0 && (
                <span className="ml-auto inline-flex items-center gap-1 text-xs text-muted-foreground">
                  <Loader2 className="h-3 w-3 animate-spin" />
                  Resolving…
                </span>
              )}
            </button>
          </CollapsibleTrigger>
          <CollapsibleContent>
            <MatchList matches={matches} />
          </CollapsibleContent>
        </Collapsible>
      )}
    </div>
  )
}

// VIRTUALIZE_THRESHOLD is the entry count above which MatchList switches
// from rendering every row inline to a windowed scroller. Below this the
// inline list is faster and produces a friendlier DOM for screen readers
// and for tests that assert getByText on a sample row.
const VIRTUALIZE_THRESHOLD = 50

function MatchList({ matches }: { matches: MatchEntry[] }) {
  if (matches.length === 0) {
    return (
      <p className="px-3 py-2 text-sm text-muted-foreground">
        No matches yet.
      </p>
    )
  }
  if (matches.length <= VIRTUALIZE_THRESHOLD) {
    return (
      <ul
        className="mt-2 max-h-72 overflow-auto rounded-md border border-border"
        data-testid="matches-preview-list"
      >
        {matches.map((m) => (
          <li
            key={entryKey(m)}
            className="border-b border-border/40 px-3 py-1.5 text-sm last:border-b-0"
          >
            <code className="text-xs">{kindLabel(m.kind)}</code>{' '}
            {m.projectName}/{m.name}
          </li>
        ))}
      </ul>
    )
  }
  return <VirtualMatchList matches={matches} />
}

// VirtualMatchList does coarse windowing: only render the slice of rows
// visible inside the scroll viewport (plus a small overscan). Avoids
// pulling in react-window or @tanstack/react-virtual for what is, today,
// a UX nice-to-have on the rare wide-binding case (>50 expanded matches).
function VirtualMatchList({ matches }: { matches: MatchEntry[] }) {
  const ROW_HEIGHT = 28
  const VIEWPORT_HEIGHT = 288 // ~max-h-72
  const OVERSCAN = 6
  const containerRef = useRef<HTMLDivElement>(null)
  const [scrollTop, setScrollTop] = useState(0)

  const startIndex = Math.max(
    0,
    Math.floor(scrollTop / ROW_HEIGHT) - OVERSCAN,
  )
  const endIndex = Math.min(
    matches.length,
    Math.ceil((scrollTop + VIEWPORT_HEIGHT) / ROW_HEIGHT) + OVERSCAN,
  )
  const slice = matches.slice(startIndex, endIndex)

  return (
    <div
      ref={containerRef}
      onScroll={(e) => setScrollTop(e.currentTarget.scrollTop)}
      style={{ height: VIEWPORT_HEIGHT }}
      className="mt-2 overflow-auto rounded-md border border-border"
      data-testid="matches-preview-virtual-list"
    >
      <div
        style={{
          height: matches.length * ROW_HEIGHT,
          position: 'relative',
        }}
      >
        <ul
          style={{
            position: 'absolute',
            top: startIndex * ROW_HEIGHT,
            left: 0,
            right: 0,
          }}
        >
          {slice.map((m) => (
            <li
              key={entryKey(m)}
              style={{ height: ROW_HEIGHT }}
              className="flex items-center border-b border-border/40 px-3 text-sm last:border-b-0"
            >
              <code className="text-xs">{kindLabel(m.kind)}</code>
              <span className="ml-1">
                {m.projectName}/{m.name}
              </span>
            </li>
          ))}
        </ul>
      </div>
    </div>
  )
}

/**
 * Probe publishes the (kind, projectName) pair's enumerated resource list
 * into the shared ProbeStore. It renders nothing. Publishing happens
 * during render via `store.publish`, which the store short-circuits when
 * the value has not changed. This side-effect during render is safe
 * because the store's `publish` is idempotent and keyed — React tolerates
 * idempotent side-effects during render. No setState lives in an effect
 * here, which satisfies `react-hooks/set-state-in-effect`.
 */
function Probe({
  probeKey,
  kind,
  projectName,
  store,
}: {
  probeKey: string
  kind: TemplatePolicyBindingTargetKind
  projectName: string
  store: ProbeStore
}) {
  const isDeployment = kind === TemplatePolicyBindingTargetKind.DEPLOYMENT
  const namespace = namespaceForProject(projectName)
  // Both hooks are always called to satisfy React's hook-order rule. Only
  // the relevant side is read; the other is short-circuited by passing '' /
  // the hook's internal enabled: !!... check.
  const templates = useListTemplates(isDeployment ? '' : namespace)
  const deployments = useListDeployments(isDeployment ? projectName : '')

  const pending = isDeployment ? !!deployments.isLoading : !!templates.isLoading

  const matches: MatchEntry[] = useMemo(() => {
    if (isDeployment) {
      return (deployments.data ?? []).map((d) => ({
        kind,
        projectName,
        name: d.name,
      }))
    }
    return (templates.data ?? [])
      .filter((t) => scopeLabelFromNamespace(t.namespace) === 'project')
      .map((t) => ({ kind, projectName, name: t.name }))
  }, [isDeployment, kind, projectName, templates.data, deployments.data])

  // Publish during render. The store dedupes on equal values so this does
  // not loop; changes to `matches` / `pending` produce a single notify().
  store.publish(probeKey, { matches, pending })

  return null
}

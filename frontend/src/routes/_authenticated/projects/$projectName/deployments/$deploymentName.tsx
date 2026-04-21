import { useState, useEffect } from 'react'
import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'
import { toast } from 'sonner'
import { StringListInput } from '@/components/string-list-input'
import { EnvVarEditor, filterEnvVars } from '@/components/env-var-editor'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'
import { Separator } from '@/components/ui/separator'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { ArrowLeft, CheckCircle2, ExternalLink, Info, TriangleAlert, XCircle } from 'lucide-react'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import type { EnvVar, Event, ContainerStatus, Link as DeploymentLink } from '@/gen/holos/console/v1/deployments_pb'
import { useGetDeployment, useGetDeploymentStatus, useGetDeploymentLogs, useGetDeploymentPolicyState, useUpdateDeployment, useDeleteDeployment } from '@/queries/deployments'
import { useGetProject } from '@/queries/projects'
import { isSafeHttpUrl } from '@/lib/url'
import { PolicySection } from '@/components/policy-drift/PolicySection'

type DeploymentTab = 'status' | 'logs'

function validateTab(value: unknown): DeploymentTab {
  if (value === 'logs') return value
  return 'status'
}

export const Route = createFileRoute('/_authenticated/projects/$projectName/deployments/$deploymentName')({
  validateSearch: (search): { tab: DeploymentTab } => ({
    tab: validateTab(search.tab),
  }),
  component: DeploymentDetailRoute,
})

/**
 * Converts a protobuf Timestamp to a human-readable relative age string.
 * Matches kubectl describe output style: "45s", "3m", "28m", "2h", "3d".
 */
function relativeAge(timestamp: { seconds: bigint; nanos: number } | undefined): string {
  if (!timestamp) return ''
  const nowMs = Date.now()
  const thenMs = Number(timestamp.seconds) * 1000
  const diffS = Math.floor((nowMs - thenMs) / 1000)
  if (diffS < 0) return '0s'
  if (diffS < 60) return `${diffS}s`
  if (diffS < 3600) return `${Math.floor(diffS / 60)}m`
  if (diffS < 86400) return `${Math.floor(diffS / 3600)}h`
  return `${Math.floor(diffS / 86400)}d`
}

/**
 * Formats the age column for an event, including count if > 1.
 * Example: "3m (x4 over 28m)" or just "28m" for count=1.
 */
function formatEventAge(event: Event): string {
  const age = relativeAge(event.lastSeen)
  if (event.count > 1) {
    const totalSpan = relativeAge(event.firstSeen)
    return `${age} (x${event.count} over ${totalSpan})`
  }
  return age
}

/**
 * Known error reasons for containers in waiting or terminated states.
 * Normal transient reasons like ContainerCreating and PodInitializing are
 * excluded so they are not visually highlighted as errors during startup.
 */
const CONTAINER_ERROR_REASONS = new Set([
  'ImagePullBackOff',
  'ErrImagePull',
  'CrashLoopBackOff',
  'CreateContainerError',
  'InvalidImageName',
  'CreateContainerConfigError',
  'RunContainerError',
  'OOMKilled',
])

/** Returns true if the container status represents an error condition. */
function isContainerError(cs: ContainerStatus): boolean {
  if (CONTAINER_ERROR_REASONS.has(cs.reason)) return true
  if (cs.state === 'terminated' && cs.restartCount > 0) return true
  return false
}

/**
 * Returns the display text for an external link. Prefers the
 * template-authored title, falls back to the annotation-suffix `name`, and
 * finally to the URL host so the anchor never renders as an empty string.
 * The host fallback uses `URL` parsing — callers are expected to have
 * already gated the URL through `isSafeHttpUrl`, so a parse failure here
 * means the value is malformed and the caller should not have asked.
 */
function linkDisplayText(link: { url: string; title: string; name: string }): string {
  if (link.title) return link.title
  if (link.name) return link.name
  try {
    return new URL(link.url).host
  } catch {
    return link.url
  }
}

/**
 * Single anchor row inside DeploymentLinksSection. Used for every entry in
 * `output.links` so anchor attributes (`target=_blank`,
 * `rel=noopener noreferrer`), the description tooltip, and the ArgoCD
 * source pill are wired identically across both Holos- and ArgoCD-sourced
 * links. The `data-testid="deployment-link-row-<name>"` attribute lets
 * tests target a specific row and assert on rendered indicators (e.g.,
 * the "argocd" pill) without depending on text proximity.
 */
function DeploymentLinkRow({
  href,
  text,
  description,
  source,
  testId,
}: {
  href: string
  text: string
  description: string
  source: string
  testId: string
}) {
  return (
    <div
      data-testid={testId}
      className="flex items-center gap-2 text-sm"
    >
      <a
        href={href}
        target="_blank"
        rel="noopener noreferrer"
        // Native title attribute provides a tooltip for both screen readers
        // and bare-DOM consumers when description is present. Set to
        // undefined when empty so the attribute does not appear at all.
        title={description || undefined}
        className="inline-flex items-center gap-1 underline-offset-4 hover:underline break-all"
      >
        <span>{text}</span>
        <ExternalLink aria-hidden="true" className="h-3.5 w-3.5 shrink-0" />
      </a>
      {/*
        Source pill — rendered only for ArgoCD-sourced links so operators
        can tell at a glance which annotation family produced the entry.
        Holos-sourced links are the implicit default and stay unbadged to
        keep the row scannable when most templates author exclusively
        through `console.holos.run/external-link.*`.
      */}
      {source === 'argocd' ? (
        <Badge variant="outline" className="text-xs uppercase tracking-wide">
          argocd
        </Badge>
      ) : null}
    </div>
  )
}

/**
 * Renders the Status-tab Links section described in HOL-575. The section is
 * a thin wrapper around `DeploymentLinkRow` that walks `output.links` in
 * the order the backend supplied them — the aggregator already sorts by
 * (name, source) so the wire order is deterministic and the UI does not
 * need to re-sort. Returns `null` when no link survives the scheme
 * allowlist so the section is hidden entirely (matching the acceptance
 * criterion that an empty `output.links` produces no DOM).
 *
 * Sourced from `deployment.statusSummary.output.links` so live-resource
 * link annotations harvested by the HOL-573 / HOL-574 aggregator surface
 * here.
 *
 * The primary URL is intentionally NOT included here — it continues to
 * render in its own dedicated "App URL" row above the Links section so
 * its visual treatment (font-mono, full URL displayed verbatim) and its
 * legacy `data-testid="deployment-output-url"` are preserved unchanged.
 */
function DeploymentLinksSection({
  output,
}: {
  output: { links?: DeploymentLink[] }
}) {
  const safeLinks = (output.links ?? []).filter((l) => isSafeHttpUrl(l.url))
  if (safeLinks.length === 0) return null

  return (
    <div data-testid="deployment-links" className="space-y-2">
      <div className="flex items-baseline gap-2">
        <span className="text-muted-foreground text-sm w-36 shrink-0">Links</span>
        <div className="flex flex-col gap-1">
          {safeLinks.map((l) => (
            <DeploymentLinkRow
              key={`${l.source}/${l.name}/${l.url}`}
              href={l.url}
              text={linkDisplayText(l)}
              description={l.description}
              source={l.source}
              testId={`deployment-link-row-${l.name}`}
            />
          ))}
        </div>
      </div>
    </div>
  )
}

function DeploymentDetailRoute() {
  const { projectName, deploymentName } = Route.useParams()
  return <DeploymentDetailPage projectName={projectName} deploymentName={deploymentName} />
}

export function DeploymentDetailPage({
  projectName: propProjectName,
  deploymentName: propDeploymentName,
}: { projectName?: string; deploymentName?: string } = {}) {
  let routeParams: { projectName?: string; deploymentName?: string } = {}
  let routeSearch: { tab?: DeploymentTab } = {}
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    routeParams = Route.useParams()
    // eslint-disable-next-line react-hooks/rules-of-hooks
    routeSearch = Route.useSearch()
  } catch {
    routeParams = {}
    routeSearch = {}
  }
  const projectName = propProjectName ?? routeParams.projectName ?? ''
  const deploymentName = propDeploymentName ?? routeParams.deploymentName ?? ''

  const navigate = useNavigate()
  const { data: deployment, isPending, error } = useGetDeployment(projectName, deploymentName)
  const { data: status } = useGetDeploymentStatus(projectName, deploymentName, { refetchInterval: 5000 })
  const { data: project } = useGetProject(projectName)
  const { data: policyState, isPending: isPolicyPending, error: policyError } = useGetDeploymentPolicyState(projectName, deploymentName)

  const [tailLines, setTailLines] = useState<number>(100)
  const [previous, setPrevious] = useState(false)
  const { data: logs } = useGetDeploymentLogs(projectName, deploymentName, { tailLines, previous })

  const updateMutation = useUpdateDeployment(projectName, deploymentName)
  const deleteMutation = useDeleteDeployment(projectName)

  const [redeployOpen, setRedeployOpen] = useState(false)
  const [redeployImage, setRedeployImage] = useState('')
  const [redeployTag, setRedeployTag] = useState('')
  const [redeployPort, setRedeployPort] = useState(8080)
  const [redeployCommand, setRedeployCommand] = useState<string[]>([])
  const [redeployArgs, setRedeployArgs] = useState<string[]>([])
  const [redeployEnv, setRedeployEnv] = useState<EnvVar[]>([])
  const [redeployError, setRedeployError] = useState<string | null>(null)

  const [deleteOpen, setDeleteOpen] = useState(false)

  // Local tab state initialised from the URL search param so that the component
  // responds immediately to tab clicks without waiting for a navigation cycle.
  // The URL is kept in sync via navigate so tabs are deep-linkable.
  // `validateTab` also strips legacy values (notably `template`, removed in
  // HOL-611) so a stale deep-link degrades to Status instead of leaking an
  // invalid id into state.
  const [activeTab, setActiveTab] = useState<DeploymentTab>(() => validateTab(routeSearch.tab))

  const handleTabChange = (next: string) => {
    const tab = validateTab(next)
    setActiveTab(tab)
    // Persist tab in URL so selections are shareable/bookmarkable.
    void (navigate as unknown as (opts: { search: { tab: DeploymentTab }; replace: boolean }) => void)({ search: { tab }, replace: true })
  }

  useEffect(() => {
    if (deployment) {
      setRedeployImage(deployment.image)
      setRedeployTag(deployment.tag)
      setRedeployPort(deployment.port || 8080)
      setRedeployCommand(deployment.command ?? [])
      setRedeployArgs(deployment.args ?? [])
      setRedeployEnv(deployment.env ?? [])
    }
  }, [deployment?.image, deployment?.tag, deployment?.port, deployment?.command, deployment?.args, deployment?.env])

  const userRole = project?.userRole ?? Role.VIEWER
  const canWrite = userRole === Role.OWNER || userRole === Role.EDITOR
  const canDelete = userRole === Role.OWNER

  const handleRedeployOpen = () => {
    setRedeployImage(deployment?.image ?? '')
    setRedeployTag(deployment?.tag ?? '')
    setRedeployPort(deployment?.port || 8080)
    setRedeployCommand(deployment?.command ?? [])
    setRedeployArgs(deployment?.args ?? [])
    setRedeployEnv(deployment?.env ?? [])
    setRedeployError(null)
    updateMutation.reset()
    setRedeployOpen(true)
  }

  // handleReconcile fires an UpdateDeployment with the deployment's current
  // image/tag/port/command/args/env. The backend treats this as a re-render:
  // the template is re-evaluated against the current TemplatePolicy chain
  // and the applied render set is re-recorded, which clears drift on
  // success. No functional changes are made to the running workload when
  // the rendered manifest is unchanged.
  const handleReconcile = async () => {
    if (!deployment) return
    try {
      await updateMutation.mutateAsync({
        image: deployment.image,
        tag: deployment.tag,
        port: deployment.port || 8080,
        command: deployment.command ?? [],
        args: deployment.args ?? [],
        env: filterEnvVars(deployment.env ?? []),
      })
      toast.success('Reconcile requested')
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err))
    }
  }

  const handleRedeploy = async () => {
    if (!redeployImage.trim()) {
      setRedeployError('Image is required')
      return
    }
    if (!redeployTag.trim()) {
      setRedeployError('Tag is required')
      return
    }
    setRedeployError(null)
    try {
      await updateMutation.mutateAsync({ image: redeployImage.trim(), tag: redeployTag.trim(), port: redeployPort, command: redeployCommand, args: redeployArgs, env: filterEnvVars(redeployEnv) })
      setRedeployOpen(false)
      toast.success('Deployment updated')
    } catch (err) {
      setRedeployError(err instanceof Error ? err.message : String(err))
    }
  }

  const handleDeleteConfirm = async () => {
    try {
      await deleteMutation.mutateAsync({ name: deploymentName })
      setDeleteOpen(false)
      navigate({ to: '/projects/$projectName/deployments', params: { projectName } })
    } catch { /* error shown via mutation */ }
  }

  if (isPending) {
    return (
      <Card>
        <CardContent className="pt-6 space-y-4">
          <Skeleton className="h-5 w-48" />
          <Skeleton className="h-8 w-full" />
          <Skeleton className="h-40 w-full" />
        </CardContent>
      </Card>
    )
  }

  if (error) {
    return (
      <Card>
        <CardContent className="pt-6">
          <Alert variant="destructive">
            <AlertDescription>{error.message}</AlertDescription>
          </Alert>
        </CardContent>
      </Card>
    )
  }

  return (
    <>
      <Card>
        <CardContent className="pt-6 space-y-6">
          {/* Header — stays above the tab bar, visible on every tab */}
          <div className="flex flex-col gap-4">
            <Link
              to="/projects/$projectName/deployments"
              params={{ projectName }}
              className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
              aria-label="Back to Deployments"
            >
              <ArrowLeft className="h-4 w-4" />
              Back to Deployments
            </Link>

            <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
              <div>
                <p className="text-sm text-muted-foreground">{projectName} / Deployments / {deploymentName}</p>
                <h2 className="text-xl font-semibold mt-1">{deployment?.displayName || deploymentName}</h2>
                <div className="flex items-center gap-4 mt-1 text-sm text-muted-foreground">
                  <span>Image: <span className="font-mono">{deployment?.image}</span></span>
                  <span>Tag: <span className="font-mono">{deployment?.tag}</span></span>
                </div>
              </div>
              <div className="flex items-center gap-2 shrink-0">
                {canWrite && (
                  <Button size="sm" onClick={handleRedeployOpen}>Re-deploy</Button>
                )}
                {canDelete && (
                  <Button
                    size="sm"
                    variant="destructive"
                    onClick={() => { deleteMutation.reset(); setDeleteOpen(true) }}
                    aria-label="Delete Deployment"
                  >
                    Delete Deployment
                  </Button>
                )}
              </div>
            </div>
          </div>

          {/* Tab bar */}
          <Tabs value={activeTab} onValueChange={handleTabChange}>
            <TabsList>
              <TabsTrigger value="status">Status</TabsTrigger>
              <TabsTrigger value="logs">Logs</TabsTrigger>
            </TabsList>

            {/* Status tab — replicas, conditions, pods, environment variables */}
            <TabsContent value="status" className="mt-4 space-y-6">
              <div className="space-y-4">
                <h3 className="text-sm font-medium">Status</h3>
                <Separator />
                {/*
                  App URL row — surfaces the deployment's primary URL
                  from the live aggregator on
                  `deployment.statusSummary.output.url` (set by the
                  HOL-574 path when a `console.holos.run/primary-url`
                  annotation is present on an owned resource). Gated
                  through `isSafeHttpUrl` so non-HTTP(S) schemes
                  (javascript:, data:, vbscript:, file:) cannot reach
                  an anchor href. When no live URL has been observed
                  yet, nothing renders.
                */}
                {(() => {
                  const liveURL = deployment?.statusSummary?.output?.url
                  const primaryURL = liveURL && isSafeHttpUrl(liveURL) ? liveURL : ''
                  if (!primaryURL) return null
                  return (
                    <div
                      data-testid="deployment-output-url"
                      className="flex items-center gap-2 text-sm"
                    >
                      <span className="text-muted-foreground w-36 shrink-0">App URL</span>
                      <a
                        href={primaryURL}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="inline-flex items-center gap-1 underline-offset-4 hover:underline break-all"
                      >
                        <span className="font-mono">{primaryURL}</span>
                        <ExternalLink aria-hidden="true" className="h-3.5 w-3.5 shrink-0" />
                      </a>
                    </div>
                  )
                })()}
                {/*
                  Links section (HOL-575) — surfaces the secondary links
                  aggregated from `console.holos.run/external-link.*` and
                  `link.argocd.argoproj.io/*` annotations on owned resources.
                  Sourced from `deployment.statusSummary.output.links`
                  (populated by the HOL-573 / HOL-574 aggregator).
                  Each anchor is gated through `isSafeHttpUrl` so unsafe
                  schemes (javascript:, data:, vbscript:, file:) cannot
                  reach an `href`; the section is hidden entirely when no
                  entry survives the allowlist. The primary URL above
                  intentionally remains its own row so its visual treatment
                  and the legacy `deployment-output-url` test id are
                  preserved unchanged.
                */}
                {deployment?.statusSummary?.output ? (
                  <DeploymentLinksSection output={deployment.statusSummary.output} />
                ) : null}
                {status ? (
                  <div className="space-y-3">
                    <div className="flex items-center gap-2 text-sm">
                      <span className="text-muted-foreground w-36 shrink-0">Replicas</span>
                      <span>{status.readyReplicas}/{status.desiredReplicas} ready, {status.availableReplicas} available</span>
                    </div>

                    {status.conditions.length > 0 && (
                      <div>
                        <p className="text-sm text-muted-foreground mb-2">Conditions</p>
                        <div className="space-y-1">
                          {status.conditions.map((cond) => (
                            <div key={cond.type} className="flex items-center gap-2 text-sm">
                              {cond.status === 'True' ? (
                                <CheckCircle2 className="h-4 w-4 text-green-600 shrink-0" />
                              ) : (
                                <XCircle className="h-4 w-4 text-red-600 shrink-0" />
                              )}
                              <span className="font-medium">{cond.type}</span>
                              {cond.reason && <span className="text-muted-foreground">({cond.reason})</span>}
                            </div>
                          ))}
                        </div>
                      </div>
                    )}

                    {status.pods.length > 0 && (
                      <div>
                        <p className="text-sm text-muted-foreground mb-2">Pods</p>
                        <div className="space-y-3">
                          {status.pods.map((pod) => (
                            <div key={pod.name} className="space-y-1">
                              <div className="flex items-center gap-3 text-sm font-mono">
                                <span>{pod.name}</span>
                                <Badge variant="outline" className="text-xs">{pod.phase}</Badge>
                                {pod.ready && <Badge className="bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200 border-transparent text-xs">Ready</Badge>}
                                {pod.restartCount > 0 && <span className="text-muted-foreground text-xs">{pod.restartCount} restarts</span>}
                              </div>
                              {pod.containerStatuses && pod.containerStatuses.length > 0 && (
                                <div className="ml-4 space-y-1">
                                  <p className="text-xs text-muted-foreground">Containers:</p>
                                  {pod.containerStatuses.map((cs) => {
                                    // Terminated containers are green for normal completion (no error reason),
                                    // red only when an error reason is present or restarts indicate failure.
                                    const terminatedIsError = cs.state === 'terminated' && isContainerError(cs)
                                    const badgeClass =
                                      cs.state === 'running'
                                        ? 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200 border-transparent text-xs'
                                        : cs.state === 'waiting'
                                          ? 'bg-yellow-100 text-yellow-800 dark:bg-yellow-900 dark:text-yellow-200 border-transparent text-xs'
                                          : terminatedIsError
                                            ? 'bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200 border-transparent text-xs'
                                            : 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200 border-transparent text-xs'
                                    return (
                                    <div key={cs.name} className="flex items-center gap-2 text-xs font-mono flex-wrap">
                                      <span className="text-foreground">{cs.name}</span>
                                      <Badge data-testid="container-state-badge" className={badgeClass}>
                                        {cs.state}
                                      </Badge>
                                      {cs.reason && (
                                        <span className={isContainerError(cs) ? 'text-yellow-600 dark:text-yellow-500' : 'text-muted-foreground'}>
                                          {cs.reason}
                                        </span>
                                      )}
                                      {cs.message && (
                                        <span className="text-muted-foreground truncate max-w-md" title={cs.message}>
                                          — {cs.message}
                                        </span>
                                      )}
                                      {cs.image && (
                                        <span className="text-muted-foreground truncate max-w-sm" title={cs.image}>
                                          {cs.image}
                                        </span>
                                      )}
                                    </div>
                                    )
                                  })}
                                </div>
                              )}
                            </div>
                          ))}
                        </div>
                      </div>
                    )}
                  </div>
                ) : (
                  <p className="text-sm text-muted-foreground">No status available.</p>
                )}
              </div>

              {/* Events section — between Pods and Environment Variables */}
              <div className="space-y-4">
                <h3 className="text-sm font-medium">Events</h3>
                <Separator />
                {status && status.events && status.events.length > 0 ? (
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead className="w-8"></TableHead>
                        <TableHead>Reason</TableHead>
                        <TableHead>Age</TableHead>
                        <TableHead>Source</TableHead>
                        <TableHead>Message</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {status.events.map((evt, idx) => (
                        <TableRow key={idx}>
                          <TableCell className="w-8 pr-0">
                            {evt.type === 'Warning' ? (
                              <TriangleAlert data-testid="event-warning-icon" className="h-4 w-4 text-yellow-600 dark:text-yellow-500 shrink-0" />
                            ) : (
                              <Info data-testid="event-normal-icon" className="h-4 w-4 text-muted-foreground shrink-0" />
                            )}
                          </TableCell>
                          <TableCell className={evt.type === 'Warning' ? 'text-yellow-600 dark:text-yellow-500 font-medium text-sm' : 'text-sm'}>
                            {evt.reason}
                          </TableCell>
                          <TableCell className="text-sm text-muted-foreground whitespace-nowrap">
                            {formatEventAge(evt)}
                          </TableCell>
                          <TableCell className="text-sm text-muted-foreground">
                            {evt.source}
                          </TableCell>
                          <TableCell className="text-sm max-w-md truncate" title={evt.message}>
                            {evt.message}
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                ) : (
                  <p className="text-sm text-muted-foreground">No events.</p>
                )}
              </div>

              {/*
                Policy section — renders the TemplatePolicy drift snapshot
                for this deployment (HOL-567 / HOL-559). The data is fetched
                via GetDeploymentPolicyState, which reads PolicyState from
                the folder-namespace render-state store. The UI never reads
                drift state directly from project-namespace resources.
                Reconcile is gated on PERMISSION_DEPLOYMENTS_WRITE (role
                OWNER or EDITOR); viewers see the badge but no button.
              */}
              <PolicySection
                state={policyState}
                isPending={isPolicyPending}
                error={policyError}
                reconcileAction={
                  canWrite ? (
                    <Button
                      size="sm"
                      onClick={handleReconcile}
                      disabled={updateMutation.isPending}
                      aria-label="Reconcile policy drift"
                    >
                      {updateMutation.isPending ? 'Reconciling...' : 'Reconcile'}
                    </Button>
                  ) : undefined
                }
              />

              {deployment && deployment.env && deployment.env.length > 0 && (
                <div className="space-y-4">
                  <h3 className="text-sm font-medium">Environment Variables</h3>
                  <Separator />
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>Name</TableHead>
                        <TableHead>Source</TableHead>
                        <TableHead>Value / Reference</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {deployment.env.map((ev, idx) => (
                        <TableRow key={idx}>
                          <TableCell className="font-mono text-sm">{ev.name}</TableCell>
                          <TableCell>
                            {ev.source.case === 'value' && 'Value'}
                            {ev.source.case === 'secretKeyRef' && 'Secret'}
                            {ev.source.case === 'configMapKeyRef' && 'ConfigMap'}
                          </TableCell>
                          <TableCell className="font-mono text-sm text-muted-foreground">
                            {ev.source.case === 'value' && ev.source.value}
                            {ev.source.case === 'secretKeyRef' && `${ev.source.value.name} → ${ev.source.value.key}`}
                            {ev.source.case === 'configMapKeyRef' && `${ev.source.value.name} → ${ev.source.value.key}`}
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </div>
              )}
            </TabsContent>

            {/* Logs tab — tail lines selector, previous checkbox, log viewer */}
            <TabsContent value="logs" className="mt-4 space-y-4">
              <div className="flex items-center justify-between">
                <h3 className="text-sm font-medium">Logs</h3>
                <div className="flex items-center gap-2">
                  <Select value={String(tailLines)} onValueChange={(v) => setTailLines(Number(v))}>
                    <SelectTrigger className="w-28" aria-label="Tail lines">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="50">tail: 50</SelectItem>
                      <SelectItem value="100">tail: 100</SelectItem>
                      <SelectItem value="500">tail: 500</SelectItem>
                    </SelectContent>
                  </Select>
                  <div className="flex items-center gap-1.5">
                    <input
                      type="checkbox"
                      id="previous-logs"
                      checked={previous}
                      onChange={(e) => setPrevious(e.target.checked)}
                      className="h-4 w-4"
                    />
                    <Label htmlFor="previous-logs" className="text-sm font-normal cursor-pointer">Previous</Label>
                  </div>
                </div>
              </div>
              <Separator />
              <pre className="rounded-md bg-muted p-4 text-xs font-mono overflow-auto max-h-[70vh] whitespace-pre-wrap">
                {logs || 'No logs available.'}
              </pre>
            </TabsContent>
          </Tabs>
        </CardContent>
      </Card>

      <Dialog open={redeployOpen} onOpenChange={setRedeployOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Re-deploy</DialogTitle>
            <DialogDescription>Update the image and tag to roll out a new version of &quot;{deploymentName}&quot;.</DialogDescription>
          </DialogHeader>
          <div className="space-y-3">
            <div>
              <Label htmlFor="redeploy-image">Image</Label>
              <Input
                id="redeploy-image"
                value={redeployImage}
                onChange={(e) => setRedeployImage(e.target.value)}
                placeholder="ghcr.io/org/app"
              />
            </div>
            <div>
              <Label htmlFor="redeploy-tag">Tag</Label>
              <Input
                id="redeploy-tag"
                value={redeployTag}
                onChange={(e) => setRedeployTag(e.target.value)}
                placeholder="v1.0.0"
              />
            </div>
            <div>
              <Label htmlFor="redeploy-port">Port</Label>
              <Input
                id="redeploy-port"
                aria-label="Port"
                type="number"
                min={1}
                max={65535}
                value={redeployPort}
                onChange={(e) => setRedeployPort(parseInt(e.target.value, 10))}
                placeholder="8080"
              />
              <p className="text-xs text-muted-foreground mt-1">
                Container port the application listens on (HTTP)
              </p>
            </div>
            <div>
              <Label>Command</Label>
              <p className="text-xs text-muted-foreground mb-1">Override container ENTRYPOINT (optional)</p>
              <StringListInput
                value={redeployCommand}
                onChange={setRedeployCommand}
                placeholder="command entry"
                addLabel="Add command"
              />
            </div>
            <div>
              <Label>Args</Label>
              <p className="text-xs text-muted-foreground mb-1">Override container CMD (optional)</p>
              <StringListInput
                value={redeployArgs}
                onChange={setRedeployArgs}
                placeholder="args entry"
                addLabel="Add args"
              />
            </div>
            <div>
              <Label>Environment Variables</Label>
              <p className="text-xs text-muted-foreground mb-1">Set container environment variables (optional)</p>
              <EnvVarEditor
                project={projectName}
                value={redeployEnv}
                onChange={setRedeployEnv}
              />
            </div>
            {redeployError && (
              <Alert variant="destructive"><AlertDescription>{redeployError}</AlertDescription></Alert>
            )}
          </div>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setRedeployOpen(false)}>Cancel</Button>
            <Button onClick={handleRedeploy} disabled={updateMutation.isPending}>
              {updateMutation.isPending ? 'Deploying...' : 'Deploy'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete Deployment</DialogTitle>
            <DialogDescription>
              This will permanently delete deployment &quot;{deploymentName}&quot;. This action cannot be undone.
            </DialogDescription>
          </DialogHeader>
          {deleteMutation.error && (
            <Alert variant="destructive">
              <AlertDescription>{deleteMutation.error.message}</AlertDescription>
            </Alert>
          )}
          <DialogFooter>
            <Button variant="ghost" onClick={() => setDeleteOpen(false)}>Cancel</Button>
            <Button variant="destructive" onClick={handleDeleteConfirm} disabled={deleteMutation.isPending}>
              {deleteMutation.isPending ? 'Deleting...' : 'Delete'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}

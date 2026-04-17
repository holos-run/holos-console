import { useState, useEffect } from 'react'
import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'
import { toast } from 'sonner'
import { StringListInput } from '@/components/string-list-input'
import { EnvVarEditor, filterEnvVars } from '@/components/env-var-editor'
import { CueTemplateEditor } from '@/components/cue-template-editor'
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
import { ArrowLeft, CheckCircle2, Copy, ExternalLink, Info, TriangleAlert, XCircle } from 'lucide-react'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import type { EnvVar, Event, ContainerStatus } from '@/gen/holos/console/v1/deployments_pb'
import { useGetDeployment, useGetDeploymentStatus, useGetDeploymentLogs, useGetDeploymentRenderPreview, useUpdateDeployment, useDeleteDeployment } from '@/queries/deployments'
import { makeProjectScope } from '@/queries/templates'
import { useGetProject } from '@/queries/projects'
import { isSafeHttpUrl } from '@/lib/url'

type DeploymentTab = 'status' | 'logs' | 'template'

function validateTab(value: unknown): DeploymentTab {
  if (value === 'logs' || value === 'template') return value
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
  const { data: preview, isPending: isPreviewPending } = useGetDeploymentRenderPreview(projectName, deploymentName)

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
  const [activeTab, setActiveTab] = useState<DeploymentTab>(() => routeSearch.tab ?? 'status')

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
              <TabsTrigger value="template">Template</TabsTrigger>
            </TabsList>

            {/* Status tab — replicas, conditions, pods, environment variables */}
            <TabsContent value="status" className="mt-4 space-y-6">
              <div className="space-y-4">
                <h3 className="text-sm font-medium">Status</h3>
                <Separator />
                {/*
                  App URL row — surfaces the template-authored deployment URL
                  from the render preview (`output.url`). Rendered only when
                  the preview has resolved with a non-empty URL that parses
                  as an http:/https: URL. Non-HTTP(S) schemes (including
                  javascript:, data:, vbscript:, file:) are dropped so they
                  cannot reach an anchor href and execute script on click.
                  While the preview is pending, `preview` is undefined so
                  nothing renders (deliberate: avoids a flash on first load).
                */}
                {!isPreviewPending && preview?.output?.url && isSafeHttpUrl(preview.output.url) ? (
                  <div
                    data-testid="deployment-output-url"
                    className="flex items-center gap-2 text-sm"
                  >
                    <span className="text-muted-foreground w-36 shrink-0">App URL</span>
                    <a
                      href={preview.output.url}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="inline-flex items-center gap-1 underline-offset-4 hover:underline break-all"
                    >
                      <span className="font-mono">{preview.output.url}</span>
                      <ExternalLink aria-hidden="true" className="h-3.5 w-3.5 shrink-0" />
                    </a>
                  </div>
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

            {/* Template tab — read-only CUE editor and API access helpers */}
            <TabsContent value="template" className="mt-4 space-y-6">
              <div className="space-y-4">
                <h3 className="text-sm font-medium">Template Preview</h3>
                <Separator />
                {isPreviewPending ? (
                  <div className="space-y-4">
                    <Skeleton className="h-5 w-48" />
                    <Skeleton className="h-64 w-full" />
                  </div>
                ) : preview ? (
                  <CueTemplateEditor
                    cueTemplate={preview.cueTemplate}
                    onChange={() => {}}
                    readOnly={true}
                    defaultPlatformInput={preview.cuePlatformInput}
                    defaultProjectInput={preview.cueProjectInput}
                    scope={makeProjectScope(projectName)}
                  />
                ) : null}
              </div>

              <div className="space-y-4">
                <h3 className="text-sm font-medium">API Access</h3>
                <Separator />
                <p className="text-xs text-muted-foreground">
                  Call this RPC from the command line. Set{' '}
                  <code className="font-mono">$HOLOS_ID_TOKEN</code> first — see the API Access
                  section on your{' '}
                  <Link to="/profile" className="underline">
                    profile page
                  </Link>
                  .
                </p>
                <div className="space-y-2">
                  <p className="text-xs uppercase tracking-wider text-muted-foreground">
                    curl (Connect protocol — recommended)
                  </p>
                  <div className="relative">
                    <pre className="rounded-md bg-muted p-4 text-xs font-mono overflow-auto whitespace-pre">
                      {`curl -s --cacert "$(mkcert -CAROOT)/rootCA.pem" \\\n  ${typeof window !== 'undefined' ? window.location.origin : 'https://localhost:8443'}/holos.console.v1.DeploymentService/GetDeploymentRenderPreview \\\n  -H "Content-Type: application/json" \\\n  -H "Connect-Protocol-Version: 1" \\\n  -H "Authorization: Bearer $HOLOS_ID_TOKEN" \\\n  -d '{"project": "${projectName}", "name": "${deploymentName}"}'`}
                    </pre>
                    <Button
                      variant="ghost"
                      size="icon"
                      aria-label="Copy curl command"
                      className="absolute top-2 right-2 h-7 w-7"
                      onClick={() => {
                        const origin = typeof window !== 'undefined' ? window.location.origin : 'https://localhost:8443'
                        const cmd = `curl -s --cacert "$(mkcert -CAROOT)/rootCA.pem" \\\n  ${origin}/holos.console.v1.DeploymentService/GetDeploymentRenderPreview \\\n  -H "Content-Type: application/json" \\\n  -H "Connect-Protocol-Version: 1" \\\n  -H "Authorization: Bearer $HOLOS_ID_TOKEN" \\\n  -d '{"project": "${projectName}", "name": "${deploymentName}"}'`
                        navigator.clipboard.writeText(cmd)
                        toast.success('Copied to clipboard')
                      }}
                    >
                      <Copy className="h-3.5 w-3.5" />
                    </Button>
                  </div>
                </div>
                <div className="space-y-2">
                  <p className="text-xs uppercase tracking-wider text-muted-foreground">
                    grpcurl (gRPC backward compatibility)
                  </p>
                  <div className="relative">
                    <pre className="rounded-md bg-muted p-4 text-xs font-mono overflow-auto whitespace-pre">
                      {`grpcurl -cacert "$(mkcert -CAROOT)/rootCA.pem" \\\n  -H "Authorization: Bearer $HOLOS_ID_TOKEN" \\\n  -d '{"project": "${projectName}", "name": "${deploymentName}"}' \\\n  ${typeof window !== 'undefined' ? window.location.host : 'localhost:8443'} \\\n  holos.console.v1.DeploymentService/GetDeploymentRenderPreview`}
                    </pre>
                    <Button
                      variant="ghost"
                      size="icon"
                      aria-label="Copy grpcurl command"
                      className="absolute top-2 right-2 h-7 w-7"
                      onClick={() => {
                        const host = typeof window !== 'undefined' ? window.location.host : 'localhost:8443'
                        const cmd = `grpcurl -cacert "$(mkcert -CAROOT)/rootCA.pem" \\\n  -H "Authorization: Bearer $HOLOS_ID_TOKEN" \\\n  -d '{"project": "${projectName}", "name": "${deploymentName}"}' \\\n  ${host} \\\n  holos.console.v1.DeploymentService/GetDeploymentRenderPreview`
                        navigator.clipboard.writeText(cmd)
                        toast.success('Copied to clipboard')
                      }}
                    >
                      <Copy className="h-3.5 w-3.5" />
                    </Button>
                  </div>
                </div>
              </div>
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

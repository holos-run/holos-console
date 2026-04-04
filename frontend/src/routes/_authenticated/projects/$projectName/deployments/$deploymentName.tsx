import { useState, useEffect } from 'react'
import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'
import { toast } from 'sonner'
import { StringListInput } from '@/components/string-list-input'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'
import { Separator } from '@/components/ui/separator'
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
import { ArrowLeft, CheckCircle2, XCircle } from 'lucide-react'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { useGetDeployment, useGetDeploymentStatus, useGetDeploymentLogs, useUpdateDeployment, useDeleteDeployment } from '@/queries/deployments'
import { useGetProject } from '@/queries/projects'

export const Route = createFileRoute('/_authenticated/projects/$projectName/deployments/$deploymentName')({
  component: DeploymentDetailRoute,
})

function DeploymentDetailRoute() {
  const { projectName, deploymentName } = Route.useParams()
  return <DeploymentDetailPage projectName={projectName} deploymentName={deploymentName} />
}

export function DeploymentDetailPage({
  projectName: propProjectName,
  deploymentName: propDeploymentName,
}: { projectName?: string; deploymentName?: string } = {}) {
  let routeParams: { projectName?: string; deploymentName?: string } = {}
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    routeParams = Route.useParams()
  } catch {
    routeParams = {}
  }
  const projectName = propProjectName ?? routeParams.projectName ?? ''
  const deploymentName = propDeploymentName ?? routeParams.deploymentName ?? ''

  const navigate = useNavigate()
  const { data: deployment, isPending, error } = useGetDeployment(projectName, deploymentName)
  const { data: status } = useGetDeploymentStatus(projectName, deploymentName, { refetchInterval: 5000 })
  const { data: project } = useGetProject(projectName)

  const [tailLines, setTailLines] = useState<number>(100)
  const [previous, setPrevious] = useState(false)
  const { data: logs } = useGetDeploymentLogs(projectName, deploymentName, { tailLines, previous })

  const updateMutation = useUpdateDeployment(projectName, deploymentName)
  const deleteMutation = useDeleteDeployment(projectName)

  const [redeployOpen, setRedeployOpen] = useState(false)
  const [redeployImage, setRedeployImage] = useState('')
  const [redeployTag, setRedeployTag] = useState('')
  const [redeployCommand, setRedeployCommand] = useState<string[]>([])
  const [redeployArgs, setRedeployArgs] = useState<string[]>([])
  const [redeployError, setRedeployError] = useState<string | null>(null)

  const [deleteOpen, setDeleteOpen] = useState(false)

  useEffect(() => {
    if (deployment) {
      setRedeployImage(deployment.image)
      setRedeployTag(deployment.tag)
      setRedeployCommand(deployment.command ?? [])
      setRedeployArgs(deployment.args ?? [])
    }
  }, [deployment?.image, deployment?.tag, deployment?.command, deployment?.args])

  const userRole = project?.userRole ?? Role.VIEWER
  const canWrite = userRole === Role.OWNER || userRole === Role.EDITOR
  const canDelete = userRole === Role.OWNER

  const handleRedeployOpen = () => {
    setRedeployImage(deployment?.image ?? '')
    setRedeployTag(deployment?.tag ?? '')
    setRedeployCommand(deployment?.command ?? [])
    setRedeployArgs(deployment?.args ?? [])
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
      await updateMutation.mutateAsync({ image: redeployImage.trim(), tag: redeployTag.trim(), command: redeployCommand, args: redeployArgs })
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

          <div className="space-y-4">
            <h3 className="text-sm font-medium">Status</h3>
            <Separator />
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
                    <div className="space-y-1">
                      {status.pods.map((pod) => (
                        <div key={pod.name} className="flex items-center gap-3 text-sm font-mono">
                          <span>{pod.name}</span>
                          <Badge variant="outline" className="text-xs">{pod.phase}</Badge>
                          {pod.ready && <Badge className="bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200 border-transparent text-xs">Ready</Badge>}
                          {pod.restartCount > 0 && <span className="text-muted-foreground text-xs">{pod.restartCount} restarts</span>}
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

          <div className="space-y-4">
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
            <pre className="rounded-md bg-muted p-4 text-xs font-mono overflow-auto max-h-96 whitespace-pre-wrap">
              {logs || 'No logs available.'}
            </pre>
          </div>
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

import { useState } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import { toast } from 'sonner'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Trash2 } from 'lucide-react'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { PhaseBadge } from '@/components/phase-badge'
import { useListDeployments, useDeleteDeployment } from '@/queries/deployments'
import { useGetProject } from '@/queries/projects'

export const Route = createFileRoute('/_authenticated/projects/$projectName/deployments/')({
  component: DeploymentsRoute,
})

function DeploymentsRoute() {
  const { projectName } = Route.useParams()
  return <DeploymentsPage projectName={projectName} />
}

export function DeploymentsPage({ projectName: propProjectName }: { projectName?: string } = {}) {
  let routeProjectName: string | undefined
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    routeProjectName = Route.useParams().projectName
  } catch {
    routeProjectName = undefined
  }
  const projectName = propProjectName ?? routeProjectName ?? ''

  const { data: deployments = [], isLoading, error } = useListDeployments(projectName)
  const { data: project } = useGetProject(projectName)
  const deleteMutation = useDeleteDeployment(projectName)

  const [deleteOpen, setDeleteOpen] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null)

  const userRole = project?.userRole ?? Role.VIEWER
  const canWrite = userRole === Role.OWNER || userRole === Role.EDITOR
  const canDelete = userRole === Role.OWNER

  const handleDeleteConfirm = async () => {
    if (!deleteTarget) return
    try {
      await deleteMutation.mutateAsync({ name: deleteTarget })
      setDeleteOpen(false)
      setDeleteTarget(null)
      toast.success('Deployment deleted')
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err))
    }
  }

  if (isLoading) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>{projectName} / Deployments</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="space-y-2">
            {[...Array(3)].map((_, i) => (
              <Skeleton key={i} className="h-10 w-full" />
            ))}
          </div>
        </CardContent>
      </Card>
    )
  }

  if (error) {
    return (
      <Card>
        <CardContent className="pt-6">
          <Alert variant="destructive"><AlertDescription>{error.message}</AlertDescription></Alert>
        </CardContent>
      </Card>
    )
  }

  return (
    <>
      <Card>
        <CardHeader className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-2">
          <CardTitle>{projectName} / Deployments</CardTitle>
          {canWrite && (
            <Link to="/projects/$projectName/deployments/new" params={{ projectName }}>
              <Button size="sm">Create Deployment</Button>
            </Link>
          )}
        </CardHeader>
        <CardContent>
          {deployments.length === 0 ? (
            <div className="flex flex-col items-center gap-3 py-8 text-center">
              <p className="text-muted-foreground">No deployments yet. Create one to get started.</p>
              {canWrite && (
                <Link to="/projects/$projectName/deployments/new" params={{ projectName }}>
                  <Button size="sm">Create Deployment</Button>
                </Link>
              )}
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Image</TableHead>
                  <TableHead>Tag</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead></TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {deployments.map((deployment) => (
                  <TableRow key={deployment.name}>
                    <TableCell>
                      <Link
                        to="/projects/$projectName/deployments/$deploymentName"
                        params={{ projectName, deploymentName: deployment.name }}
                        search={{ tab: 'status' }}
                        className="font-medium hover:underline"
                      >
                        {deployment.name}
                      </Link>
                    </TableCell>
                    <TableCell className="font-mono text-sm">{deployment.image}</TableCell>
                    <TableCell className="font-mono text-sm">{deployment.tag}</TableCell>
                    <TableCell>
                      <PhaseBadge summary={deployment.statusSummary} />
                    </TableCell>
                    <TableCell className="text-right">
                      {canDelete && (
                        <Button
                          variant="ghost"
                          size="icon"
                          aria-label={`delete ${deployment.name}`}
                          onClick={() => { setDeleteTarget(deployment.name); deleteMutation.reset(); setDeleteOpen(true) }}
                        >
                          <Trash2 className="h-4 w-4" />
                        </Button>
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      <Dialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete Deployment</DialogTitle>
            <DialogDescription>
              Are you sure you want to delete deployment &quot;{deleteTarget}&quot;? This action cannot be undone.
            </DialogDescription>
          </DialogHeader>
          {deleteMutation.error && (
            <Alert variant="destructive"><AlertDescription>{deleteMutation.error.message}</AlertDescription></Alert>
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


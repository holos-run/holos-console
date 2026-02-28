import { useState } from 'react'
import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { Trash2 } from 'lucide-react'
import { useAuth } from '@/lib/auth'
import { useOrg } from '@/lib/org-context'
import { useListProjects, useDeleteProject, useCreateProject } from '@/queries/projects'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { slugify } from '@/utils/slugify'

export const Route = createFileRoute('/_authenticated/projects/')({
  component: ProjectsListPage,
})

function roleName(role: Role): string {
  switch (role) {
    case Role.OWNER: return 'Owner'
    case Role.EDITOR: return 'Editor'
    case Role.VIEWER: return 'Viewer'
    default: return 'None'
  }
}

function ProjectsListPage() {
  const { selectedOrg } = useOrg()
  const effectiveOrg = selectedOrg || ''
  const navigate = useNavigate()
  const { user } = useAuth()

  const { data, isLoading, error } = useListProjects(effectiveOrg)
  const projects = data?.projects ?? []

  const deleteMutation = useDeleteProject()
  const createMutation = useCreateProject()

  const [createOpen, setCreateOpen] = useState(false)
  const [createName, setCreateName] = useState('')
  const [createDisplayName, setCreateDisplayName] = useState('')
  const [createDescription, setCreateDescription] = useState('')
  const [createError, setCreateError] = useState<string | null>(null)
  const [nameManuallyEdited, setNameManuallyEdited] = useState(false)

  const [deleteOpen, setDeleteOpen] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null)

  const handleCreateOpen = () => {
    setCreateName('')
    setCreateDisplayName('')
    setCreateDescription('')
    setCreateError(null)
    setNameManuallyEdited(false)
    setCreateOpen(true)
  }

  const handleCreate = async () => {
    if (!createName.trim()) {
      setCreateError('Project name is required')
      return
    }
    setCreateError(null)
    try {
      await createMutation.mutateAsync({
        name: createName.trim(),
        displayName: createDisplayName.trim(),
        description: createDescription.trim(),
        organization: effectiveOrg,
        userGrants: [{ principal: (user?.profile?.email as string) || '', role: Role.OWNER }],
        roleGrants: [],
      })
      setCreateOpen(false)
      navigate({ to: '/projects/$projectName', params: { projectName: createName.trim() } })
    } catch (err) {
      setCreateError(err instanceof Error ? err.message : String(err))
    }
  }

  const handleDeleteConfirm = async () => {
    if (!deleteTarget) return
    try {
      await deleteMutation.mutateAsync({ name: deleteTarget })
      setDeleteOpen(false)
      setDeleteTarget(null)
    } catch { /* error via mutation */ }
  }

  if (isLoading) {
    return (
      <Card>
        <CardContent className="pt-6">
          <div className="flex items-center gap-2">
            <div className="h-5 w-5 animate-spin rounded-full border-2 border-primary border-t-transparent" />
            <span>Loading...</span>
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
          <CardTitle>{effectiveOrg ? `Projects in ${effectiveOrg}` : 'Projects'}</CardTitle>
          <TooltipProvider>
            <Tooltip>
              <TooltipTrigger asChild>
                <span>
                  <Button size="sm" onClick={handleCreateOpen} disabled={!effectiveOrg}>
                    Create Project
                  </Button>
                </span>
              </TooltipTrigger>
              {!effectiveOrg && (
                <TooltipContent><p>Select an organization first</p></TooltipContent>
              )}
            </Tooltip>
          </TooltipProvider>
        </CardHeader>
        <CardContent>
          {projects.length === 0 ? (
            <p className="text-muted-foreground">No projects available.</p>
          ) : (
            <ul className="divide-y">
              {projects.map((project) => (
                <li key={project.name} className="flex items-center gap-2 py-2">
                  <Link
                    to="/projects/$projectName"
                    params={{ projectName: project.name }}
                    className="flex-1 min-w-0 hover:underline"
                  >
                    <div className="font-medium truncate">{project.displayName || project.name}</div>
                    <div className="text-sm text-muted-foreground truncate">{project.description || project.name}</div>
                  </Link>
                  <Badge variant="outline" className="shrink-0">{roleName(project.userRole)}</Badge>
                  {project.userRole === Role.OWNER && (
                    <Button
                      variant="ghost"
                      size="icon"
                      aria-label={`delete ${project.name}`}
                      onClick={() => { setDeleteTarget(project.name); deleteMutation.reset(); setDeleteOpen(true) }}
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  )}
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>

      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Create Project</DialogTitle>
            <DialogDescription>Create a new project. You will be added as the Owner.</DialogDescription>
          </DialogHeader>
          <div className="space-y-3">
            <div>
              <Label>Display Name</Label>
              <Input
                autoFocus
                value={createDisplayName}
                onChange={(e) => {
                  setCreateDisplayName(e.target.value)
                  if (!nameManuallyEdited) setCreateName(slugify(e.target.value))
                }}
                placeholder="My Project"
              />
            </div>
            <div>
              <Label>Name</Label>
              <Input
                value={createName}
                onChange={(e) => {
                  setCreateName(e.target.value)
                  setNameManuallyEdited(e.target.value !== '')
                }}
                placeholder="my-project"
              />
              <p className="text-xs text-muted-foreground mt-1">Lowercase alphanumeric and hyphens</p>
            </div>
            <div>
              <Label>Description</Label>
              <Input
                value={createDescription}
                onChange={(e) => setCreateDescription(e.target.value)}
                placeholder="What is this project for?"
              />
            </div>
            {createError && (
              <Alert variant="destructive"><AlertDescription>{createError}</AlertDescription></Alert>
            )}
          </div>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setCreateOpen(false)}>Cancel</Button>
            <Button onClick={handleCreate} disabled={createMutation.isPending}>
              {createMutation.isPending ? 'Creating...' : 'Create'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete Project</DialogTitle>
            <DialogDescription>
              Are you sure you want to delete project &quot;{deleteTarget}&quot;? This will delete the namespace and all resources within it. This action cannot be undone.
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

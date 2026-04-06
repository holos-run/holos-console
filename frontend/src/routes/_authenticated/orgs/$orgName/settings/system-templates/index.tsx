import { useState } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import { toast } from 'sonner'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'
import { Separator } from '@/components/ui/separator'
import { Badge } from '@/components/ui/badge'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Lock } from 'lucide-react'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { useListSystemTemplates, useCreateSystemTemplate } from '@/queries/system-templates'
import { useGetOrganization } from '@/queries/organizations'

export const Route = createFileRoute('/_authenticated/orgs/$orgName/settings/system-templates/')({
  component: SystemTemplatesListRoute,
})

function SystemTemplatesListRoute() {
  const { orgName } = Route.useParams()
  return <SystemTemplatesListPage orgName={orgName} />
}

export function SystemTemplatesListPage({ orgName: propOrgName }: { orgName?: string } = {}) {
  let routeOrgName: string | undefined
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    routeOrgName = Route.useParams().orgName
  } catch {
    routeOrgName = undefined
  }
  const orgName = propOrgName ?? routeOrgName ?? ''

  const { data: templates, isPending, error } = useListSystemTemplates(orgName)
  const { data: org } = useGetOrganization(orgName)
  const createMutation = useCreateSystemTemplate(orgName)

  const [createOpen, setCreateOpen] = useState(false)
  const [createName, setCreateName] = useState('')
  const [createDisplayName, setCreateDisplayName] = useState('')
  const [createDescription, setCreateDescription] = useState('')
  const [createEnabled, setCreateEnabled] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)

  const userRole = org?.userRole ?? Role.VIEWER
  const canWrite = userRole === Role.OWNER

  const handleOpenCreate = () => {
    setCreateName('')
    setCreateDisplayName('')
    setCreateDescription('')
    setCreateEnabled(false)
    setCreateError(null)
    setCreateOpen(true)
  }

  const handleCreateConfirm = async () => {
    if (!createName.trim()) {
      setCreateError('Name is required')
      return
    }
    setCreateError(null)
    try {
      await createMutation.mutateAsync({
        name: createName.trim(),
        displayName: createDisplayName.trim(),
        description: createDescription.trim(),
        cueTemplate: '',
        mandatory: false,
        enabled: createEnabled,
      })
      toast.success(`Created system template "${createName}"`)
      setCreateOpen(false)
    } catch (err) {
      setCreateError(err instanceof Error ? err.message : String(err))
    }
  }

  if (isPending) {
    return (
      <Card>
        <CardContent className="pt-6 space-y-4">
          <Skeleton className="h-5 w-48" />
          <Skeleton className="h-8 w-full" />
          <Skeleton className="h-8 w-full" />
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
        <CardHeader className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-2">
          <div>
            <p className="text-sm text-muted-foreground">{orgName} / Settings / System Templates</p>
            <CardTitle className="mt-1">System Templates</CardTitle>
          </div>
          {canWrite && (
            <Button size="sm" onClick={handleOpenCreate}>Create Template</Button>
          )}
        </CardHeader>
        <CardContent className="space-y-4">
          <p className="text-sm text-muted-foreground">
            System templates are automatically applied to project namespaces when projects are created.
            Mandatory templates are marked with a lock badge.
          </p>
          <Separator />
          {templates && templates.length > 0 ? (
            <ul className="space-y-2">
              {templates.map((tmpl) => (
                <li key={tmpl.name}>
                  <Link
                    to="/orgs/$orgName/settings/system-templates/$templateName"
                    params={{ orgName, templateName: tmpl.name }}
                    className="flex items-center gap-2 p-3 rounded-md hover:bg-muted transition-colors"
                  >
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2">
                        <span className="text-sm font-medium font-mono">{tmpl.name}</span>
                        {tmpl.mandatory && (
                          <Badge variant="secondary" className="flex items-center gap-1 text-xs">
                            <Lock className="h-3 w-3" />
                            Mandatory
                          </Badge>
                        )}
                        {tmpl.enabled ? (
                          <Badge variant="outline" className="text-xs text-green-500 border-green-500/30">
                            Enabled
                          </Badge>
                        ) : (
                          <Badge variant="outline" className="text-xs text-muted-foreground">
                            Disabled
                          </Badge>
                        )}
                      </div>
                      {tmpl.description && (
                        <p className="text-xs text-muted-foreground truncate mt-0.5">{tmpl.description}</p>
                      )}
                    </div>
                  </Link>
                </li>
              ))}
            </ul>
          ) : (
            <p className="text-sm text-muted-foreground">No system templates found.</p>
          )}
        </CardContent>
      </Card>

      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>Create System Template</DialogTitle>
            <DialogDescription>
              Create a new system template for organization &quot;{orgName}&quot;.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="create-name">Name</Label>
              <Input
                id="create-name"
                aria-label="Name"
                value={createName}
                onChange={(e) => setCreateName(e.target.value)}
                placeholder="my-template"
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="create-display-name">Display Name</Label>
              <Input
                id="create-display-name"
                aria-label="Display Name"
                value={createDisplayName}
                onChange={(e) => setCreateDisplayName(e.target.value)}
                placeholder="My Template"
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="create-description">Description</Label>
              <Input
                id="create-description"
                aria-label="Description"
                value={createDescription}
                onChange={(e) => setCreateDescription(e.target.value)}
                placeholder="What does this template produce?"
              />
            </div>
            <div className="flex items-center gap-3">
              <Switch
                id="create-enabled"
                aria-label="Enabled"
                checked={createEnabled}
                onCheckedChange={setCreateEnabled}
              />
              <Label htmlFor="create-enabled" className="text-sm cursor-pointer">
                Enabled (apply to new project namespaces)
              </Label>
            </div>
          </div>
          {createError && (
            <Alert variant="destructive">
              <AlertDescription>{createError}</AlertDescription>
            </Alert>
          )}
          <DialogFooter>
            <Button variant="ghost" onClick={() => setCreateOpen(false)}>Cancel</Button>
            <Button onClick={handleCreateConfirm} disabled={createMutation.isPending || !createName}>
              {createMutation.isPending ? 'Creating...' : 'Create'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}

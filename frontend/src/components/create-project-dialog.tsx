import { useState } from 'react'
import { useNavigate } from '@tanstack/react-router'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { useListOrganizations } from '@/queries/organizations'
import { useCreateProject } from '@/queries/projects'

export interface CreateProjectDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  defaultOrganization?: string
  onCreated?: (name: string) => void
}

export function CreateProjectDialog({
  open,
  onOpenChange,
  defaultOrganization,
  onCreated,
}: CreateProjectDialogProps) {
  const [name, setName] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [description, setDescription] = useState('')
  const [organization, setOrganization] = useState(defaultOrganization ?? '')
  const [error, setError] = useState<string | null>(null)

  const { data: orgsData } = useListOrganizations()
  const organizations = orgsData?.organizations ?? []

  const { mutateAsync, isPending } = useCreateProject()
  const navigate = useNavigate()

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError(null)
    try {
      const response = await mutateAsync({ name, displayName, description, organization })
      setName('')
      setDisplayName('')
      setDescription('')
      onCreated?.(response.name)
      onOpenChange(false)
      navigate({
        to: '/projects/$projectName/secrets',
        params: { projectName: response.name },
      })
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create project')
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New Project</DialogTitle>
        </DialogHeader>
        <form role="form" onSubmit={handleSubmit}>
          <div className="space-y-4 py-2">
            {error && (
              <Alert variant="destructive">
                <AlertDescription>{error}</AlertDescription>
              </Alert>
            )}
            <div className="space-y-1">
              <Label htmlFor="project-org">Organization</Label>
              <Select defaultValue={defaultOrganization} onValueChange={setOrganization}>
                <SelectTrigger id="project-org">
                  <SelectValue placeholder="Select organization" />
                </SelectTrigger>
                <SelectContent>
                  {organizations.map((org) => (
                    <SelectItem key={org.name} value={org.name}>
                      {org.displayName || org.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1">
              <Label htmlFor="project-name">Name</Label>
              <Input
                id="project-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="my-project"
                pattern="[a-z0-9-]+"
                required
              />
              <p className="text-xs text-muted-foreground">Lowercase letters, numbers, and hyphens only</p>
            </div>
            <div className="space-y-1">
              <Label htmlFor="project-display-name">Display Name</Label>
              <Input
                id="project-display-name"
                value={displayName}
                onChange={(e) => setDisplayName(e.target.value)}
                placeholder="My Project"
              />
            </div>
            <div className="space-y-1">
              <Label htmlFor="project-description">Description</Label>
              <Textarea
                id="project-description"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="Optional description"
              />
            </div>
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={isPending || !name || !organization}>
              {isPending ? 'Creating…' : 'Create'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

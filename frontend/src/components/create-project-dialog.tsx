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
import { toSlug } from '@/lib/slug'

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
  const [displayName, setDisplayName] = useState('')
  const [name, setName] = useState('')
  const [nameEdited, setNameEdited] = useState(false)
  const [description, setDescription] = useState('')
  const [organization, setOrganization] = useState(defaultOrganization ?? '')
  const [error, setError] = useState<string | null>(null)

  const { data: orgsData } = useListOrganizations()
  const organizations = orgsData?.organizations ?? []

  const { mutateAsync, isPending } = useCreateProject()
  const navigate = useNavigate()

  const handleDisplayNameChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const val = e.target.value
    setDisplayName(val)
    if (!nameEdited) {
      setName(toSlug(val))
    }
  }

  const handleNameChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    setNameEdited(true)
    setName(e.target.value)
  }

  const handleResetName = () => {
    setNameEdited(false)
    setName(toSlug(displayName))
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError(null)
    try {
      const response = await mutateAsync({ name, displayName, description, organization })
      setName('')
      setDisplayName('')
      setDescription('')
      setNameEdited(false)
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
              <Label htmlFor="project-display-name">Display Name</Label>
              <Input
                id="project-display-name"
                value={displayName}
                onChange={handleDisplayNameChange}
                placeholder="My Project"
              />
            </div>
            <div className="space-y-1">
              <Label htmlFor="project-name">Name</Label>
              <Input
                id="project-name"
                value={name}
                onChange={handleNameChange}
                placeholder="my-project"
                pattern="[a-z0-9-]+"
                required
              />
              {nameEdited ? (
                <button
                  type="button"
                  className="text-xs text-primary underline"
                  onClick={handleResetName}
                >
                  Auto-derive from display name
                </button>
              ) : (
                <p className="text-xs text-muted-foreground">
                  Auto-derived from display name. Lowercase letters, numbers, and hyphens only.
                </p>
              )}
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

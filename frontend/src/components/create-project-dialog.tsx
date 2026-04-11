import { useState, useEffect } from 'react'
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
import { Combobox } from '@/components/ui/combobox'
import { useListOrganizations, useGetOrganization } from '@/queries/organizations'
import { useCreateProject } from '@/queries/projects'
import { useListFolders } from '@/queries/folders'
import { ParentType } from '@/gen/holos/console/v1/folders_pb'
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
  const [folder, setFolder] = useState('')
  const [error, setError] = useState<string | null>(null)

  const { data: orgsData } = useListOrganizations()
  const organizations = orgsData?.organizations ?? []

  // Fetch folders for the selected organization
  const { data: folders } = useListFolders(organization, ParentType.ORGANIZATION, organization)

  // Fetch org data to get the default folder
  const { data: orgData } = useGetOrganization(organization)

  // Reset folder when the selected organization changes to avoid stale
  // cross-org folder references (the backend rejects them, but the UX
  // should not allow submission in that state).
  useEffect(() => {
    setFolder('')
  }, [organization])

  // When the org data loads (or changes), pre-select the org's default folder
  useEffect(() => {
    if (orgData?.defaultFolder) {
      setFolder(orgData.defaultFolder)
    }
  }, [orgData?.defaultFolder])

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
      const parentType = folder ? ParentType.FOLDER : ParentType.ORGANIZATION
      const parentName = folder || organization
      const response = await mutateAsync({ name, displayName, description, organization, parentType, parentName })
      setName('')
      setDisplayName('')
      setDescription('')
      setFolder('')
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
              <Combobox
                aria-label="Organization"
                items={organizations.map((org) => ({
                  value: org.name,
                  label: org.displayName || org.name,
                }))}
                value={organization}
                onValueChange={setOrganization}
                placeholder="Select organization"
                searchPlaceholder="Search organizations..."
                emptyMessage="No organizations found."
              />
            </div>
            <div className="space-y-1">
              <Label htmlFor="project-folder">Folder</Label>
              <Combobox
                aria-label="Folder"
                items={[
                  { value: '', label: 'None (organization root)' },
                  ...(folders ?? []).map((f) => ({
                    value: f.name,
                    label: f.displayName || f.name,
                  })),
                ]}
                value={folder}
                onValueChange={setFolder}
                placeholder="Select folder"
                searchPlaceholder="Search folders..."
                emptyMessage="No folders found."
              />
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

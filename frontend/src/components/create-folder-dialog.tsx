import { useState } from 'react'
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
import { useCreateFolder } from '@/queries/folders'
import { toSlug } from '@/lib/slug'
import type { ParentType } from '@/gen/holos/console/v1/folders_pb'

export interface CreateFolderDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  organization: string
  parentType: ParentType
  parentName: string
  onCreated?: (name: string) => void
}

export function CreateFolderDialog({
  open,
  onOpenChange,
  organization,
  parentType,
  parentName,
  onCreated,
}: CreateFolderDialogProps) {
  const [displayName, setDisplayName] = useState('')
  const [name, setName] = useState('')
  const [nameEdited, setNameEdited] = useState(false)
  const [description, setDescription] = useState('')
  const [error, setError] = useState<string | null>(null)

  const { mutateAsync, isPending } = useCreateFolder(organization)

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
      const response = await mutateAsync({ name, displayName, description, parentType, parentName })
      setName('')
      setDisplayName('')
      setDescription('')
      setNameEdited(false)
      onCreated?.(response.name ?? name)
      onOpenChange(false)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create folder')
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New Folder</DialogTitle>
        </DialogHeader>
        <form role="form" onSubmit={handleSubmit}>
          <div className="space-y-4 py-2">
            {error && (
              <Alert variant="destructive">
                <AlertDescription>{error}</AlertDescription>
              </Alert>
            )}
            <div className="space-y-1">
              <Label htmlFor="folder-display-name">Display Name</Label>
              <Input
                id="folder-display-name"
                value={displayName}
                onChange={handleDisplayNameChange}
                placeholder="Payments Team"
              />
            </div>
            <div className="space-y-1">
              <Label htmlFor="folder-name">Name</Label>
              <Input
                id="folder-name"
                value={name}
                onChange={handleNameChange}
                placeholder="payments"
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
              <Label htmlFor="folder-description">Description</Label>
              <Textarea
                id="folder-description"
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
            <Button type="submit" disabled={isPending || !name}>
              {isPending ? 'Creating…' : 'Create'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

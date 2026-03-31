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
import { useCreateOrganization } from '@/queries/organizations'
import { toSlug } from '@/lib/slug'

export interface CreateOrgDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onCreated?: (name: string) => void
}

export function CreateOrgDialog({ open, onOpenChange, onCreated }: CreateOrgDialogProps) {
  const [displayName, setDisplayName] = useState('')
  const [name, setName] = useState('')
  const [nameEdited, setNameEdited] = useState(false)
  const [description, setDescription] = useState('')
  const [error, setError] = useState<string | null>(null)

  const { mutateAsync, isPending } = useCreateOrganization()

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
      const response = await mutateAsync({ name, displayName, description })
      setName('')
      setDisplayName('')
      setDescription('')
      setNameEdited(false)
      onCreated?.(response.name)
      onOpenChange(false)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create organization')
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New Organization</DialogTitle>
        </DialogHeader>
        <form role="form" onSubmit={handleSubmit}>
          <div className="space-y-4 py-2">
            {error && (
              <Alert variant="destructive">
                <AlertDescription>{error}</AlertDescription>
              </Alert>
            )}
            <div className="space-y-1">
              <Label htmlFor="org-display-name">Display Name</Label>
              <Input
                id="org-display-name"
                value={displayName}
                onChange={handleDisplayNameChange}
                placeholder="My Organization"
                autoFocus
              />
            </div>
            <div className="space-y-1">
              <Label htmlFor="org-name">Name</Label>
              <Input
                id="org-name"
                value={name}
                onChange={handleNameChange}
                placeholder="my-org"
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
              <Label htmlFor="org-description">Description</Label>
              <Textarea
                id="org-description"
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

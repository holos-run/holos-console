/**
 * ConfirmDeleteDialog — shared shadcn/ui Dialog for destructive deletes.
 *
 * Shows the resource name and namespace, then calls `onConfirm()` on Delete.
 * The caller owns the `open` / `onOpenChange` state so multiple rows in a
 * grid can share a single instance of this dialog.
 */

import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'

export interface ConfirmDeleteDialogProps {
  /** Controls whether the dialog is open. */
  open: boolean
  /** Called when the dialog should close (cancel or after confirm). */
  onOpenChange: (open: boolean) => void
  /**
   * Display name of the resource to delete (shown in the dialog body).
   * Falls back to the `name` field if empty.
   */
  displayName?: string
  /** Kubernetes resource name shown in the description. */
  name: string
  /** Kubernetes namespace shown in the description. */
  namespace: string
  /**
   * Async function invoked when the user clicks Delete.
   * The dialog stays open while this is in progress.
   */
  onConfirm: () => Promise<void>
  /** Whether the delete mutation is currently in flight. */
  isDeleting?: boolean
  /** If set, renders a destructive alert inside the dialog. */
  error?: Error | null
}

export function ConfirmDeleteDialog({
  open,
  onOpenChange,
  displayName,
  name,
  namespace,
  onConfirm,
  isDeleting = false,
  error,
}: ConfirmDeleteDialogProps) {
  const label = displayName || name

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Delete Resource</DialogTitle>
          <DialogDescription>
            Are you sure you want to delete <strong>{label}</strong> in
            namespace <code>{namespace}</code>? This action cannot be undone.
          </DialogDescription>
        </DialogHeader>
        {error && (
          <Alert variant="destructive">
            <AlertDescription>{error.message}</AlertDescription>
          </Alert>
        )}
        <DialogFooter>
          <Button
            variant="ghost"
            onClick={() => onOpenChange(false)}
            disabled={isDeleting}
          >
            Cancel
          </Button>
          <Button
            variant="destructive"
            onClick={onConfirm}
            disabled={isDeleting}
          >
            {isDeleting ? 'Deleting…' : 'Delete'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

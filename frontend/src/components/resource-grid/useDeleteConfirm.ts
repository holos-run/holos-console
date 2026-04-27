import { useCallback, useState } from 'react'
import { toast } from 'sonner'

import { connectErrorMessage } from '@/lib/connect-toast'
import type { Row } from './types'

interface UseDeleteConfirmOptions {
  onDelete: (row: Row) => Promise<void>
}

export function useDeleteConfirm({ onDelete }: UseDeleteConfirmOptions) {
  const [deleteTarget, setDeleteTarget] = useState<Row | null>(null)
  const [isDeleting, setIsDeleting] = useState(false)
  const [deleteError, setDeleteError] = useState<Error | null>(null)

  const handleDeleteClick = useCallback((row: Row) => {
    setDeleteTarget(row)
    setDeleteError(null)
  }, [])

  const handleDeleteConfirm = useCallback(async () => {
    if (!deleteTarget) return
    setIsDeleting(true)
    setDeleteError(null)
    try {
      await onDelete(deleteTarget)
      setDeleteTarget(null)
      toast.success(`Deleted ${deleteTarget.displayName || deleteTarget.name}`)
    } catch (err) {
      const e = err instanceof Error ? err : new Error(String(err))
      setDeleteError(e)
      toast.error(connectErrorMessage(err))
    } finally {
      setIsDeleting(false)
    }
  }, [deleteTarget, onDelete])

  const handleDeleteOpenChange = useCallback((open: boolean) => {
    if (!open) setDeleteTarget(null)
  }, [])

  return {
    deleteTarget,
    isDeleting,
    deleteError,
    handleDeleteClick,
    handleDeleteConfirm,
    handleDeleteOpenChange,
  }
}

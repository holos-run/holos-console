import { useState } from 'react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
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
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { ArrowUpCircle, AlertTriangle } from 'lucide-react'
import { useCheckUpdates, useUpdateTemplate } from '@/queries/templates'
import type { TemplateScopeRef, TemplateUpdate, LinkedTemplateRef } from '@/queries/templates'
import { TemplateScope } from '@/gen/holos/console/v1/policy_state_pb.js'
import { toast } from 'sonner'

// scopeLabel returns a human-readable scope label.
function scopeLabel(scope: number | undefined): string {
  if (scope === TemplateScope.ORGANIZATION) return 'Org'
  if (scope === TemplateScope.FOLDER) return 'Folder'
  return ''
}

// hasCompatibleUpdate returns true when a non-breaking compatible update exists.
function hasCompatibleUpdate(update: TemplateUpdate): boolean {
  return !!update.latestCompatibleVersion && update.latestCompatibleVersion !== update.currentVersion
}

// --- UpdatesAvailableBadge ---

interface UpdatesAvailableBadgeProps {
  scope: TemplateScopeRef
  templateName: string
  onClick?: () => void
}

/** Pill badge showing "N updates" for a template with available linked-template updates. */
export function UpdatesAvailableBadge({ scope, templateName, onClick }: UpdatesAvailableBadgeProps) {
  const { data: updates, isPending } = useCheckUpdates(scope, templateName)

  if (isPending || !updates || updates.length === 0) return null

  const count = updates.length
  const label = count === 1 ? '1 update' : `${count} updates`

  return (
    <Badge
      variant="secondary"
      className="cursor-pointer gap-1"
      onClick={onClick}
    >
      <ArrowUpCircle className="h-3 w-3" />
      {label}
    </Badge>
  )
}

// --- UpgradeDialog ---

interface UpgradeDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  updates: TemplateUpdate[]
  scope: TemplateScopeRef
  templateName: string
  linkedTemplates: LinkedTemplateRef[]
}

/** Dialog showing available updates for linked templates with upgrade actions. */
export function UpgradeDialog({
  open,
  onOpenChange,
  updates,
  scope,
  templateName,
  linkedTemplates,
}: UpgradeDialogProps) {
  const updateMutation = useUpdateTemplate(scope, templateName)
  const [error, setError] = useState<string | null>(null)
  const [confirmBreaking, setConfirmBreaking] = useState<TemplateUpdate | null>(null)

  const compatibleUpdates = updates.filter(hasCompatibleUpdate)
  const hasMultipleCompatible = compatibleUpdates.length > 1

  // buildUpdatedLinkedTemplates replaces the versionConstraint for a specific
  // linked template ref, targeting the new version range.
  function buildUpdatedLinkedTemplates(
    currentLinked: LinkedTemplateRef[],
    update: TemplateUpdate,
    newVersion: string,
  ): LinkedTemplateRef[] {
    return currentLinked.map((lt) => {
      if (
        lt.scope === update.ref?.scope &&
        lt.scopeName === update.ref?.scopeName &&
        lt.name === update.ref?.name
      ) {
        // Build new constraint: >=newVersion <nextMajor
        const major = parseInt(newVersion.split('.')[0], 10)
        const newConstraint = `>=${newVersion} <${major + 1}.0.0`
        return { ...lt, versionConstraint: newConstraint }
      }
      return lt
    })
  }

  // handleUpdateSingle updates one linked template's version constraint.
  // Uses the same isBreaking classification as the display to pick the right
  // target version: compatible when available, breaking only when no compatible
  // update exists.
  async function handleUpdateSingle(update: TemplateUpdate) {
    setError(null)
    const isBreaking = update.breakingUpdateAvailable && !hasCompatibleUpdate(update)
    const targetVersion = isBreaking
      ? update.latestVersion
      : update.latestCompatibleVersion
    if (!targetVersion) return

    try {
      const newLinked = buildUpdatedLinkedTemplates(linkedTemplates, update, targetVersion)
      await updateMutation.mutateAsync({
        linkedTemplates: newLinked,
        updateLinkedTemplates: true,
      })
      toast.success(`Updated ${update.ref?.name} to ${targetVersion}`)
      onOpenChange(false)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  // handleUpdateAllCompatible bulk-updates all non-breaking compatible updates.
  async function handleUpdateAllCompatible() {
    setError(null)
    try {
      let updated = [...linkedTemplates]
      for (const update of compatibleUpdates) {
        updated = buildUpdatedLinkedTemplates(updated, update, update.latestCompatibleVersion)
      }
      await updateMutation.mutateAsync({
        linkedTemplates: updated,
        updateLinkedTemplates: true,
      })
      toast.success(`Updated ${compatibleUpdates.length} linked templates`)
      onOpenChange(false)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  return (
    <>
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>Available Updates</DialogTitle>
          <DialogDescription>
            Updates available for linked platform templates.
          </DialogDescription>
        </DialogHeader>

        {updates.length === 0 ? (
          <p className="text-sm text-muted-foreground py-4 text-center">No updates available.</p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Template</TableHead>
                <TableHead>Current</TableHead>
                <TableHead>Available</TableHead>
                <TableHead></TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {updates.map((update) => {
                const ref = update.ref
                const isBreaking = update.breakingUpdateAvailable && !hasCompatibleUpdate(update)
                const targetVersion = isBreaking
                  ? update.latestVersion
                  : update.latestCompatibleVersion
                return (
                  <TableRow key={`${ref?.scope}/${ref?.scopeName}/${ref?.name}`}>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <span className="font-medium">{ref?.name}</span>
                        {ref && (
                          <span className="text-xs text-muted-foreground">
                            {scopeLabel(ref.scope)}
                          </span>
                        )}
                      </div>
                    </TableCell>
                    <TableCell>
                      <span className="font-mono text-sm">{update.currentVersion}</span>
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <span className="font-mono text-sm">{targetVersion}</span>
                        {isBreaking && (
                          <Badge variant="destructive" className="text-xs gap-1">
                            <AlertTriangle className="h-3 w-3" />
                            breaking
                          </Badge>
                        )}
                      </div>
                    </TableCell>
                    <TableCell className="text-right">
                      {isBreaking ? (
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={() => setConfirmBreaking(update)}
                          disabled={updateMutation.isPending}
                        >
                          Upgrade
                        </Button>
                      ) : (
                        <Button
                          size="sm"
                          onClick={() => handleUpdateSingle(update)}
                          disabled={updateMutation.isPending}
                        >
                          Update
                        </Button>
                      )}
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        )}

        {error && (
          <Alert variant="destructive">
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        )}

        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Close
          </Button>
          {hasMultipleCompatible && (
            <Button onClick={handleUpdateAllCompatible} disabled={updateMutation.isPending}>
              {updateMutation.isPending ? 'Updating...' : 'Update All Compatible'}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>

    {/* Breaking upgrade confirmation dialog */}
    <Dialog open={!!confirmBreaking} onOpenChange={(open) => { if (!open) setConfirmBreaking(null) }}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>Confirm Breaking Upgrade</DialogTitle>
          <DialogDescription>
            Upgrading <strong>{confirmBreaking?.ref?.name}</strong> from{' '}
            <span className="font-mono">{confirmBreaking?.currentVersion}</span> to{' '}
            <span className="font-mono">{confirmBreaking?.latestVersion}</span> is a
            breaking change. This may require changes to your template.
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="ghost" onClick={() => setConfirmBreaking(null)}>
            Cancel
          </Button>
          <Button
            variant="destructive"
            onClick={async () => {
              const update = confirmBreaking
              setConfirmBreaking(null)
              await handleUpdateSingle(update!)
            }}
            disabled={updateMutation.isPending}
          >
            {updateMutation.isPending ? 'Upgrading...' : 'Confirm Upgrade'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
    </>
  )
}

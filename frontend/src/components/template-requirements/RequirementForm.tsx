import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Separator } from '@/components/ui/separator'
import type { LinkedTemplateRef } from '@/queries/templateRequirements'
import { connectErrorMessage } from '@/lib/connect-toast'

/**
 * RequirementFormValues are the shape of data emitted by RequirementForm on submit.
 */
export type RequirementFormValues = {
  name: string
  requires: LinkedTemplateRef
  cascadeDelete: boolean
}

export type RequirementFormProps = {
  mode: 'create' | 'edit'
  namespace: string
  canWrite: boolean
  initialValues?: {
    name: string
    requiresNamespace: string
    requiresName: string
    cascadeDelete: boolean
  }
  submitLabel: string
  pendingLabel: string
  onSubmit: (values: RequirementFormValues) => Promise<void>
  onCancel: () => void
  isPending?: boolean
  lockName?: boolean
}

/**
 * RequirementForm renders the shared create/edit form for a TemplateRequirement.
 * It handles both create and edit modes. In create mode the name field is
 * editable. In edit mode the name is locked.
 */
export function RequirementForm({
  canWrite,
  initialValues,
  submitLabel,
  pendingLabel,
  onSubmit,
  onCancel,
  isPending = false,
  lockName = false,
}: RequirementFormProps) {
  const [name, setName] = useState(initialValues?.name ?? '')
  const [requiresNamespace, setRequiresNamespace] = useState(
    initialValues?.requiresNamespace ?? '',
  )
  const [requiresName, setRequiresName] = useState(initialValues?.requiresName ?? '')
  const [cascadeDelete, setCascadeDelete] = useState(initialValues?.cascadeDelete ?? true)
  const [error, setError] = useState<string | null>(null)

  const handleSubmit = async () => {
    setError(null)

    if (!name.trim()) {
      setError('Requirement name is required.')
      return
    }
    if (!requiresNamespace.trim() || !requiresName.trim()) {
      setError('Requires namespace and name are required.')
      return
    }

    try {
      await onSubmit({
        name: name.trim(),
        requires: {
          $typeName: 'holos.console.v1.LinkedTemplateRef',
          namespace: requiresNamespace.trim(),
          name: requiresName.trim(),
          versionConstraint: '',
        },
        cascadeDelete,
      })
    } catch (err) {
      setError(connectErrorMessage(err))
    }
  }

  return (
    <div className="space-y-4">
      <div className="rounded-md border border-border p-3 text-sm text-muted-foreground">
        A TemplateRequirement records that project templates or deployment instances in this
        organization require another template. Cross-namespace requires references are authorised
        by TemplateGrant at reconcile time.
      </div>

      <div>
        <Label htmlFor="requirement-name">Name (slug)</Label>
        <Input
          id="requirement-name"
          aria-label="Requirement name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="my-requirement"
          disabled={!canWrite || lockName}
        />
        <p className="text-xs text-muted-foreground mt-1">
          {lockName
            ? 'Requirement names are immutable after creation.'
            : 'Lowercase alphanumeric and hyphens only.'}
        </p>
      </div>

      <Separator />

      <div className="space-y-2">
        <Label>Requires</Label>
        <p className="text-xs text-muted-foreground">
          The template that matching targets must have applied.
        </p>
        <div className="grid grid-cols-2 gap-2">
          <div>
            <Label htmlFor="requires-namespace" className="text-xs">
              Namespace
            </Label>
            <Input
              id="requires-namespace"
              aria-label="Requires namespace"
              value={requiresNamespace}
              onChange={(e) => setRequiresNamespace(e.target.value)}
              placeholder="holos-org-my-org"
              disabled={!canWrite}
            />
          </div>
          <div>
            <Label htmlFor="requires-name" className="text-xs">
              Name
            </Label>
            <Input
              id="requires-name"
              aria-label="Requires name"
              value={requiresName}
              onChange={(e) => setRequiresName(e.target.value)}
              placeholder="base-template"
              disabled={!canWrite}
            />
          </div>
        </div>
      </div>

      <Separator />

      <div className="flex items-center gap-2">
        <input
          id="cascade-delete"
          type="checkbox"
          checked={cascadeDelete}
          onChange={(e) => setCascadeDelete(e.target.checked)}
          disabled={!canWrite}
          aria-label="Cascade delete"
          className="h-4 w-4"
        />
        <Label htmlFor="cascade-delete" className="font-normal">
          Cascade delete — deleting the required template also removes matching targets
        </Label>
      </div>

      {error && (
        <Alert variant="destructive" data-testid="requirement-form-error">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}

      <div className="flex items-center gap-3 pt-2">
        <Button onClick={handleSubmit} disabled={isPending || !canWrite}>
          {isPending ? pendingLabel : submitLabel}
        </Button>
        <Button variant="ghost" type="button" aria-label="Cancel" onClick={onCancel}>
          Cancel
        </Button>
      </div>
    </div>
  )
}

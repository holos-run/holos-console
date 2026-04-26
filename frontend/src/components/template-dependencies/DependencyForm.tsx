import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Separator } from '@/components/ui/separator'
import type { LinkedTemplateRef } from '@/queries/templateDependencies'

/**
 * DependencyFormValues are the shape of data emitted by DependencyForm on submit.
 */
export type DependencyFormValues = {
  name: string
  dependent: LinkedTemplateRef
  requires: LinkedTemplateRef
  cascadeDelete: boolean
}

export type DependencyFormProps = {
  mode: 'create' | 'edit'
  namespace: string
  canWrite: boolean
  initialValues?: {
    name: string
    dependentNamespace: string
    dependentName: string
    requiresNamespace: string
    requiresName: string
    cascadeDelete: boolean
  }
  submitLabel: string
  pendingLabel: string
  onSubmit: (values: DependencyFormValues) => Promise<void>
  onCancel: () => void
  isPending?: boolean
  lockName?: boolean
}

/**
 * DependencyForm renders the shared create/edit form for a TemplateDependency.
 * It handles both create and edit modes. In create mode the name field is
 * auto-derived from the dependent and requires names. In edit mode the name is
 * locked.
 */
export function DependencyForm({
  mode,
  namespace,
  canWrite,
  initialValues,
  submitLabel,
  pendingLabel,
  onSubmit,
  onCancel,
  isPending = false,
  lockName = false,
}: DependencyFormProps) {
  const [name, setName] = useState(initialValues?.name ?? '')
  const [dependentNamespace, setDependentNamespace] = useState(
    initialValues?.dependentNamespace ?? namespace,
  )
  const [dependentName, setDependentName] = useState(initialValues?.dependentName ?? '')
  const [requiresNamespace, setRequiresNamespace] = useState(
    initialValues?.requiresNamespace ?? '',
  )
  const [requiresName, setRequiresName] = useState(initialValues?.requiresName ?? '')
  const [cascadeDelete, setCascadeDelete] = useState(initialValues?.cascadeDelete ?? true)
  const [error, setError] = useState<string | null>(null)

  const slugify = (val: string) =>
    val.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '')

  const deriveNameFromFields = (depName: string, reqName: string) => {
    if (!depName && !reqName) return ''
    const parts = [depName, reqName].filter(Boolean)
    return slugify(parts.join('-depends-on-'))
  }

  const handleDependentNameChange = (val: string) => {
    setDependentName(val)
    if (mode === 'create' && !lockName) {
      setName(deriveNameFromFields(val, requiresName))
    }
  }

  const handleRequiresNameChange = (val: string) => {
    setRequiresName(val)
    if (mode === 'create' && !lockName) {
      setName(deriveNameFromFields(dependentName, val))
    }
  }

  const handleSubmit = async () => {
    setError(null)

    if (!name.trim()) {
      setError('Dependency name is required.')
      return
    }
    if (!dependentNamespace.trim() || !dependentName.trim()) {
      setError('Dependent namespace and name are required.')
      return
    }
    if (!requiresNamespace.trim() || !requiresName.trim()) {
      setError('Requires namespace and name are required.')
      return
    }

    try {
      await onSubmit({
        name: name.trim(),
        dependent: {
          $typeName: 'holos.console.v1.LinkedTemplateRef',
          namespace: dependentNamespace.trim(),
          name: dependentName.trim(),
          versionConstraint: '',
        },
        requires: {
          $typeName: 'holos.console.v1.LinkedTemplateRef',
          namespace: requiresNamespace.trim(),
          name: requiresName.trim(),
          versionConstraint: '',
        },
        cascadeDelete,
      })
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  return (
    <div className="space-y-4">
      <div className="rounded-md border border-border p-3 text-sm text-muted-foreground">
        A TemplateDependency records that one project template or deployment instance depends on
        another template. Cross-namespace requires references are authorised by TemplateGrant at
        reconcile time.
      </div>

      <div>
        <Label htmlFor="dependency-name">Name (slug)</Label>
        <Input
          id="dependency-name"
          aria-label="Dependency name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="my-template-depends-on-base"
          disabled={!canWrite || lockName}
        />
        <p className="text-xs text-muted-foreground mt-1">
          {lockName
            ? 'Dependency names are immutable after creation.'
            : 'Auto-derived from dependent and requires names. Lowercase alphanumeric and hyphens only.'}
        </p>
      </div>

      <Separator />

      <div className="space-y-2">
        <Label>Dependent</Label>
        <p className="text-xs text-muted-foreground">
          The template or deployment instance that has this dependency.
        </p>
        <div className="grid grid-cols-2 gap-2">
          <div>
            <Label htmlFor="dependent-namespace" className="text-xs">
              Namespace
            </Label>
            <Input
              id="dependent-namespace"
              aria-label="Dependent namespace"
              value={dependentNamespace}
              onChange={(e) => setDependentNamespace(e.target.value)}
              placeholder="holos-project-my-project"
              disabled={!canWrite}
            />
          </div>
          <div>
            <Label htmlFor="dependent-name" className="text-xs">
              Name
            </Label>
            <Input
              id="dependent-name"
              aria-label="Dependent name"
              value={dependentName}
              onChange={(e) => handleDependentNameChange(e.target.value)}
              placeholder="my-template"
              disabled={!canWrite}
            />
          </div>
        </div>
      </div>

      <div className="space-y-2">
        <Label>Requires</Label>
        <p className="text-xs text-muted-foreground">
          The template that the dependent needs.
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
              onChange={(e) => handleRequiresNameChange(e.target.value)}
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
          Cascade delete — deleting the required template also deletes the dependent
        </Label>
      </div>

      {error && (
        <Alert variant="destructive" data-testid="dependency-form-error">
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

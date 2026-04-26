import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Separator } from '@/components/ui/separator'
import type { TemplateGrantFromRef, TemplateGrantToRef } from '@/queries/templateGrants'

/**
 * GrantFormValues are the shape of data emitted by GrantForm on submit.
 */
export type GrantFormValues = {
  name: string
  from: TemplateGrantFromRef[]
  to: TemplateGrantToRef[]
}

export type GrantFormProps = {
  mode: 'create' | 'edit'
  namespace: string
  canWrite: boolean
  initialValues?: {
    name: string
    fromNamespace: string
    toNamespace: string
    toName: string
  }
  submitLabel: string
  pendingLabel: string
  onSubmit: (values: GrantFormValues) => Promise<void>
  onCancel: () => void
  isPending?: boolean
  lockName?: boolean
}

/**
 * GrantForm renders the shared create/edit form for a TemplateGrant.
 * It handles both create and edit modes. In create mode the name field is
 * editable. In edit mode the name is locked.
 *
 * A TemplateGrant authorizes cross-namespace template references by specifying
 * which namespaces (from) are permitted to reference templates in the grant's
 * namespace, optionally narrowed to specific templates (to).
 */
export function GrantForm({
  mode: _mode,
  namespace: _namespace,
  canWrite,
  initialValues,
  submitLabel,
  pendingLabel,
  onSubmit,
  onCancel,
  isPending = false,
  lockName = false,
}: GrantFormProps) {
  const [name, setName] = useState(initialValues?.name ?? '')
  const [fromNamespace, setFromNamespace] = useState(initialValues?.fromNamespace ?? '')
  const [toNamespace, setToNamespace] = useState(initialValues?.toNamespace ?? '')
  const [toName, setToName] = useState(initialValues?.toName ?? '')
  const [error, setError] = useState<string | null>(null)

  const handleSubmit = async () => {
    setError(null)

    if (!name.trim()) {
      setError('Grant name is required.')
      return
    }
    if (!fromNamespace.trim()) {
      setError('From namespace is required.')
      return
    }

    const fromRef: TemplateGrantFromRef = {
      $typeName: 'holos.console.v1.TemplateGrantFromRef',
      namespace: fromNamespace.trim(),
    }

    const toRefs: TemplateGrantToRef[] =
      toNamespace.trim() && toName.trim()
        ? [
            {
              $typeName: 'holos.console.v1.TemplateGrantToRef',
              namespace: toNamespace.trim(),
              name: toName.trim(),
            },
          ]
        : []

    try {
      await onSubmit({
        name: name.trim(),
        from: [fromRef],
        to: toRefs,
      })
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  return (
    <div className="space-y-4">
      <div className="rounded-md border border-border p-3 text-sm text-muted-foreground">
        A TemplateGrant authorizes cross-namespace template references. It lives in the namespace
        that owns the templates being shared and permits specified namespaces to reference them,
        mirroring the Gateway API ReferenceGrant pattern.
      </div>

      <div>
        <Label htmlFor="grant-name">Name (slug)</Label>
        <Input
          id="grant-name"
          aria-label="Grant name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="allow-project-foo"
          disabled={!canWrite || lockName}
        />
        <p className="text-xs text-muted-foreground mt-1">
          {lockName
            ? 'Grant names are immutable after creation.'
            : 'Lowercase alphanumeric and hyphens only.'}
        </p>
      </div>

      <Separator />

      <div className="space-y-2">
        <Label>From</Label>
        <p className="text-xs text-muted-foreground">
          The namespace permitted to reference templates in this grant&apos;s namespace. Use{' '}
          <code className="font-mono">*</code> to permit all namespaces.
        </p>
        <div>
          <Label htmlFor="from-namespace" className="text-xs">
            Namespace
          </Label>
          <Input
            id="from-namespace"
            aria-label="From namespace"
            value={fromNamespace}
            onChange={(e) => setFromNamespace(e.target.value)}
            placeholder="holos-project-my-project"
            disabled={!canWrite}
          />
        </div>
      </div>

      <Separator />

      <div className="space-y-2">
        <Label>To (optional)</Label>
        <p className="text-xs text-muted-foreground">
          Optionally narrow which template may be referenced. Leave both fields empty to permit all
          templates in this namespace.
        </p>
        <div className="grid grid-cols-2 gap-2">
          <div>
            <Label htmlFor="to-namespace" className="text-xs">
              Namespace
            </Label>
            <Input
              id="to-namespace"
              aria-label="To namespace"
              value={toNamespace}
              onChange={(e) => setToNamespace(e.target.value)}
              placeholder="holos-org-my-org"
              disabled={!canWrite}
            />
          </div>
          <div>
            <Label htmlFor="to-name" className="text-xs">
              Name
            </Label>
            <Input
              id="to-name"
              aria-label="To name"
              value={toName}
              onChange={(e) => setToName(e.target.value)}
              placeholder="base-template"
              disabled={!canWrite}
            />
          </div>
        </div>
      </div>

      {error && (
        <Alert variant="destructive" data-testid="grant-form-error">
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

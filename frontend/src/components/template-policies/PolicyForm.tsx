import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Separator } from '@/components/ui/separator'
import { RuleEditor } from '@/components/template-policies/RuleEditor'
import {
  newEmptyRule,
  ruleDraftToProto,
  validateRuleDraft,
  type RuleDraft,
} from '@/components/template-policies/rule-draft'
import { useListLinkableTemplates } from '@/queries/templates'
import type { TemplateScopeRef } from '@/queries/templates'

/**
 * PolicyScope captures the allowed scope types for a template policy. The
 * form-level guard rejects any value other than ORGANIZATION or FOLDER. The
 * scope is expected to be derived from the URL by the caller (folder vs org
 * route); this type exists to enforce the narrowing explicitly.
 */
export type PolicyScope = 'organization' | 'folder' | 'project' | 'unknown'

export type PolicyFormProps = {
  mode: 'create' | 'edit'
  scopeType: PolicyScope
  scopeRef: TemplateScopeRef
  canWrite: boolean
  initialValues?: {
    name: string
    displayName: string
    description: string
    rules: RuleDraft[]
  }
  submitLabel: string
  pendingLabel: string
  onSubmit: (values: {
    name: string
    displayName: string
    description: string
    rules: ReturnType<typeof ruleDraftToProto>[]
  }) => Promise<void>
  onCancel: () => void
  isPending?: boolean
  lockName?: boolean
}

/**
 * PolicyForm renders the shared create/edit form for a TemplatePolicy. It
 * enforces the scope guard described in HOL-558: the form refuses to submit
 * when `scopeType` is not `organization` or `folder`, regardless of what
 * URL or caller supplied.
 */
export function PolicyForm({
  mode,
  scopeType,
  scopeRef,
  canWrite,
  initialValues,
  submitLabel,
  pendingLabel,
  onSubmit,
  onCancel,
  isPending = false,
  lockName = false,
}: PolicyFormProps) {
  const [name, setName] = useState(initialValues?.name ?? '')
  const [displayName, setDisplayName] = useState(initialValues?.displayName ?? '')
  const [description, setDescription] = useState(initialValues?.description ?? '')
  const [rules, setRules] = useState<RuleDraft[]>(
    initialValues?.rules ?? [newEmptyRule()],
  )
  const [error, setError] = useState<string | null>(null)

  const { data: linkableTemplates = [] } = useListLinkableTemplates(scopeRef)

  const slugify = (val: string) =>
    val.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '')

  const handleDisplayNameChange = (val: string) => {
    setDisplayName(val)
    if (mode === 'create' && !lockName) {
      setName(slugify(val))
    }
  }

  const handleSubmit = async () => {
    setError(null)

    // Scope guard: policies can only be authored at folder or organization
    // scope. Anything else is a programmer error or a contrived URL; the form
    // refuses to dispatch the mutation and surfaces the constraint to the
    // user. The backend performs the authoritative check, but the UI must
    // make it clear before round-tripping.
    if (scopeType !== 'organization' && scopeType !== 'folder') {
      setError(
        'Template policies can only be created at folder or organization scope. Navigate to a folder or organization to manage policies.',
      )
      return
    }

    if (!name.trim()) {
      setError('Policy name is required.')
      return
    }
    if (rules.length === 0) {
      setError('A policy must have at least one rule.')
      return
    }
    for (let i = 0; i < rules.length; i++) {
      const ruleErr = validateRuleDraft(rules[i])
      if (ruleErr) {
        setError(`Rule ${i + 1}: ${ruleErr}`)
        return
      }
    }

    try {
      await onSubmit({
        name: name.trim(),
        displayName: displayName.trim(),
        description: description.trim(),
        rules: rules.map(ruleDraftToProto),
      })
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  return (
    <div className="space-y-4">
      <div className="rounded-md border border-border p-3 text-sm text-muted-foreground">
        Template policies apply to BOTH project templates and deployments. Rules use glob
        patterns against project and deployment names. To require a template on every
        project template and deployment, leave both patterns as <code>*</code>.
      </div>

      <div>
        <Label htmlFor="policy-display-name">Display Name</Label>
        <Input
          id="policy-display-name"
          aria-label="Display Name"
          autoFocus
          value={displayName}
          onChange={(e) => handleDisplayNameChange(e.target.value)}
          placeholder="My Policy"
          disabled={!canWrite}
        />
      </div>

      <div>
        <Label htmlFor="policy-name">Name (slug)</Label>
        <Input
          id="policy-name"
          aria-label="Name slug"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="my-policy"
          disabled={!canWrite || lockName}
        />
        <p className="text-xs text-muted-foreground mt-1">
          {lockName
            ? 'Policy names are immutable after creation.'
            : 'Auto-derived from display name. Lowercase alphanumeric and hyphens only.'}
        </p>
      </div>

      <div>
        <Label htmlFor="policy-description">Description</Label>
        <Textarea
          id="policy-description"
          aria-label="Description"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          placeholder="What does this policy enforce?"
          disabled={!canWrite}
          rows={3}
        />
      </div>

      <Separator />

      <div className="space-y-2">
        <div className="flex items-center justify-between">
          <Label>Rules</Label>
          <p className="text-xs text-muted-foreground">
            Scope: {scopeType === 'folder' ? 'Folder' : scopeType === 'organization' ? 'Organization' : 'Invalid'}
          </p>
        </div>
        <RuleEditor
          rules={rules}
          onChange={setRules}
          linkableTemplates={linkableTemplates}
          disabled={!canWrite}
        />
      </div>

      {error && (
        <Alert variant="destructive" data-testid="policy-form-error">
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

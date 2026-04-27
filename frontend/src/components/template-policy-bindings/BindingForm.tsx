import { useMemo, useState } from 'react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Separator } from '@/components/ui/separator'
import { Combobox, type ComboboxItem } from '@/components/ui/combobox'
import { TargetRefEditor } from './TargetRefEditor'
import {
  MatchesPreview,
  type MatchesPreviewParentScope,
} from './MatchesPreview'
import {
  applyPolicyKey,
  draftToMutationParams,
  newEmptyBindingDraft,
  policyKey,
  validateBindingDraft,
  type BindingDraft,
  type BindingMutationParams,
} from './binding-draft'
import { useListTemplatePolicies } from '@/queries/templatePolicies'
import {
  scopeLabelFromNamespace,
  scopeNameFromNamespace,
} from '@/lib/scope-labels'
import { connectErrorMessage } from '@/lib/connect-toast'

/**
 * BindingScope captures the allowed scope types for a TemplatePolicyBinding.
 * The form-level guard rejects any value other than ORGANIZATION or FOLDER.
 * Mirrors the PolicyScope type in PolicyForm.tsx — bindings live only where
 * their referenced policy can live.
 */
export type BindingScope = 'organization' | 'folder' | 'project' | 'unknown'

export type BindingFormProps = {
  mode: 'create' | 'edit'
  scopeType: BindingScope
  /** Namespace the binding will be created in. Drives policy picker scope. */
  namespace: string
  /** Organization that owns the scope — required to populate the per-row
   * project picker. */
  organization: string
  /** Folder name when `scopeType === 'folder'`. Used by MatchesPreview to
   * enumerate the folder's children when the author picks `project: "*"`. */
  folderName?: string
  canWrite: boolean
  initialValues?: BindingDraft
  submitLabel: string
  pendingLabel: string
  isPending?: boolean
  lockName?: boolean
  onSubmit: (values: BindingMutationParams) => Promise<void>
  onCancel: () => void
}

/**
 * BindingForm renders the shared create/edit form for a TemplatePolicyBinding.
 * It enforces the same scope guard as PolicyForm: the form refuses to submit
 * when `scopeType` is not `organization` or `folder`, regardless of what the
 * URL or caller supplied.
 */
export function BindingForm({
  mode,
  scopeType,
  namespace,
  organization,
  folderName,
  canWrite,
  initialValues,
  submitLabel,
  pendingLabel,
  isPending = false,
  lockName = false,
  onSubmit,
  onCancel,
}: BindingFormProps) {
  const [draft, setDraft] = useState<BindingDraft>(
    initialValues ?? newEmptyBindingDraft(),
  )
  const [error, setError] = useState<string | null>(null)

  // HOL-917: switch from ListLinkableTemplatePolicies (ancestor-walk RPC) to
  // ListTemplatePolicies (namespace-only RPC from HOL-912 Phase 1). The org
  // namespace is passed in as the `namespace` prop so only org-scoped policies
  // appear in the picker.
  const { data: policies = [] } = useListTemplatePolicies(namespace)

  const policyItems: ComboboxItem[] = useMemo(() => {
    return policies.map((p) => {
      const scopeLabel = scopeLabelFromNamespace(p.namespace) ?? 'unknown'
      const scopeName = scopeNameFromNamespace(p.namespace)
      return {
        value: policyKey(p.namespace, p.name),
        label: `${scopeLabel} / ${scopeName} / ${p.name}`,
      }
    })
  }, [policies])

  const selectedPolicyKey = useMemo(
    () =>
      draft.policyName
        ? policyKey(draft.policyNamespace, draft.policyName)
        : '',
    [draft.policyNamespace, draft.policyName],
  )

  const slugify = (val: string) =>
    val
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, '-')
      .replace(/^-+|-+$/g, '')

  const handleDisplayNameChange = (val: string) => {
    setDraft((prev) => ({
      ...prev,
      displayName: val,
      name: mode === 'create' && !lockName ? slugify(val) : prev.name,
    }))
  }

  const handleSubmit = async () => {
    setError(null)

    // Scope guard: bindings can only be authored at folder, organization, or
    // project scope. Matches PolicyForm's guard.
    //
    // HOL-1024: 'project' is now a valid scope (ScopePicker on the new page
    // routes the create mutation to the project namespace when selected).
    if (scopeType !== 'organization' && scopeType !== 'folder' && scopeType !== 'project') {
      setError(
        'Template policy bindings can only be created at folder, organization, or project scope. Navigate to a valid scope to manage bindings.',
      )
      return
    }

    const validationError = validateBindingDraft(draft)
    if (validationError) {
      setError(validationError)
      return
    }

    try {
      await onSubmit(draftToMutationParams(draft))
    } catch (err) {
      setError(connectErrorMessage(err))
    }
  }

  const parentScope: MatchesPreviewParentScope = useMemo(() => {
    if (scopeType === 'folder' && folderName) {
      return { kind: 'folder', folderName }
    }
    return { kind: 'organization' }
  }, [scopeType, folderName])

  return (
    <div className="space-y-4">
      <div
        data-testid="binding-form-info"
        className="rounded-md border border-border p-3 text-sm text-muted-foreground"
      >
        A TemplatePolicyBinding attaches one policy to a list of project
        templates, deployments, or project namespaces. Use the wildcard{' '}
        <code>*</code> in <code>project_name</code> or <code>name</code> to
        expand a row to every match the binding's storage scope can reach — a
        folder-scoped binding can only touch resources under that folder, an
        organization-scoped binding can touch every project in the org.{' '}
        <code>kind</code> is never wildcarded: use a separate row for each kind
        so audit logs stay readable. A <code>project_namespace</code> row
        matches the namespace created for each new project under the selected
        ancestor when <code>project_name</code> is <code>*</code>.
      </div>

      <div>
        <Label htmlFor="binding-display-name">Display Name</Label>
        <Input
          id="binding-display-name"
          aria-label="Display Name"
          autoFocus
          value={draft.displayName}
          onChange={(e) => handleDisplayNameChange(e.target.value)}
          placeholder="My Binding"
          disabled={!canWrite}
        />
      </div>

      <div>
        <Label htmlFor="binding-name">Name (slug)</Label>
        <Input
          id="binding-name"
          aria-label="Name slug"
          value={draft.name}
          onChange={(e) => setDraft((prev) => ({ ...prev, name: e.target.value }))}
          placeholder="my-binding"
          disabled={!canWrite || lockName}
        />
        <p className="text-xs text-muted-foreground mt-1">
          {lockName
            ? 'Binding names are immutable after creation.'
            : 'Auto-derived from display name. Lowercase alphanumeric and hyphens only.'}
        </p>
      </div>

      <div>
        <Label htmlFor="binding-description">Description</Label>
        <Textarea
          id="binding-description"
          aria-label="Description"
          value={draft.description}
          onChange={(e) =>
            setDraft((prev) => ({ ...prev, description: e.target.value }))
          }
          placeholder="What does this binding attach and why?"
          disabled={!canWrite}
          rows={3}
        />
      </div>

      <Separator />

      <div>
        <Label htmlFor="binding-policy">Policy</Label>
        <Combobox
          items={policyItems}
          value={selectedPolicyKey}
          onValueChange={(v) => {
            if (!canWrite) return
            setDraft((prev) => applyPolicyKey(prev, v))
          }}
          placeholder="Select a template policy..."
          searchPlaceholder="Search policies..."
          emptyMessage="No template policies exist in this org yet. Create a policy first before creating a binding."
          aria-label="Template policy"
        />
        <p className="text-xs text-muted-foreground mt-1">
          Pick the TemplatePolicy this binding attaches. Policies in this
          organization namespace are offered.
        </p>
      </div>

      <Separator />

      <div className="space-y-2">
        <div className="flex items-center justify-between">
          <Label>Targets</Label>
          <p className="text-xs text-muted-foreground">
            Scope:{' '}
            {scopeType === 'folder'
              ? 'Folder'
              : scopeType === 'organization'
                ? 'Organization'
                : scopeType === 'project'
                  ? 'Project'
                  : 'Invalid'}
          </p>
        </div>
        <TargetRefEditor
          organization={organization}
          targets={draft.targetRefs}
          onChange={(targetRefs) =>
            setDraft((prev) => ({ ...prev, targetRefs }))
          }
          disabled={!canWrite}
        />
        <MatchesPreview
          organization={organization}
          parentScope={parentScope}
          targets={draft.targetRefs}
        />
      </div>

      {error && (
        <Alert variant="destructive" data-testid="binding-form-error">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}

      <div className="flex items-center gap-3 pt-2">
        <Button onClick={handleSubmit} disabled={isPending || !canWrite}>
          {isPending ? pendingLabel : submitLabel}
        </Button>
        <Button
          variant="ghost"
          type="button"
          aria-label="Cancel"
          onClick={onCancel}
        >
          Cancel
        </Button>
      </div>
    </div>
  )
}

import { useMemo } from 'react'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Combobox, type ComboboxItem } from '@/components/ui/combobox'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Trash2 } from 'lucide-react'
import { TemplatePolicyBindingTargetKind } from '@/queries/templatePolicyBindings'
import type { TargetRefDraft } from './binding-draft'
import { useListDeployments } from '@/queries/deployments'
import { useListProjects } from '@/queries/projects'
import { useListTemplates } from '@/queries/templates'
import { makeProjectScope } from '@/queries/templates'
import { TemplateScope, scopeFromNamespace } from '@/lib/scope-shim'

// Kind options exposed to the target-row select. UNSPECIFIED is intentionally
// omitted — the backend rejects it and the UI must not offer it.
const KIND_OPTIONS: Array<{
  value: TemplatePolicyBindingTargetKind
  label: string
}> = [
  {
    value: TemplatePolicyBindingTargetKind.PROJECT_TEMPLATE,
    label: 'Project Template',
  },
  { value: TemplatePolicyBindingTargetKind.DEPLOYMENT, label: 'Deployment' },
]

export type TargetRefEditorProps = {
  /** The organization these target refs live under. Required to populate the
   * project picker. */
  organization: string
  targets: TargetRefDraft[]
  onChange: (targets: TargetRefDraft[]) => void
  disabled?: boolean
}

/**
 * TargetRefEditor renders an editable list of binding target refs. Each row
 * is a kind select, a project combobox (always visible), and a name combobox
 * whose source depends on the kind: PROJECT_TEMPLATE loads project-scope
 * templates for the selected project, DEPLOYMENT loads deployments for the
 * selected project. The caller owns the targets state and passes an
 * onChange callback.
 */
export function TargetRefEditor({
  organization,
  targets,
  onChange,
  disabled = false,
}: TargetRefEditorProps) {
  const handleUpdate = (index: number, patch: Partial<TargetRefDraft>) => {
    const next = targets.map((t, i) => (i === index ? { ...t, ...patch } : t))
    onChange(next)
  }

  const handleRemove = (index: number) => {
    onChange(targets.filter((_, i) => i !== index))
  }

  const handleAdd = () => {
    onChange([
      ...targets,
      {
        kind: TemplatePolicyBindingTargetKind.PROJECT_TEMPLATE,
        projectName: '',
        name: '',
      },
    ])
  }

  return (
    <div className="space-y-4" data-testid="target-ref-editor">
      {targets.length === 0 && (
        <p className="text-sm text-muted-foreground">
          No targets yet. A binding must have at least one target.
        </p>
      )}
      {targets.map((target, index) => (
        <TargetRow
          key={index}
          index={index}
          organization={organization}
          target={target}
          onUpdate={(patch) => handleUpdate(index, patch)}
          onRemove={() => handleRemove(index)}
          disabled={disabled}
        />
      ))}

      {!disabled && (
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={handleAdd}
          aria-label="Add target"
        >
          Add Target
        </Button>
      )}
    </div>
  )
}

type TargetRowProps = {
  index: number
  organization: string
  target: TargetRefDraft
  onUpdate: (patch: Partial<TargetRefDraft>) => void
  onRemove: () => void
  disabled: boolean
}

function TargetRow({
  index,
  organization,
  target,
  onUpdate,
  onRemove,
  disabled,
}: TargetRowProps) {
  const isDeployment = target.kind === TemplatePolicyBindingTargetKind.DEPLOYMENT

  const { data: projectsResponse } = useListProjects(organization)
  const projectItems: ComboboxItem[] = useMemo(() => {
    const projects = projectsResponse?.projects ?? []
    return projects.map((p) => ({
      value: p.name,
      label: p.displayName ? `${p.displayName} (${p.name})` : p.name,
    }))
  }, [projectsResponse])

  // Name picker: project-scope templates or deployments, both scoped to the
  // selected project. Both hooks are called unconditionally — they gate on a
  // non-empty project name via their `enabled` option (useListTemplates keys
  // on scope.scopeName, useListDeployments keys on the project argument), so
  // no fetch occurs until a project is picked.
  const projectScope = makeProjectScope(target.projectName)
  const { data: projectTemplates = [] } = useListTemplates(projectScope)
  const { data: deployments = [] } = useListDeployments(target.projectName)

  // KIND_OPTIONS omits UNSPECIFIED, so kind is always DEPLOYMENT or
  // PROJECT_TEMPLATE. Branching on a single `isDeployment` flag keeps the
  // contract explicit and avoids an unreachable fallthrough.
  const nameItems: ComboboxItem[] = useMemo(() => {
    if (isDeployment) {
      return deployments.map((d) => ({
        value: d.name,
        label: d.displayName ? `${d.displayName} (${d.name})` : d.name,
      }))
    }
    return projectTemplates
      .filter((t) => scopeFromNamespace(t.namespace) === TemplateScope.PROJECT)
      .map((t) => ({
        value: t.name,
        label: t.displayName ? `${t.displayName} (${t.name})` : t.name,
      }))
  }, [isDeployment, projectTemplates, deployments])

  return (
    <div
      data-testid={`target-ref-row-${index}`}
      className="space-y-3 rounded-md border border-border p-4"
    >
      <div className="flex items-start justify-between gap-2">
        <span className="text-sm text-muted-foreground">Target {index + 1}</span>
        {!disabled && (
          <Button
            variant="ghost"
            size="sm"
            type="button"
            aria-label={`Remove target ${index + 1}`}
            onClick={onRemove}
          >
            <Trash2 className="h-4 w-4" />
          </Button>
        )}
      </div>

      <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
        <div>
          <Label htmlFor={`target-kind-${index}`}>Kind</Label>
          <Select
            value={String(target.kind)}
            onValueChange={(v) => {
              const next = Number(v) as TemplatePolicyBindingTargetKind
              // Changing the kind invalidates the selected name (PROJECT_TEMPLATE
              // and DEPLOYMENT pull from different pickers) but keeps the
              // project so the user does not lose that selection when toggling.
              onUpdate({ kind: next, name: '' })
            }}
            disabled={disabled}
          >
            <SelectTrigger
              id={`target-kind-${index}`}
              aria-label={`Target ${index + 1} kind`}
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {KIND_OPTIONS.map((opt) => (
                <SelectItem key={opt.value} value={String(opt.value)}>
                  {opt.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        <div>
          <Label htmlFor={`target-project-${index}`}>Project</Label>
          <Combobox
            items={projectItems}
            value={target.projectName}
            onValueChange={(v) => {
              if (disabled) return
              // Changing the project invalidates the previously picked name
              // (names are scoped per-project) so clear it.
              onUpdate({ projectName: v, name: '' })
            }}
            placeholder="Select a project..."
            searchPlaceholder="Search projects..."
            aria-label={`Target ${index + 1} project`}
          />
        </div>

        <div>
          <Label htmlFor={`target-name-${index}`}>Name</Label>
          <Combobox
            items={nameItems}
            value={target.name}
            onValueChange={(v) => {
              if (disabled) return
              onUpdate({ name: v })
            }}
            placeholder={
              isDeployment
                ? 'Select a deployment...'
                : 'Select a project template...'
            }
            searchPlaceholder="Search..."
            aria-label={`Target ${index + 1} name`}
          />
        </div>
      </div>
    </div>
  )
}


import { useMemo } from 'react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Combobox, type ComboboxItem } from '@/components/ui/combobox'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Asterisk, Trash2 } from 'lucide-react'
import { TemplatePolicyBindingTargetKind } from '@/queries/templatePolicyBindings'
import {
  WILDCARD,
  findDuplicateTargetIndex,
  type TargetRefDraft,
} from './binding-draft'
import { useListDeployments } from '@/queries/deployments'
import { useListProjects } from '@/queries/projects'
import { useListTemplates } from '@/queries/templates'
import { namespaceForProject, scopeLabelFromNamespace } from '@/lib/scope-labels'

// Kind options exposed to the target-row select. UNSPECIFIED is intentionally
// omitted — the backend rejects it and the UI must not offer it. `kind` is
// also never wildcarded (HOL-767 audit-readability rule); cross-kind fan-out
// requires separate target rows.
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

// Synthetic combobox items for the wildcard sentinel. Using the literal
// string '*' as the value is load-bearing — it round-trips byte-identically
// to `policyresolver.WildcardAny` on the backend (HOL-769 / HOL-770 / HOL-772).
const WILDCARD_PROJECT_ITEM: ComboboxItem = {
  value: WILDCARD,
  label: 'All projects (*)',
}
function wildcardNameItem(isDeployment: boolean): ComboboxItem {
  return {
    value: WILDCARD,
    label: isDeployment ? 'All deployments (*)' : 'All project templates (*)',
  }
}

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
 *
 * Both the project and name comboboxes carry a synthetic `*` (wildcard)
 * option at the top — picking it expands the binding to every match the
 * storage scope can reach (HOL-767). Two rows that share the same
 * `(kind, projectName, name)` triple are flagged inline as duplicates so
 * the author sees the conflict before submit; the backend rejects the same
 * shape with `target_refs[i]: duplicate of target_refs[j] (...)`.
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

  // Compute the index of the first row that duplicates an earlier row on the
  // (kind, projectName, name) triple. The wildcard literal "*" participates
  // as a normal string value — `{kind, "*", "*"}` matches itself, mirroring
  // backend dedup (HOL-772). Memoized so we recompute only when the target
  // list changes.
  const duplicateIndex = useMemo(
    () => findDuplicateTargetIndex(targets),
    [targets],
  )

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
          isDuplicate={index === duplicateIndex}
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
  isDuplicate: boolean
  onUpdate: (patch: Partial<TargetRefDraft>) => void
  onRemove: () => void
  disabled: boolean
}

function TargetRow({
  index,
  organization,
  target,
  isDuplicate,
  onUpdate,
  onRemove,
  disabled,
}: TargetRowProps) {
  const isDeployment = target.kind === TemplatePolicyBindingTargetKind.DEPLOYMENT
  const projectIsWildcard = target.projectName === WILDCARD
  const isLiteralProject = !!target.projectName && !projectIsWildcard

  const { data: projectsResponse } = useListProjects(organization)
  const projectItems: ComboboxItem[] = useMemo(() => {
    const projects = projectsResponse?.projects ?? []
    const literal = projects.map((p) => ({
      value: p.name,
      label: p.displayName ? `${p.displayName} (${p.name})` : p.name,
    }))
    // Wildcard sentinel pinned to the top so the author always sees the
    // "All projects" option without scrolling past the literal list.
    return [WILDCARD_PROJECT_ITEM, ...literal]
  }, [projectsResponse])

  // Name picker: project-scope templates or deployments, both scoped to the
  // selected literal project. When the project is the wildcard "*" the
  // hooks are short-circuited via the `enabled` flag — `namespaceForProject`
  // would otherwise build a malformed namespace string for the literal "*".
  // The user can still pick the wildcard sentinel or type a literal name
  // (the combobox accepts free-text via the search input even when the
  // backed list is empty).
  const projectNamespace = isLiteralProject
    ? namespaceForProject(target.projectName)
    : ''
  const { data: projectTemplates = [] } = useListTemplates(projectNamespace)
  const { data: deployments = [] } = useListDeployments(
    isLiteralProject ? target.projectName : '',
  )

  // KIND_OPTIONS omits UNSPECIFIED, so kind is always DEPLOYMENT or
  // PROJECT_TEMPLATE. Branching on a single `isDeployment` flag keeps the
  // contract explicit and avoids an unreachable fallthrough.
  const nameItems: ComboboxItem[] = useMemo(() => {
    const wildcard = wildcardNameItem(isDeployment)
    if (!isLiteralProject) {
      // No backing list when project is unset or wildcard — only the
      // wildcard sentinel is offered.
      return [wildcard]
    }
    const literal = isDeployment
      ? deployments.map((d) => ({
          value: d.name,
          label: d.displayName ? `${d.displayName} (${d.name})` : d.name,
        }))
      : projectTemplates
          .filter((t) => scopeLabelFromNamespace(t.namespace) === 'project')
          .map((t) => ({
            value: t.name,
            label: t.displayName ? `${t.displayName} (${t.name})` : t.name,
          }))
    return [wildcard, ...literal]
  }, [isDeployment, isLiteralProject, projectTemplates, deployments])

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
          {isLiteralProject ? (
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
          ) : (
            // When project is "*" or unset, there is no per-project list to
            // pick from. Authors still need to be able to type a literal name
            // (e.g. {project:"*", name:"ingress"} for a scope-wide template)
            // OR snap to the wildcard. Our `Combobox` is strict single-select
            // and does not accept arbitrary text, so we render an Input here
            // with a one-click "*" quick-pick — see codex review on PR #1084.
            <div
              className="flex items-center gap-2"
              data-testid={`target-ref-row-${index}-name-literal`}
            >
              <Input
                id={`target-name-${index}`}
                type="text"
                value={target.name}
                onChange={(e) => {
                  if (disabled) return
                  onUpdate({ name: e.target.value })
                }}
                placeholder={
                  isDeployment
                    ? 'Deployment name or *'
                    : 'Template name or *'
                }
                aria-label={`Target ${index + 1} name`}
                disabled={disabled}
              />
              <Button
                type="button"
                variant="outline"
                size="sm"
                aria-label={`Target ${index + 1} set name to wildcard`}
                data-testid={`target-ref-row-${index}-name-wildcard-btn`}
                onClick={() => {
                  if (disabled) return
                  onUpdate({ name: WILDCARD })
                }}
                disabled={disabled}
              >
                <Asterisk className="h-4 w-4" />
              </Button>
            </div>
          )}
        </div>
      </div>

      {isDuplicate && (
        <p
          role="alert"
          data-testid={`target-ref-row-${index}-duplicate-error`}
          className="text-sm text-destructive"
        >
          Duplicate of an earlier target ({'kind, project_name, name'} triple
          must be unique).
        </p>
      )}
    </div>
  )
}

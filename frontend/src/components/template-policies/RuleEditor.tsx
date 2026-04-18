import { useMemo } from 'react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Combobox, type ComboboxItem } from '@/components/ui/combobox'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { Trash2, Info, AlertTriangle } from 'lucide-react'
import { TemplatePolicyKind } from '@/queries/templatePolicies'
import { TemplateScope, linkableKey } from '@/queries/templates'
import type { LinkableTemplate } from '@/queries/templates'
import { newEmptyRule, type RuleDraft } from '@/components/template-policies/rule-draft'
import {
  REQUIRE_RULE_DESCRIPTION,
  EXCLUDE_RULE_DESCRIPTION,
} from '@/components/platform-template-copy'

export type RuleEditorProps = {
  rules: RuleDraft[]
  onChange: (rules: RuleDraft[]) => void
  linkableTemplates: LinkableTemplate[]
  disabled?: boolean
}

// Intentionally only the short enum label — the long REQUIRE/EXCLUDE copy is
// sourced from `platform-template-copy` and surfaced once via the tooltip next
// to the Kind `<Label>` so it does not overflow the Radix Select popover.
const KIND_OPTIONS: Array<{ value: TemplatePolicyKind; label: string }> = [
  { value: TemplatePolicyKind.REQUIRE, label: 'REQUIRE' },
  { value: TemplatePolicyKind.EXCLUDE, label: 'EXCLUDE' },
]

/**
 * RuleEditor renders an editable list of policy rules. It is used by both the
 * create route (new policy) and the detail route (existing policy). The
 * caller owns the rules state and passes an onChange callback.
 */
export function RuleEditor({
  rules,
  onChange,
  linkableTemplates,
  disabled = false,
}: RuleEditorProps) {
  const templateItems: ComboboxItem[] = useMemo(() => {
    return linkableTemplates.map((t) => {
      const scope = t.scopeRef?.scope ?? TemplateScope.UNSPECIFIED
      const scopeName = t.scopeRef?.scopeName ?? ''
      const scopeLabel =
        scope === TemplateScope.ORGANIZATION
          ? 'org'
          : scope === TemplateScope.FOLDER
            ? 'folder'
            : 'project'
      return {
        value: linkableKey(scope, scopeName, t.name),
        label: `${scopeLabel} / ${scopeName} / ${t.name}`,
      }
    })
  }, [linkableTemplates])

  const handleUpdate = (index: number, patch: Partial<RuleDraft>) => {
    const next = rules.map((rule, i) => (i === index ? { ...rule, ...patch } : rule))
    onChange(next)
  }

  const handleRemove = (index: number) => {
    onChange(rules.filter((_, i) => i !== index))
  }

  const handleAdd = () => {
    // HOL-598: the draft no longer carries glob pattern fields; attachment is
    // expressed exclusively via TemplatePolicyBinding.
    onChange([...rules, newEmptyRule()])
  }

  return (
    <div className="space-y-4">
      {rules.length === 0 && (
        <p className="text-sm text-muted-foreground">
          No rules yet. A policy must have at least one rule.
        </p>
      )}
      {rules.map((rule, index) => {
        const isExclude = rule.kind === TemplatePolicyKind.EXCLUDE
        return (
          <div
            key={index}
            data-testid={`rule-editor-row-${index}`}
            className="space-y-3 rounded-md border border-border p-4"
          >
            <div className="flex items-start justify-between gap-2">
              <div className="flex items-center gap-2">
                <Badge
                  variant="outline"
                  className={
                    rule.kind === TemplatePolicyKind.REQUIRE
                      ? 'text-xs border-green-500/30 text-green-500'
                      : 'text-xs border-amber-500/30 text-amber-500'
                  }
                >
                  {rule.kind === TemplatePolicyKind.REQUIRE ? 'REQUIRE' : 'EXCLUDE'}
                </Badge>
                <span className="text-sm text-muted-foreground">Rule {index + 1}</span>
              </div>
              {!disabled && (
                <Button
                  variant="ghost"
                  size="sm"
                  type="button"
                  aria-label={`Remove rule ${index + 1}`}
                  onClick={() => handleRemove(index)}
                >
                  <Trash2 className="h-4 w-4" />
                </Button>
              )}
            </div>

            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
              <div>
                <Label htmlFor={`rule-kind-${index}`} className="flex items-center gap-1">
                  Kind
                  <TooltipProvider>
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <button
                          type="button"
                          aria-label="Explain REQUIRE and EXCLUDE"
                          className="inline-flex"
                        >
                          <Info className="h-3.5 w-3.5 text-muted-foreground" />
                        </button>
                      </TooltipTrigger>
                      <TooltipContent className="max-w-sm space-y-2">
                        <p>{REQUIRE_RULE_DESCRIPTION}</p>
                        <p>{EXCLUDE_RULE_DESCRIPTION}</p>
                      </TooltipContent>
                    </Tooltip>
                  </TooltipProvider>
                </Label>
                <Select
                  value={String(rule.kind)}
                  onValueChange={(v) => handleUpdate(index, { kind: Number(v) as TemplatePolicyKind })}
                  disabled={disabled}
                >
                  <SelectTrigger id={`rule-kind-${index}`} aria-label={`Rule ${index + 1} kind`}>
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
                <Label htmlFor={`rule-template-${index}`}>Template</Label>
                <Combobox
                  items={templateItems}
                  value={rule.templateKey}
                  onValueChange={(v) => {
                    if (disabled) return
                    handleUpdate(index, { templateKey: v })
                  }}
                  placeholder="Select a template..."
                  searchPlaceholder="Search templates..."
                  aria-label={`Rule ${index + 1} template`}
                />
              </div>

              <div>
                <Label htmlFor={`rule-version-${index}`}>Version constraint (optional)</Label>
                <Input
                  id={`rule-version-${index}`}
                  aria-label={`Rule ${index + 1} version constraint`}
                  placeholder='e.g. ">=2.0.0 <3.0.0"'
                  value={rule.versionConstraint}
                  onChange={(e) => handleUpdate(index, { versionConstraint: e.target.value })}
                  disabled={disabled}
                />
                <p className="text-xs text-muted-foreground mt-1">
                  Semver range. Leave empty to always use the latest release.
                </p>
              </div>
              {/*
               * HOL-598: the former "Project pattern" and "Deployment pattern"
               * text inputs lived here. They authored opaque glob patterns on
               * the rule's Target. Attachment is now expressed exclusively via
               * TemplatePolicyBinding (see the Bindings section on the Policy
               * detail page), so the inputs were removed. Newly created and
               * edited rules submit with `target` unset.
               */}
            </div>

            {isExclude && (
              <Alert className="border-amber-500/30 text-amber-500">
                <AlertTriangle className="h-4 w-4" />
                <AlertDescription>
                  EXCLUDE rules may be rejected by the backend when they target a template that is
                  already explicitly linked to a project. The exact conflict is reported when you
                  submit the policy.
                </AlertDescription>
              </Alert>
            )}
          </div>
        )
      })}

      {!disabled && (
        <Button type="button" variant="outline" size="sm" onClick={handleAdd} aria-label="Add rule">
          Add Rule
        </Button>
      )}
    </div>
  )
}

import { create } from '@bufbuild/protobuf'
import { TemplatePolicyKind } from '@/queries/templatePolicies'
import type { TemplatePolicyRule } from '@/queries/templatePolicies'
import {
  TemplateScope,
  linkableKey,
  parseLinkableKey,
} from '@/queries/templates'
import {
  TemplatePolicyRuleSchema,
  TemplatePolicyTargetSchema,
} from '@/gen/holos/console/v1/template_policies_pb.js'
import { LinkedTemplateRefSchema } from '@/gen/holos/console/v1/templates_pb.js'

/**
 * Draft shape used by the form while the user is authoring rules. This is
 * intentionally flatter than the proto TemplatePolicyRule message so inputs
 * can be bound to strings, and only gets converted into proto messages when
 * the form is submitted.
 */
export type RuleDraft = {
  kind: TemplatePolicyKind
  templateKey: string // composite key built by linkableKey(...)
  versionConstraint: string
  projectPattern: string
  deploymentPattern: string
}

export function newEmptyRule(): RuleDraft {
  return {
    kind: TemplatePolicyKind.REQUIRE,
    templateKey: '',
    versionConstraint: '',
    projectPattern: '*',
    deploymentPattern: '*',
  }
}

/** Convert a draft into a proto rule message suitable for submission. */
export function ruleDraftToProto(draft: RuleDraft): TemplatePolicyRule {
  const { scope, scopeName, name } = parseLinkableKey(draft.templateKey)
  return create(TemplatePolicyRuleSchema, {
    kind: draft.kind,
    template: create(LinkedTemplateRefSchema, {
      scope: scope as TemplateScope,
      scopeName,
      name,
      versionConstraint: draft.versionConstraint,
    }),
    target: create(TemplatePolicyTargetSchema, {
      projectPattern: draft.projectPattern,
      deploymentPattern: draft.deploymentPattern,
    }),
  })
}

/** Convert a proto rule back into a draft for the editor. */
export function ruleProtoToDraft(rule: TemplatePolicyRule): RuleDraft {
  const tmpl = rule.template
  return {
    kind: rule.kind,
    templateKey: tmpl
      ? linkableKey(tmpl.scope, tmpl.scopeName, tmpl.name)
      : '',
    versionConstraint: tmpl?.versionConstraint ?? '',
    projectPattern: rule.target?.projectPattern ?? '',
    deploymentPattern: rule.target?.deploymentPattern ?? '',
  }
}

/**
 * validateRuleDraft returns a human-readable error string when the draft is
 * not submittable, or null when it is valid for the client. The backend
 * performs authoritative validation (e.g. glob compilation, EXCLUDE-vs-linked
 * checks).
 */
export function validateRuleDraft(draft: RuleDraft): string | null {
  if (!draft.templateKey) {
    return 'Template selection is required.'
  }
  if (!draft.projectPattern) {
    return 'Project pattern is required (use "*" to match every project).'
  }
  // filepath.Match uses "\" as an escape character. Detect unpaired escapes
  // early so the user sees feedback before the backend round-trip.
  const patterns = [draft.projectPattern, draft.deploymentPattern]
  for (const p of patterns) {
    if (!p) continue
    // Reject bare backslash at end of string or unmatched brackets as common
    // mistakes. Backend remains authoritative.
    if (/\\$/.test(p)) return 'Invalid glob pattern: trailing backslash.'
    if (/\[[^\]]*$/.test(p)) return 'Invalid glob pattern: unmatched "[".'
  }
  return null
}

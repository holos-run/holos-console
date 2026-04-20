import { create } from '@bufbuild/protobuf'
import { TemplatePolicyKind } from '@/queries/templatePolicies'
import type { TemplatePolicyRule } from '@/queries/templatePolicies'
import {
  linkableKey,
  parseLinkableKey,
} from '@/queries/templates'
import { TemplatePolicyRuleSchema } from '@/gen/holos/console/v1/template_policies_pb.js'
import { LinkedTemplateRefSchema } from '@/gen/holos/console/v1/policy_state_pb.js'

/**
 * Draft shape used by the form while the user is authoring rules. This is
 * intentionally flatter than the proto TemplatePolicyRule message so inputs
 * can be bound to strings, and only gets converted into proto messages when
 * the form is submitted.
 *
 * HOL-598: the `projectPattern` and `deploymentPattern` glob fields were
 * removed from the UI. Attachment is now expressed exclusively via
 * TemplatePolicyBinding. The draft therefore no longer carries glob fields,
 * and `ruleDraftToProto` emits a rule with `target` unset.
 *
 * HOL-623: the template identity on the wire is now (namespace, name) — the
 * composite templateKey is `<namespace>/<name>`, built via `linkableKey`.
 */
export type RuleDraft = {
  kind: TemplatePolicyKind
  templateKey: string // composite key built by linkableKey(...)
  versionConstraint: string
}

export function newEmptyRule(): RuleDraft {
  return {
    kind: TemplatePolicyKind.REQUIRE,
    templateKey: '',
    versionConstraint: '',
  }
}

/**
 * Convert a draft into a proto rule message suitable for submission. The
 * resulting rule has `target` unset — HOL-598 routes all attachment through
 * TemplatePolicyBinding.
 */
export function ruleDraftToProto(draft: RuleDraft): TemplatePolicyRule {
  const { namespace, name } = parseLinkableKey(draft.templateKey)
  return create(TemplatePolicyRuleSchema, {
    kind: draft.kind,
    template: create(LinkedTemplateRefSchema, {
      namespace,
      name,
      versionConstraint: draft.versionConstraint,
    }),
    // target is intentionally omitted. HOL-598 removed UI authoring of the
    // glob Target fields; bindings now carry attachment exclusively.
  })
}

/**
 * Convert a proto rule back into a draft for the editor. Any legacy `target`
 * field present on the server-side message is discarded — the editor cannot
 * author globs, so the draft does not surface them.
 */
export function ruleProtoToDraft(rule: TemplatePolicyRule): RuleDraft {
  const tmpl = rule.template
  return {
    kind: rule.kind,
    templateKey: tmpl ? linkableKey(tmpl.namespace, tmpl.name) : '',
    versionConstraint: tmpl?.versionConstraint ?? '',
  }
}

/**
 * validateRuleDraft returns a human-readable error string when the draft is
 * not submittable, or null when it is valid for the client. The backend
 * performs authoritative validation (e.g. EXCLUDE-vs-linked checks). With
 * HOL-598 the only client-side requirement is a selected template — glob
 * patterns no longer exist in the draft.
 */
export function validateRuleDraft(draft: RuleDraft): string | null {
  if (!draft.templateKey) {
    return 'Template selection is required.'
  }
  return null
}

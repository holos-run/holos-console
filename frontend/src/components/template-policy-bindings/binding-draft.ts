import { create } from '@bufbuild/protobuf'
import {
  TemplatePolicyBindingTargetKind,
  type TemplatePolicyBindingTargetRef,
  type LinkedTemplatePolicyRef,
} from '@/queries/templatePolicyBindings'
import {
  TemplatePolicyBindingTargetRefSchema,
  LinkedTemplatePolicyRefSchema,
} from '@/gen/holos/console/v1/template_policy_bindings_pb.js'

/**
 * Draft shape for a single target ref while the user is authoring a binding.
 * Kept flatter than the proto message so inputs can be bound to strings and
 * converted at submit time, mirroring rule-draft.ts.
 */
export type TargetRefDraft = {
  kind: TemplatePolicyBindingTargetKind
  projectName: string
  name: string
}

/**
 * Draft shape for a binding while the user is authoring it. `policyNamespace`
 * together with `policyName` identifies the TemplatePolicy referenced by the
 * binding; the picker surfaces ancestor- and same-namespace policies so the
 * form submits both halves of the LinkedTemplatePolicyRef together.
 *
 * HOL-623: policies are keyed by (namespace, name); the draft tracks
 * `policyNamespace` directly rather than the legacy (scope, scopeName) pair.
 */
export type BindingDraft = {
  name: string
  displayName: string
  description: string
  policyNamespace: string
  policyName: string
  targetRefs: TargetRefDraft[]
}

export function newEmptyTargetRef(): TargetRefDraft {
  return {
    kind: TemplatePolicyBindingTargetKind.PROJECT_TEMPLATE,
    projectName: '',
    name: '',
  }
}

export function newEmptyBindingDraft(): BindingDraft {
  return {
    name: '',
    displayName: '',
    description: '',
    policyNamespace: '',
    policyName: '',
    targetRefs: [newEmptyTargetRef()],
  }
}

/** Convert a target-ref draft into a proto TemplatePolicyBindingTargetRef. */
export function targetRefDraftToProto(
  draft: TargetRefDraft,
): TemplatePolicyBindingTargetRef {
  return create(TemplatePolicyBindingTargetRefSchema, {
    kind: draft.kind,
    name: draft.name,
    projectName: draft.projectName,
  })
}

/** Convert a proto TemplatePolicyBindingTargetRef back into a draft. */
export function targetRefProtoToDraft(
  ref: TemplatePolicyBindingTargetRef,
): TargetRefDraft {
  return {
    kind: ref.kind,
    projectName: ref.projectName ?? '',
    name: ref.name ?? '',
  }
}

/** Build a LinkedTemplatePolicyRef proto from a binding draft. */
export function draftToPolicyRef(draft: BindingDraft): LinkedTemplatePolicyRef {
  return create(LinkedTemplatePolicyRefSchema, {
    namespace: draft.policyNamespace,
    name: draft.policyName,
  })
}

/**
 * Populate a binding draft from an existing proto binding. Used by the
 * detail/edit route to seed the form with saved values.
 */
export function bindingProtoToDraft(
  binding: {
    name?: string
    displayName?: string
    description?: string
    policyRef?: LinkedTemplatePolicyRef
    targetRefs?: TemplatePolicyBindingTargetRef[]
  },
): BindingDraft {
  return {
    name: binding.name ?? '',
    displayName: binding.displayName ?? '',
    description: binding.description ?? '',
    policyNamespace: binding.policyRef?.namespace ?? '',
    policyName: binding.policyRef?.name ?? '',
    targetRefs: (binding.targetRefs ?? []).map(targetRefProtoToDraft),
  }
}

/**
 * validateBindingDraft returns a human-readable error string when the draft
 * is not submittable, or null when it is valid for the client. The backend
 * performs authoritative validation (duplicates, cross-scope reachability).
 */
export function validateBindingDraft(draft: BindingDraft): string | null {
  if (!draft.name.trim()) {
    return 'Binding name is required.'
  }
  if (!draft.policyName || !draft.policyNamespace) {
    return 'Policy selection is required.'
  }
  if (draft.targetRefs.length === 0) {
    return 'A binding must have at least one target.'
  }
  for (let i = 0; i < draft.targetRefs.length; i++) {
    const target = draft.targetRefs[i]
    const position = i + 1
    if (target.kind === TemplatePolicyBindingTargetKind.UNSPECIFIED) {
      return `Target ${position}: kind is required.`
    }
    if (!target.projectName) {
      return `Target ${position}: project is required.`
    }
    if (!target.name) {
      return `Target ${position}: name is required.`
    }
  }
  // Reject duplicates (identical (kind, projectName, name) triples). The
  // backend is authoritative but the UI should refuse to submit an obviously
  // invalid draft.
  const seen = new Set<string>()
  for (let i = 0; i < draft.targetRefs.length; i++) {
    const t = draft.targetRefs[i]
    const key = `${t.kind}/${t.projectName}/${t.name}`
    if (seen.has(key)) {
      return `Target ${i + 1}: duplicate of another target in this binding.`
    }
    seen.add(key)
  }
  return null
}

/**
 * Composite key used by the policy picker. A TemplatePolicy is uniquely
 * identified by (namespace, name). Serialize into a single value so the
 * Combobox can present it as a single option. The name segment may contain
 * characters other than `/`, so we split on the first slash only.
 */
export function policyKey(namespace: string | undefined, name: string): string {
  return `${namespace ?? ''}/${name}`
}

/** Inverse of policyKey — parse a composite key back into its parts. */
export function parsePolicyKey(key: string): {
  namespace: string
  name: string
} {
  const slash = key.indexOf('/')
  if (slash < 0) return { namespace: '', name: key }
  return {
    namespace: key.slice(0, slash),
    name: key.slice(slash + 1),
  }
}

/**
 * Fill in policyNamespace/policyName on a draft from a composite
 * policy key (used by the PolicyForm's Combobox handler).
 */
export function applyPolicyKey(
  draft: BindingDraft,
  key: string,
): BindingDraft {
  const { namespace, name } = parsePolicyKey(key)
  return {
    ...draft,
    policyNamespace: namespace,
    policyName: name,
  }
}

/**
 * BindingMutationParams is the payload shape consumed by
 * useCreateTemplatePolicyBinding and useUpdateTemplatePolicyBinding. Exported
 * so BindingForm's onSubmit prop and consumers can reference the type
 * directly rather than recovering it via ReturnType<typeof>.
 */
export type BindingMutationParams = {
  name: string
  displayName: string
  description: string
  policyRef: LinkedTemplatePolicyRef
  targetRefs: TemplatePolicyBindingTargetRef[]
}

/**
 * Helper for the BindingForm submit step: convert a validated draft into the
 * set of values expected by the create/update mutation hook.
 */
export function draftToMutationParams(draft: BindingDraft): BindingMutationParams {
  return {
    name: draft.name.trim(),
    displayName: draft.displayName.trim(),
    description: draft.description.trim(),
    policyRef: draftToPolicyRef(draft),
    targetRefs: draft.targetRefs.map(targetRefDraftToProto),
  }
}

import { describe, it, expect } from 'vitest'
import {
  applyPolicyKey,
  bindingProtoToDraft,
  draftToMutationParams,
  draftToPolicyRef,
  newEmptyBindingDraft,
  parsePolicyKey,
  policyKey,
  targetRefDraftToProto,
  targetRefProtoToDraft,
  validateBindingDraft,
} from './binding-draft'
import { TemplatePolicyBindingTargetKind } from '@/queries/templatePolicyBindings'
import { TemplateScope } from '@/queries/templates'
import { namespaceFor } from '@/lib/scope-shim'

describe('policyKey / parsePolicyKey', () => {
  it('round-trips a composite key', () => {
    const key = policyKey(TemplateScope.ORGANIZATION, 'test-org', 'policy-a')
    expect(key).toBe('1/test-org/policy-a')
    const parsed = parsePolicyKey(key)
    expect(parsed).toEqual({
      scope: TemplateScope.ORGANIZATION,
      scopeName: 'test-org',
      name: 'policy-a',
    })
  })

  it('handles a name containing a slash', () => {
    // parsePolicyKey joins the remainder after the first two parts so names
    // with slashes survive the round trip.
    const key = policyKey(TemplateScope.FOLDER, 'team', 'has/slash')
    const parsed = parsePolicyKey(key)
    expect(parsed.name).toBe('has/slash')
    expect(parsed.scope).toBe(TemplateScope.FOLDER)
    expect(parsed.scopeName).toBe('team')
  })

  it('parses an empty key into UNSPECIFIED zero values', () => {
    const parsed = parsePolicyKey('')
    expect(parsed.scope).toBe(TemplateScope.UNSPECIFIED)
    expect(parsed.scopeName).toBe('')
    expect(parsed.name).toBe('')
  })
})

describe('validateBindingDraft', () => {
  it('rejects a missing name', () => {
    const draft = { ...newEmptyBindingDraft(), name: '  ' }
    expect(validateBindingDraft(draft)).toMatch(/binding name is required/i)
  })

  it('rejects a missing policy', () => {
    const draft = { ...newEmptyBindingDraft(), name: 'bind-a' }
    expect(validateBindingDraft(draft)).toMatch(/policy selection is required/i)
  })

  it('rejects an empty target list', () => {
    const draft = {
      ...newEmptyBindingDraft(),
      name: 'bind-a',
      policyScope: TemplateScope.ORGANIZATION,
      policyScopeName: 'test-org',
      policyName: 'policy-a',
      targetRefs: [],
    }
    expect(validateBindingDraft(draft)).toMatch(/must have at least one target/i)
  })

  it('rejects UNSPECIFIED kind', () => {
    const draft = {
      ...newEmptyBindingDraft(),
      name: 'bind-a',
      policyScope: TemplateScope.ORGANIZATION,
      policyScopeName: 'test-org',
      policyName: 'policy-a',
      targetRefs: [
        {
          kind: TemplatePolicyBindingTargetKind.UNSPECIFIED,
          projectName: 'proj-a',
          name: 'ingress',
        },
      ],
    }
    expect(validateBindingDraft(draft)).toMatch(/kind is required/i)
  })

  it('rejects a missing project on a target', () => {
    const draft = {
      ...newEmptyBindingDraft(),
      name: 'bind-a',
      policyScope: TemplateScope.ORGANIZATION,
      policyScopeName: 'test-org',
      policyName: 'policy-a',
      targetRefs: [
        {
          kind: TemplatePolicyBindingTargetKind.PROJECT_TEMPLATE,
          projectName: '',
          name: 'ingress',
        },
      ],
    }
    expect(validateBindingDraft(draft)).toMatch(/project is required/i)
  })

  it('rejects duplicate (kind, projectName, name) triples', () => {
    const draft = {
      ...newEmptyBindingDraft(),
      name: 'bind-a',
      policyScope: TemplateScope.ORGANIZATION,
      policyScopeName: 'test-org',
      policyName: 'policy-a',
      targetRefs: [
        {
          kind: TemplatePolicyBindingTargetKind.PROJECT_TEMPLATE,
          projectName: 'proj-a',
          name: 'ingress',
        },
        {
          kind: TemplatePolicyBindingTargetKind.PROJECT_TEMPLATE,
          projectName: 'proj-a',
          name: 'ingress',
        },
      ],
    }
    expect(validateBindingDraft(draft)).toMatch(/duplicate/i)
  })

  it('accepts two entries that differ only in kind (per proto spec)', () => {
    // The proto doc permits a PROJECT_TEMPLATE and a DEPLOYMENT with the same
    // (project_name, name) pair because they name distinct resources.
    const draft = {
      ...newEmptyBindingDraft(),
      name: 'bind-a',
      policyScope: TemplateScope.ORGANIZATION,
      policyScopeName: 'test-org',
      policyName: 'policy-a',
      targetRefs: [
        {
          kind: TemplatePolicyBindingTargetKind.PROJECT_TEMPLATE,
          projectName: 'proj-a',
          name: 'shared',
        },
        {
          kind: TemplatePolicyBindingTargetKind.DEPLOYMENT,
          projectName: 'proj-a',
          name: 'shared',
        },
      ],
    }
    expect(validateBindingDraft(draft)).toBeNull()
  })
})

describe('targetRefDraft round-trip', () => {
  it('targetRefDraftToProto and targetRefProtoToDraft are inverses', () => {
    const draft = {
      kind: TemplatePolicyBindingTargetKind.DEPLOYMENT,
      projectName: 'proj-a',
      name: 'web',
    }
    const proto = targetRefDraftToProto(draft)
    expect(proto.kind).toBe(TemplatePolicyBindingTargetKind.DEPLOYMENT)
    expect(proto.projectName).toBe('proj-a')
    expect(proto.name).toBe('web')
    const back = targetRefProtoToDraft(proto)
    expect(back).toEqual(draft)
  })
})

describe('bindingProtoToDraft', () => {
  it('populates every field from a proto binding', () => {
    const draft = bindingProtoToDraft({
      name: 'bind-a',
      displayName: 'Bind A',
      description: 'desc',
      policyRef: {
        namespace: namespaceFor(TemplateScope.FOLDER, 'team'),
        name: 'policy-a',
      } as never,
      targetRefs: [
        {
          kind: TemplatePolicyBindingTargetKind.PROJECT_TEMPLATE,
          projectName: 'proj-a',
          name: 'ingress',
        } as never,
      ],
    })
    expect(draft).toMatchObject({
      name: 'bind-a',
      displayName: 'Bind A',
      description: 'desc',
      policyScope: TemplateScope.FOLDER,
      policyScopeName: 'team',
      policyName: 'policy-a',
    })
    expect(draft.targetRefs).toHaveLength(1)
    expect(draft.targetRefs[0]).toEqual({
      kind: TemplatePolicyBindingTargetKind.PROJECT_TEMPLATE,
      projectName: 'proj-a',
      name: 'ingress',
    })
  })

  it('defaults missing fields to empty / UNSPECIFIED', () => {
    const draft = bindingProtoToDraft({})
    expect(draft.name).toBe('')
    expect(draft.policyScope).toBe(TemplateScope.UNSPECIFIED)
    expect(draft.policyScopeName).toBe('')
    expect(draft.policyName).toBe('')
    expect(draft.targetRefs).toEqual([])
  })
})

describe('applyPolicyKey', () => {
  it('splits a composite policy key into the draft fields', () => {
    const draft = newEmptyBindingDraft()
    const next = applyPolicyKey(draft, policyKey(TemplateScope.ORGANIZATION, 'test-org', 'policy-a'))
    expect(next.policyScope).toBe(TemplateScope.ORGANIZATION)
    expect(next.policyScopeName).toBe('test-org')
    expect(next.policyName).toBe('policy-a')
    // Other fields remain untouched
    expect(next.name).toBe(draft.name)
    expect(next.targetRefs).toEqual(draft.targetRefs)
  })
})

describe('draftToPolicyRef / draftToMutationParams', () => {
  it('builds a LinkedTemplatePolicyRef from draft fields', () => {
    const draft = {
      ...newEmptyBindingDraft(),
      policyScope: TemplateScope.FOLDER,
      policyScopeName: 'team',
      policyName: 'policy-a',
    }
    const ref = draftToPolicyRef(draft)
    expect(ref.name).toBe('policy-a')
    expect(ref.namespace).toBe(namespaceFor(TemplateScope.FOLDER, 'team'))
  })

  it('draftToMutationParams trims whitespace on user-editable fields', () => {
    const draft = {
      ...newEmptyBindingDraft(),
      name: '  bind-a  ',
      displayName: '  Bind A  ',
      description: '  desc  ',
      policyScope: TemplateScope.ORGANIZATION,
      policyScopeName: 'test-org',
      policyName: 'policy-a',
      targetRefs: [
        {
          kind: TemplatePolicyBindingTargetKind.PROJECT_TEMPLATE,
          projectName: 'proj-a',
          name: 'ingress',
        },
      ],
    }
    const params = draftToMutationParams(draft)
    expect(params.name).toBe('bind-a')
    expect(params.displayName).toBe('Bind A')
    expect(params.description).toBe('desc')
    expect(params.policyRef.name).toBe('policy-a')
    expect(params.targetRefs).toHaveLength(1)
    expect(params.targetRefs[0].kind).toBe(
      TemplatePolicyBindingTargetKind.PROJECT_TEMPLATE,
    )
  })
})

import { describe, it, expect } from 'vitest'
import {
  newEmptyRule,
  ruleDraftToProto,
  ruleProtoToDraft,
  validateRuleDraft,
  type RuleDraft,
} from './rule-draft'
import { TemplatePolicyKind } from '@/queries/templatePolicies'
import { linkableKey } from '@/queries/templates'
import { TemplateScope, namespaceFor } from '@/lib/scope-shim'

// HOL-598: The rule draft is no longer a vehicle for glob Target authoring.
// Attachment is expressed exclusively via TemplatePolicyBinding. These tests
// pin the new contract: `newEmptyRule()` does not carry glob fields,
// `ruleDraftToProto` emits a rule with `target` unset, `ruleProtoToDraft`
// discards any legacy populated Target, and `validateRuleDraft` no longer
// gates on glob patterns.
describe('rule-draft (HOL-598)', () => {
  describe('newEmptyRule', () => {
    it('returns a draft with no glob pattern fields', () => {
      const draft = newEmptyRule()
      expect(draft).toEqual({
        kind: TemplatePolicyKind.REQUIRE,
        templateKey: '',
        versionConstraint: '',
      })
      const extra = draft as Partial<RuleDraft> & {
        projectPattern?: string
        deploymentPattern?: string
      }
      expect(extra.projectPattern).toBeUndefined()
      expect(extra.deploymentPattern).toBeUndefined()
    })
  })

  describe('ruleDraftToProto', () => {
    it('emits a proto rule with target unset', () => {
      const draft: RuleDraft = {
        kind: TemplatePolicyKind.REQUIRE,
        templateKey: linkableKey(TemplateScope.ORGANIZATION, 'acme', 'httproute'),
        versionConstraint: '^1.0.0',
      }
      const proto = ruleDraftToProto(draft)
      expect(proto.kind).toBe(TemplatePolicyKind.REQUIRE)
      expect(proto.template?.name).toBe('httproute')
      expect(proto.template?.namespace).toBe(namespaceFor(TemplateScope.ORGANIZATION, 'acme'))
      expect(proto.template?.versionConstraint).toBe('^1.0.0')
      // AC: target must be unset (or explicitly empty) on every new/edited rule.
      expect(proto.target).toBeUndefined()
    })

    it('emits target unset even if a legacy projectPattern field is set on the draft', () => {
      // Defensively accept legacy in-memory drafts that might still carry old
      // fields and verify they are stripped at the proto conversion boundary.
      const legacyDraft = {
        kind: TemplatePolicyKind.EXCLUDE,
        templateKey: linkableKey(TemplateScope.FOLDER, 'team-a', 'gateway'),
        versionConstraint: '',
        projectPattern: '*',
        deploymentPattern: '*',
      } as RuleDraft
      const proto = ruleDraftToProto(legacyDraft)
      expect(proto.target).toBeUndefined()
    })
  })

  describe('ruleProtoToDraft', () => {
    it('produces a draft with no glob pattern fields even when the proto Target was populated', () => {
      // Cast through `unknown` so the test stays readable without hand-rolling
      // the `$typeName` brand fields required by protobuf-es Message types.
      const draft = ruleProtoToDraft({
        kind: TemplatePolicyKind.REQUIRE,
        template: {
          namespace: namespaceFor(TemplateScope.ORGANIZATION, 'acme'),
          name: 'httproute',
          versionConstraint: '',
        },
        target: {
          projectPattern: 'legacy-*',
          deploymentPattern: 'prod-*',
        },
      } as unknown as Parameters<typeof ruleProtoToDraft>[0])
      expect(draft).toEqual({
        kind: TemplatePolicyKind.REQUIRE,
        templateKey: linkableKey(TemplateScope.ORGANIZATION, 'acme', 'httproute'),
        versionConstraint: '',
      })
      const extra = draft as Partial<RuleDraft> & {
        projectPattern?: string
        deploymentPattern?: string
      }
      expect(extra.projectPattern).toBeUndefined()
      expect(extra.deploymentPattern).toBeUndefined()
    })
  })

  describe('validateRuleDraft', () => {
    it('requires a template selection', () => {
      const draft = newEmptyRule()
      expect(validateRuleDraft(draft)).toMatch(/template/i)
    })

    it('passes when a template is selected and no glob fields exist', () => {
      const draft: RuleDraft = {
        kind: TemplatePolicyKind.REQUIRE,
        templateKey: linkableKey(TemplateScope.ORGANIZATION, 'acme', 'httproute'),
        versionConstraint: '',
      }
      expect(validateRuleDraft(draft)).toBeNull()
    })
  })
})

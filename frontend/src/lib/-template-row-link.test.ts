/**
 * Tests for template-row-link.ts (HOL-859).
 *
 * Exercises resolveTemplateRowHref for all three template-family kinds across
 * all three scopes (org / folder / project).
 */

import { describe, it, expect, vi } from 'vitest'

// ---------------------------------------------------------------------------
// Mock console-config so scope-labels helpers resolve predictable prefixes.
// ---------------------------------------------------------------------------

vi.mock('@/lib/console-config', () => ({
  getConsoleConfig: vi.fn().mockReturnValue({
    namespacePrefix: '',
    organizationPrefix: 'org-',
    folderPrefix: 'folder-',
    projectPrefix: 'project-',
  }),
}))

import { resolveTemplateRowHref, parentLabelFromNamespace } from '@/lib/template-row-link'

// ---------------------------------------------------------------------------
// resolveTemplateRowHref
// ---------------------------------------------------------------------------

describe('resolveTemplateRowHref', () => {
  // Template kind
  describe('Template', () => {
    it('resolves org-scope Template link', () => {
      expect(resolveTemplateRowHref('Template', 'org-acme', 'my-tpl')).toBe(
        '/orgs/acme/templates/org-acme/my-tpl',
      )
    })

    it('resolves folder-scope Template link', () => {
      expect(resolveTemplateRowHref('Template', 'folder-platform', 'my-tpl')).toBe(
        '/folders/platform/templates/my-tpl',
      )
    })

    it('resolves project-scope Template link', () => {
      expect(resolveTemplateRowHref('Template', 'project-web', 'my-tpl')).toBe(
        '/projects/web/templates/my-tpl',
      )
    })

    it('returns undefined for unknown namespace', () => {
      expect(resolveTemplateRowHref('Template', 'unknown-ns', 'my-tpl')).toBeUndefined()
    })
  })

  // TemplatePolicy kind
  describe('TemplatePolicy', () => {
    it('resolves org-scope TemplatePolicy link', () => {
      expect(resolveTemplateRowHref('TemplatePolicy', 'org-acme', 'my-policy')).toBe(
        '/orgs/acme/template-policies/my-policy',
      )
    })

    it('resolves folder-scope TemplatePolicy link', () => {
      expect(
        resolveTemplateRowHref('TemplatePolicy', 'folder-platform', 'my-policy'),
      ).toBe('/folders/platform/template-policies/my-policy')
    })

    it('returns undefined for project-scope TemplatePolicy (unsupported)', () => {
      expect(resolveTemplateRowHref('TemplatePolicy', 'project-web', 'my-policy')).toBeUndefined()
    })
  })

  // TemplatePolicyBinding kind
  describe('TemplatePolicyBinding', () => {
    it('resolves org-scope TemplatePolicyBinding link', () => {
      expect(
        resolveTemplateRowHref('TemplatePolicyBinding', 'org-acme', 'my-binding'),
      ).toBe('/orgs/acme/template-policy-bindings/my-binding')
    })

    it('resolves folder-scope TemplatePolicyBinding link', () => {
      expect(
        resolveTemplateRowHref('TemplatePolicyBinding', 'folder-platform', 'my-binding'),
      ).toBe('/folders/platform/template-policy-bindings/my-binding')
    })

    it('returns undefined for project-scope TemplatePolicyBinding (unsupported)', () => {
      expect(
        resolveTemplateRowHref('TemplatePolicyBinding', 'project-web', 'my-binding'),
      ).toBeUndefined()
    })
  })
})

// ---------------------------------------------------------------------------
// parentLabelFromNamespace
// ---------------------------------------------------------------------------

describe('parentLabelFromNamespace', () => {
  it('returns the org name for an org namespace', () => {
    expect(parentLabelFromNamespace('org-acme')).toBe('acme')
  })

  it('returns the folder name for a folder namespace', () => {
    expect(parentLabelFromNamespace('folder-platform')).toBe('platform')
  })

  it('returns the project name for a project namespace', () => {
    expect(parentLabelFromNamespace('project-web')).toBe('web')
  })

  it('returns the raw namespace when no prefix matches', () => {
    expect(parentLabelFromNamespace('unknown-namespace')).toBe('unknown-namespace')
  })
})

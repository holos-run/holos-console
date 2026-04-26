/**
 * Tests for template-row-link.ts (HOL-859 / HOL-978).
 *
 * Exercises resolveTemplateRowHref for all three template-family kinds across
 * all three scopes (org / folder / project).
 *
 * HOL-978: Folder-scoped routes were removed for MVP. Folder-scope now returns
 * undefined (treated as "no link — render plain text") for all three kinds.
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
        '/organizations/acme/templates/org-acme/my-tpl',
      )
    })

    it('returns undefined for folder-scope Template (routes removed for MVP by HOL-978)', () => {
      expect(resolveTemplateRowHref('Template', 'folder-platform', 'my-tpl')).toBeUndefined()
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
        '/organizations/acme/template-policies/my-policy',
      )
    })

    it('returns undefined for folder-scope TemplatePolicy (routes removed for MVP by HOL-978)', () => {
      expect(
        resolveTemplateRowHref('TemplatePolicy', 'folder-platform', 'my-policy'),
      ).toBeUndefined()
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
      ).toBe('/organizations/acme/template-bindings/my-binding')
    })

    it('returns undefined for folder-scope TemplatePolicyBinding (routes removed for MVP by HOL-978)', () => {
      expect(
        resolveTemplateRowHref('TemplatePolicyBinding', 'folder-platform', 'my-binding'),
      ).toBeUndefined()
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

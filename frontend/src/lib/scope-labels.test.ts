import { describe, it, expect } from 'vitest'
import {
  TemplateScope,
  namespaceForOrg,
  namespaceForFolder,
  namespaceForProject,
  scopeLabelFromNamespace,
  scopeNameFromNamespace,
  scopeFromNamespace,
  scopeDisplayLabel,
} from './scope-labels'

describe('scope-labels', () => {
  describe('namespaceForOrg / Folder / Project', () => {
    it('builds the holos-org- prefixed namespace', () => {
      expect(namespaceForOrg('acme')).toBe('holos-org-acme')
    })

    it('builds the holos-fld- prefixed namespace', () => {
      expect(namespaceForFolder('engineering')).toBe('holos-fld-engineering')
    })

    it('builds the holos-prj- prefixed namespace', () => {
      expect(namespaceForProject('web-app')).toBe('holos-prj-web-app')
    })

    it('returns empty string when the scope name is empty', () => {
      expect(namespaceForOrg('')).toBe('')
      expect(namespaceForFolder('')).toBe('')
      expect(namespaceForProject('')).toBe('')
    })
  })

  describe('scopeLabelFromNamespace', () => {
    it.each([
      { ns: 'holos-org-acme', expected: 'org' as const },
      { ns: 'holos-fld-engineering', expected: 'folder' as const },
      { ns: 'holos-prj-web-app', expected: 'project' as const },
    ])('returns $expected for $ns', ({ ns, expected }) => {
      expect(scopeLabelFromNamespace(ns)).toBe(expected)
    })

    it('returns undefined for unknown or empty namespaces', () => {
      expect(scopeLabelFromNamespace('')).toBeUndefined()
      expect(scopeLabelFromNamespace(null)).toBeUndefined()
      expect(scopeLabelFromNamespace(undefined)).toBeUndefined()
      expect(scopeLabelFromNamespace('default')).toBeUndefined()
      expect(scopeLabelFromNamespace('kube-system')).toBeUndefined()
    })
  })

  describe('scopeNameFromNamespace', () => {
    it('strips the org prefix', () => {
      expect(scopeNameFromNamespace('holos-org-acme')).toBe('acme')
    })

    it('strips the folder prefix', () => {
      expect(scopeNameFromNamespace('holos-fld-engineering')).toBe('engineering')
    })

    it('strips the project prefix', () => {
      expect(scopeNameFromNamespace('holos-prj-web-app')).toBe('web-app')
    })

    it('returns empty string for unknown or empty namespaces', () => {
      expect(scopeNameFromNamespace('')).toBe('')
      expect(scopeNameFromNamespace('default')).toBe('')
    })
  })

  describe('scopeFromNamespace', () => {
    it('returns the legacy numeric enum for each scope', () => {
      expect(scopeFromNamespace('holos-org-acme')).toBe(TemplateScope.ORGANIZATION)
      expect(scopeFromNamespace('holos-fld-engineering')).toBe(TemplateScope.FOLDER)
      expect(scopeFromNamespace('holos-prj-web-app')).toBe(TemplateScope.PROJECT)
      expect(scopeFromNamespace('')).toBe(TemplateScope.UNSPECIFIED)
      expect(scopeFromNamespace('default')).toBe(TemplateScope.UNSPECIFIED)
    })
  })

  describe('scopeDisplayLabel', () => {
    it.each([
      { ns: 'holos-org-acme', label: 'Organization' },
      { ns: 'holos-fld-engineering', label: 'Folder' },
      { ns: 'holos-prj-web-app', label: 'Project' },
      { ns: 'default', label: '' },
      { ns: '', label: '' },
    ])('returns "$label" for $ns', ({ ns, label }) => {
      expect(scopeDisplayLabel(ns)).toBe(label)
    })
  })
})

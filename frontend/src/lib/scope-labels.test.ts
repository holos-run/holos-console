import { describe, it, expect, afterEach } from 'vitest'
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
  afterEach(() => {
    delete window.__CONSOLE_CONFIG__
  })

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

  describe('with non-default prefixes from window.__CONSOLE_CONFIG__', () => {
    it.each([
      {
        title: 'operator uses ci- global prefix and renames scope suffixes',
        config: {
          namespacePrefix: 'ci-',
          organizationPrefix: 'organization-',
          folderPrefix: 'folder-',
          projectPrefix: 'project-',
        },
        org: 'acme',
        folder: 'engineering',
        project: 'web-app',
        expected: {
          orgNs: 'ci-organization-acme',
          folderNs: 'ci-folder-engineering',
          projectNs: 'ci-project-web-app',
          orgLabel: 'org' as const,
          folderLabel: 'folder' as const,
          projectLabel: 'project' as const,
        },
      },
      {
        title: 'operator disables global prefix (empty namespacePrefix)',
        config: {
          namespacePrefix: '',
          organizationPrefix: 'org-',
          folderPrefix: 'fld-',
          projectPrefix: 'prj-',
        },
        org: 'acme',
        folder: 'engineering',
        project: 'web-app',
        expected: {
          orgNs: 'org-acme',
          folderNs: 'fld-engineering',
          projectNs: 'prj-web-app',
          orgLabel: 'org' as const,
          folderLabel: 'folder' as const,
          projectLabel: 'project' as const,
        },
      },
      {
        title: 'operator picks fully custom short suffixes',
        config: {
          namespacePrefix: 'qa-',
          organizationPrefix: 'o-',
          folderPrefix: 'f-',
          projectPrefix: 'p-',
        },
        org: 'acme',
        folder: 'engineering',
        project: 'web-app',
        expected: {
          orgNs: 'qa-o-acme',
          folderNs: 'qa-f-engineering',
          projectNs: 'qa-p-web-app',
          orgLabel: 'org' as const,
          folderLabel: 'folder' as const,
          projectLabel: 'project' as const,
        },
      },
    ])('$title', ({ config, org, folder, project, expected }) => {
      window.__CONSOLE_CONFIG__ = {
        devToolsEnabled: false,
        ...config,
      }

      expect(namespaceForOrg(org)).toBe(expected.orgNs)
      expect(namespaceForFolder(folder)).toBe(expected.folderNs)
      expect(namespaceForProject(project)).toBe(expected.projectNs)

      expect(scopeLabelFromNamespace(expected.orgNs)).toBe(expected.orgLabel)
      expect(scopeLabelFromNamespace(expected.folderNs)).toBe(expected.folderLabel)
      expect(scopeLabelFromNamespace(expected.projectNs)).toBe(expected.projectLabel)

      expect(scopeNameFromNamespace(expected.orgNs)).toBe(org)
      expect(scopeNameFromNamespace(expected.folderNs)).toBe(folder)
      expect(scopeNameFromNamespace(expected.projectNs)).toBe(project)

      // A namespace that would have matched the default prefixes must NOT match
      // when the operator has configured non-default prefixes.
      if (expected.orgNs !== 'holos-org-acme') {
        expect(scopeLabelFromNamespace('holos-org-acme')).toBeUndefined()
      }
    })
  })
})

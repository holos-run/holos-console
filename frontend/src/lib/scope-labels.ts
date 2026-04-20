// scope-labels.ts — namespace-aware helpers for (namespace, name)-keyed template
// resources.
//
// Per HOL-619 the Template / TemplatePolicy / TemplatePolicyBinding proto API
// is keyed by `(namespace, name)` only; the legacy `TemplateScope` enum and
// `(scope, scopeName)` pairs were removed from the wire protocol. HOL-623
// unified the editor routes and swapped query hooks onto `namespace: string`.
//
// Namespaces encode the scope via the prefix layout
// `{NamespacePrefix}{ResourcePrefix}{name}`. The four prefix values are
// configurable on the server (CLI flags `--namespace-prefix`,
// `--organization-prefix`, `--folder-prefix`, `--project-prefix`; see
// `console/console.go` Config). Per HOL-722 this module reads the live
// prefixes from `window.__CONSOLE_CONFIG__` via `getConsoleConfig()` so the
// UI stays aligned with deployments that customize the namespace layout.
// `getConsoleConfig()` is a synchronous read of a value injected once into
// `index.html` by the server, so there is no per-render network cost.

import { getConsoleConfig } from './console-config'

/**
 * Scope label values returned by `scopeLabelFromNamespace`.
 *
 * Matches the lowercase tags used in UI labels and in backend namespace
 * annotations (`holos.run/scope`).
 */
export type ScopeLabel = 'org' | 'folder' | 'project'

/**
 * Legacy numeric scope enum preserved for callers that compare against
 * `TemplateScope.ORGANIZATION` / `FOLDER` / `PROJECT`. New code should
 * prefer `ScopeLabel` and reason in namespace strings.
 */
export const TemplateScope = {
  UNSPECIFIED: 0,
  ORGANIZATION: 1,
  FOLDER: 2,
  PROJECT: 3,
} as const
export type TemplateScope = (typeof TemplateScope)[keyof typeof TemplateScope]

interface ScopePrefixes {
  org: string
  folder: string
  project: string
}

function scopePrefixes(): ScopePrefixes {
  const cfg = getConsoleConfig()
  return {
    org: cfg.namespacePrefix + cfg.organizationPrefix,
    folder: cfg.namespacePrefix + cfg.folderPrefix,
    project: cfg.namespacePrefix + cfg.projectPrefix,
  }
}

/** Build the namespace string for an organization-scoped resource. */
export function namespaceForOrg(orgName: string): string {
  return orgName ? scopePrefixes().org + orgName : ''
}

/** Build the namespace string for a folder-scoped resource. */
export function namespaceForFolder(folderName: string): string {
  return folderName ? scopePrefixes().folder + folderName : ''
}

/** Build the namespace string for a project-scoped resource. */
export function namespaceForProject(projectName: string): string {
  return projectName ? scopePrefixes().project + projectName : ''
}

/**
 * Return the scope label for a namespace, derived from the server-configured
 * namespace prefixes.
 */
export function scopeLabelFromNamespace(
  ns: string | undefined | null,
): ScopeLabel | undefined {
  if (!ns) return undefined
  const p = scopePrefixes()
  if (ns.startsWith(p.org)) return 'org'
  if (ns.startsWith(p.folder)) return 'folder'
  if (ns.startsWith(p.project)) return 'project'
  return undefined
}

/**
 * Return the scope name (org/folder/project name) encoded in a namespace.
 *
 * Returns an empty string when the namespace does not match any known prefix.
 */
export function scopeNameFromNamespace(ns: string | undefined | null): string {
  if (!ns) return ''
  const p = scopePrefixes()
  if (ns.startsWith(p.org)) return ns.slice(p.org.length)
  if (ns.startsWith(p.folder)) return ns.slice(p.folder.length)
  if (ns.startsWith(p.project)) return ns.slice(p.project.length)
  return ''
}

/**
 * Return the legacy numeric TemplateScope for a namespace. Kept for
 * components that still drive conditional rendering off the enum values.
 */
export function scopeFromNamespace(ns: string | undefined | null): TemplateScope {
  const label = scopeLabelFromNamespace(ns)
  switch (label) {
    case 'org':
      return TemplateScope.ORGANIZATION
    case 'folder':
      return TemplateScope.FOLDER
    case 'project':
      return TemplateScope.PROJECT
    default:
      return TemplateScope.UNSPECIFIED
  }
}

/**
 * Human-readable scope label ("Organization" / "Folder" / "Project").
 *
 * Returns an empty string when the namespace does not match any known prefix.
 */
export function scopeDisplayLabel(ns: string | undefined | null): string {
  switch (scopeLabelFromNamespace(ns)) {
    case 'org':
      return 'Organization'
    case 'folder':
      return 'Folder'
    case 'project':
      return 'Project'
    default:
      return ''
  }
}

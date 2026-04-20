// scope-labels.ts — namespace-aware helpers for (namespace, name)-keyed template
// resources.
//
// Per HOL-619 the Template / TemplatePolicy / TemplatePolicyBinding proto API
// is keyed by `(namespace, name)` only; the legacy `TemplateScope` enum and
// `(scope, scopeName)` pairs were removed from the wire protocol. This module
// replaces the temporary `scope-shim.ts` introduced while the UI still
// reasoned in `(scope, scopeName)` pairs. HOL-623 unifies the editor routes
// and swaps query hooks onto `namespace: string`.
//
// ## Annotation key contract
//
// `scopeLabelFromNamespace` is a pure prefix-based derivation: it does NOT
// require a NamespaceMetadata lookup. The server assigns namespace prefixes
// deterministically when an Organization/Folder/Project resource is created:
//
//   * Organization → `holos-org-<orgName>`
//   * Folder       → `holos-fld-<folderName>`
//   * Project      → `holos-prj-<projectName>`
//
// These prefixes are stable identifiers that the console relies on to derive
// the *scope label* ("Organization" / "Folder" / "Project") without a second
// round-trip. The equivalent annotation key the backend stamps on each
// namespace — `holos.run/scope` set to `org`/`folder`/`project` — is still
// the authoritative source should a future rename ever decouple the prefix
// from the scope. Until then the prefix is cheaper and correct. If/when the
// bootstrap config endpoint exposes the prefix values for non-default
// deployments, read them here.

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

const NAMESPACE_PREFIX = 'holos-'
const ORG_SUFFIX = 'org-'
const FOLDER_SUFFIX = 'fld-'
const PROJECT_SUFFIX = 'prj-'

const FULL_ORG_PREFIX = NAMESPACE_PREFIX + ORG_SUFFIX
const FULL_FOLDER_PREFIX = NAMESPACE_PREFIX + FOLDER_SUFFIX
const FULL_PROJECT_PREFIX = NAMESPACE_PREFIX + PROJECT_SUFFIX

/** Build the namespace string for an organization-scoped resource. */
export function namespaceForOrg(orgName: string): string {
  return orgName ? FULL_ORG_PREFIX + orgName : ''
}

/** Build the namespace string for a folder-scoped resource. */
export function namespaceForFolder(folderName: string): string {
  return folderName ? FULL_FOLDER_PREFIX + folderName : ''
}

/** Build the namespace string for a project-scoped resource. */
export function namespaceForProject(projectName: string): string {
  return projectName ? FULL_PROJECT_PREFIX + projectName : ''
}

/**
 * Return the scope label for a namespace, derived from the namespace prefix.
 *
 * See the module-level "Annotation key contract" comment for the prefix
 * meanings and a note on the equivalent `holos.run/scope` annotation.
 */
export function scopeLabelFromNamespace(
  ns: string | undefined | null,
): ScopeLabel | undefined {
  if (!ns) return undefined
  if (ns.startsWith(FULL_ORG_PREFIX)) return 'org'
  if (ns.startsWith(FULL_FOLDER_PREFIX)) return 'folder'
  if (ns.startsWith(FULL_PROJECT_PREFIX)) return 'project'
  return undefined
}

/**
 * Return the scope name (org/folder/project name) encoded in a namespace.
 *
 * Returns an empty string when the namespace does not match any known prefix.
 */
export function scopeNameFromNamespace(ns: string | undefined | null): string {
  if (!ns) return ''
  if (ns.startsWith(FULL_ORG_PREFIX)) return ns.slice(FULL_ORG_PREFIX.length)
  if (ns.startsWith(FULL_FOLDER_PREFIX)) return ns.slice(FULL_FOLDER_PREFIX.length)
  if (ns.startsWith(FULL_PROJECT_PREFIX)) return ns.slice(FULL_PROJECT_PREFIX.length)
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

// HOL-619: Frontend compatibility shim for TemplateScope / TemplateScopeRef.
//
// The proto was changed to key Template / TemplatePolicy / TemplatePolicyBinding
// resources by (namespace, name) alone. The UI still reasons in
// (scope, scopeName) pairs today — HOL-623 collapses the UI to a single
// namespace-aware editor. In the meantime this shim provides:
//
//   * A `TemplateScope` enum preserved for UI code paths.
//   * A `TemplateScopeRef` record type preserved for UI code paths.
//   * `namespaceFor(scope, scopeName)` to convert the pair to a namespace
//     string for RPC calls.
//   * `scopeFromNamespace(ns)` / `scopeNameFromNamespace(ns)` to convert
//     incoming namespaces back to the pair for display.
//   * `scopeRefFromNamespace(ns)` convenience.
//
// Prefixes match the server defaults (NamespacePrefix=holos-,
// OrganizationPrefix=org-, FolderPrefix=fld-, ProjectPrefix=prj-). A future
// phase will expose the prefixes via the bootstrap config endpoint and read
// them here; for now they are hardcoded to the documented defaults.
//
// This module is temporary and removed by HOL-623 / HOL-624.

// TemplateScope mirrors the numeric values used by the legacy proto enum so
// existing callers that compare against `TemplateScope.ORGANIZATION` etc
// continue to behave the same way. Uses a const object because the project
// builds under `erasableSyntaxOnly` which forbids non-const-enum TS enums.
export const TemplateScope = {
  UNSPECIFIED: 0,
  ORGANIZATION: 1,
  FOLDER: 2,
  PROJECT: 3,
} as const
export type TemplateScope = (typeof TemplateScope)[keyof typeof TemplateScope]

// TemplateScopeRef is a plain object carrying the legacy (scope, scopeName)
// pair. Constructed via `makeScope` / `makeOrgScope` / `makeFolderScope` /
// `makeProjectScope`.
export interface TemplateScopeRef {
  scope: TemplateScope
  scopeName: string
}

const NAMESPACE_PREFIX = 'holos-'
const ORGANIZATION_PREFIX = 'org-'
const FOLDER_PREFIX = 'fld-'
const PROJECT_PREFIX = 'prj-'

const FULL_ORG_PREFIX = NAMESPACE_PREFIX + ORGANIZATION_PREFIX
const FULL_FOLDER_PREFIX = NAMESPACE_PREFIX + FOLDER_PREFIX
const FULL_PROJECT_PREFIX = NAMESPACE_PREFIX + PROJECT_PREFIX

export function namespaceFor(scope: TemplateScope | number, scopeName: string): string {
  if (!scopeName) return ''
  switch (scope) {
    case TemplateScope.ORGANIZATION:
      return FULL_ORG_PREFIX + scopeName
    case TemplateScope.FOLDER:
      return FULL_FOLDER_PREFIX + scopeName
    case TemplateScope.PROJECT:
      return FULL_PROJECT_PREFIX + scopeName
    default:
      return ''
  }
}

export function namespaceForRef(ref: TemplateScopeRef | undefined): string {
  if (!ref) return ''
  return namespaceFor(ref.scope, ref.scopeName)
}

export function scopeFromNamespace(ns: string | undefined | null): TemplateScope {
  if (!ns) return TemplateScope.UNSPECIFIED
  if (ns.startsWith(FULL_ORG_PREFIX)) return TemplateScope.ORGANIZATION
  if (ns.startsWith(FULL_FOLDER_PREFIX)) return TemplateScope.FOLDER
  if (ns.startsWith(FULL_PROJECT_PREFIX)) return TemplateScope.PROJECT
  return TemplateScope.UNSPECIFIED
}

export function scopeNameFromNamespace(ns: string | undefined | null): string {
  if (!ns) return ''
  if (ns.startsWith(FULL_ORG_PREFIX)) return ns.slice(FULL_ORG_PREFIX.length)
  if (ns.startsWith(FULL_FOLDER_PREFIX)) return ns.slice(FULL_FOLDER_PREFIX.length)
  if (ns.startsWith(FULL_PROJECT_PREFIX)) return ns.slice(FULL_PROJECT_PREFIX.length)
  return ''
}

export function scopeRefFromNamespace(ns: string | undefined | null): TemplateScopeRef {
  return { scope: scopeFromNamespace(ns), scopeName: scopeNameFromNamespace(ns) }
}

export function makeScope(scope: TemplateScope, scopeName: string): TemplateScopeRef {
  return { scope, scopeName }
}

export function makeOrgScope(org: string): TemplateScopeRef {
  return { scope: TemplateScope.ORGANIZATION, scopeName: org }
}

export function makeFolderScope(folder: string): TemplateScopeRef {
  return { scope: TemplateScope.FOLDER, scopeName: folder }
}

export function makeProjectScope(project: string): TemplateScopeRef {
  return { scope: TemplateScope.PROJECT, scopeName: project }
}

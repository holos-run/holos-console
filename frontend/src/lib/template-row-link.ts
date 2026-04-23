/**
 * template-row-link.ts — scope-aware link resolver for the three template-family
 * kinds (Template, TemplatePolicy, TemplatePolicyBinding).
 *
 * Extracted from orgs/$orgName/templates/index.tsx (lines 93-151) so the
 * project-scoped unified Templates index (HOL-859) can reuse the same routing
 * logic without duplicating it.
 *
 * Rules:
 *  - Template   → org / folder / project detail routes keyed by (namespace, name)
 *  - TemplatePolicy → org / folder detail routes keyed by (namespace, name)
 *  - TemplatePolicyBinding → org / folder detail routes keyed by (namespace, name)
 *
 * Returns `undefined` when the namespace does not match any known prefix, which
 * the caller should treat as "no link — render plain text".
 */

import {
  scopeLabelFromNamespace,
  scopeNameFromNamespace,
} from '@/lib/scope-labels'

/** The three kinds the unified Templates index displays. */
export type TemplateKind = 'Template' | 'TemplatePolicy' | 'TemplatePolicyBinding'

/**
 * Resolve the detail-page href for a row in the unified Templates index.
 *
 * @param kind   One of the three template-family kinds.
 * @param namespace  The resource's namespace string (encodes scope + name).
 * @param name   The resource's name within its namespace.
 * @returns The detail href string, or `undefined` if the scope cannot be resolved.
 */
export function resolveTemplateRowHref(
  kind: TemplateKind,
  namespace: string,
  name: string,
): string | undefined {
  const scope = scopeLabelFromNamespace(namespace)
  const scopeName = scopeNameFromNamespace(namespace)

  if (!scope || !scopeName) return undefined

  switch (kind) {
    case 'Template': {
      if (scope === 'org') {
        return `/orgs/${scopeName}/templates/${namespace}/${name}`
      }
      if (scope === 'folder') {
        return `/folders/${scopeName}/templates/${name}`
      }
      if (scope === 'project') {
        return `/projects/${scopeName}/templates/${name}`
      }
      return undefined
    }

    case 'TemplatePolicy': {
      if (scope === 'org') {
        return `/orgs/${scopeName}/template-policies/${name}`
      }
      if (scope === 'folder') {
        return `/folders/${scopeName}/template-policies/${name}`
      }
      // TemplatePolicies do not exist at project scope.
      return undefined
    }

    case 'TemplatePolicyBinding': {
      if (scope === 'org') {
        return `/orgs/${scopeName}/template-bindings/${name}`
      }
      if (scope === 'folder') {
        return `/folders/${scopeName}/template-policy-bindings/${name}`
      }
      // TemplatePolicyBindings do not exist at project scope.
      return undefined
    }
  }
}

/**
 * Derive a human-readable parent label from a namespace.
 *
 * Returns the scoped name (e.g. "my-org", "my-folder", "my-project") so the
 * Parent column in the ResourceGrid makes sense when rows span multiple scopes.
 * Falls back to the raw namespace when no prefix matches.
 */
export function parentLabelFromNamespace(namespace: string): string {
  const name = scopeNameFromNamespace(namespace)
  return name || namespace
}

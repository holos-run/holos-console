/**
 * Centralized UI copy for platform-template surfaces.
 *
 * Platform templates describe eligibility and render-time inclusion; they do
 * not directly "apply" resources to project namespaces. Drift between the org
 * and folder wordings previously claimed the opposite, which was the root
 * cause of the misdescription bug tracked under HOL-580. Consolidating the
 * copy here prevents the wordings from diverging again.
 *
 * The `enabled` flag only controls pickability and render-time unification.
 * Template policies with REQUIRE or EXCLUDE rules control inclusion at
 * render time and do not themselves create, apply, or delete resources.
 */

export const ENABLED_TOGGLE_ACTIVE_DESCRIPTION =
  'Enabled — eligible to appear in linked-template pickers and to be included when rendering downstream templates and deployments.'

export const ENABLED_TOGGLE_INACTIVE_DESCRIPTION =
  'Disabled — hidden from linked-template pickers and excluded from render-time unification. Existing linked references to this template render as no-ops.'

export const ORG_SCOPE_INDEX_DESCRIPTION =
  'Platform templates authored at organization scope are available for inclusion by project templates and deployments throughout the organization.'

export const FOLDER_SCOPE_INDEX_DESCRIPTION =
  'Platform templates authored at this folder scope are available for inclusion by project templates and deployments in this folder and its descendants.'

export const REQUIRE_RULE_DESCRIPTION =
  'REQUIRE — when a project template or deployment matching the target is rendered, include this platform template in the effective ref set. The rule affects render-time ref selection only; it does not itself create, apply, or delete resources.'

export const EXCLUDE_RULE_DESCRIPTION =
  'EXCLUDE — when a project template or deployment matching the target is rendered, remove this platform template from the effective ref set even if explicitly linked.'

/**
 * Returns the description for the enabled toggle based on the template state.
 */
export function enabledToggleDescription(enabled: boolean): string {
  return enabled
    ? ENABLED_TOGGLE_ACTIVE_DESCRIPTION
    : ENABLED_TOGGLE_INACTIVE_DESCRIPTION
}

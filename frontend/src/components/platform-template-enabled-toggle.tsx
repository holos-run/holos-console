import { Switch } from '@/components/ui/switch'
import { enabledToggleDescription } from '@/components/platform-template-copy'

/**
 * PlatformTemplateEnabledToggle renders the shared "Enabled" row used on
 * platform-template detail pages at both org and folder scope.
 *
 * The row is: a fixed-width "Enabled" label, the switch itself, and the
 * scope-agnostic description text sourced from platform-template-copy so
 * there is exactly one place the wording can drift (HOL-580, HOL-583).
 *
 * The component is intentionally scope-agnostic: it takes only the minimum
 * state needed to render and emit changes. The permission-denied hint
 * (org Owner vs folder Owner) is rendered by the parent route because the
 * exact wording differs between scopes and the hint is displayed in a
 * different region of the page (above the CUE editor, not next to the
 * toggle). Keeping the hint outside this component means the component
 * contains zero scope-specific branching.
 */
export interface PlatformTemplateEnabledToggleProps {
  /** Current enabled state of the platform template. */
  enabled: boolean
  /** Whether the current user has permission to toggle enabled. */
  canWrite: boolean
  /** Whether an update mutation is currently in flight. */
  isUpdating: boolean
  /** Invoked with the next enabled state when the user toggles the switch. */
  onChange: (next: boolean) => void
}

export function PlatformTemplateEnabledToggle({
  enabled,
  canWrite,
  isUpdating,
  onChange,
}: PlatformTemplateEnabledToggleProps) {
  return (
    <div className="flex items-center gap-2">
      <span className="w-36 text-sm text-muted-foreground shrink-0">
        Enabled
      </span>
      <Switch
        aria-label="Enabled"
        checked={enabled}
        onCheckedChange={onChange}
        disabled={!canWrite || isUpdating}
      />
      <span className="text-sm text-muted-foreground">
        {enabledToggleDescription(enabled)}
      </span>
    </div>
  )
}

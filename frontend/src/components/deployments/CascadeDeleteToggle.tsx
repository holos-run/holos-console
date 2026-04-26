/**
 * CascadeDeleteToggle renders a toggle that controls whether deleting the
 * dependent Deployment also cascades to the shared singleton produced by a
 * TemplateDependency or TemplateRequirement.
 *
 * The default value is `true` (cascade on), matching the Kubernetes
 * ownerReference model: when a dependent is deleted, the singleton is GC'd
 * automatically when it has no remaining co-owners.
 *
 * The component is purely controlled. The deployment detail page (HOL-991)
 * wires this to useGetDependencyEdgeCascadeDelete /
 * useSetDependencyEdgeCascadeDelete so the value reflects and updates
 * Spec.CascadeDelete on the originating CRD.
 */

import { Switch } from '@/components/ui/switch'
import { Label } from '@/components/ui/label'

export interface CascadeDeleteToggleProps {
  /** Current value of the cascade-delete toggle. Defaults to true. */
  value?: boolean
  /** Callback fired when the user changes the toggle. */
  onChange?: (value: boolean) => void
  /** Optional id for the underlying switch element (for label association). */
  id?: string
  /** When true, the toggle is disabled (read-only). */
  disabled?: boolean
}

/**
 * CascadeDeleteToggle renders a labeled on/off switch for the cascade-delete
 * behaviour of a singleton shared Deployment. Placing it in the deployment
 * create / detail form satisfies the per-edge toggle requirement from HOL-963
 * AC 3.
 */
export function CascadeDeleteToggle({
  value = true,
  onChange,
  id = 'cascade-delete-toggle',
  disabled = false,
}: CascadeDeleteToggleProps) {
  return (
    <div className="flex items-center gap-3">
      <Switch
        id={id}
        aria-label="Cascade delete"
        checked={value}
        onCheckedChange={onChange}
        disabled={disabled}
        data-testid={id}
      />
      <div>
        <Label htmlFor={id} className="cursor-pointer font-normal">
          Cascade delete
        </Label>
        <p className="text-xs text-muted-foreground mt-0.5">
          {value
            ? 'Deleting this deployment will also remove the shared singleton if it has no other dependents.'
            : 'Deleting this deployment will leave the shared singleton in place.'}
        </p>
      </div>
    </div>
  )
}

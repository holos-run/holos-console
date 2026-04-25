import { Checkbox } from '@/components/ui/checkbox'
import { Label } from '@/components/ui/label'

import type { KindFilterProps } from './types'

/** Kind filter checkboxes - rendered only when kinds.length > 1. */
export function KindFilter({
  kinds,
  selectedKindIds,
  onChange,
}: KindFilterProps) {
  const selectedSet = new Set(selectedKindIds)

  const toggle = (id: string) => {
    const next = new Set(selectedSet)
    if (next.has(id)) {
      next.delete(id)
    } else {
      next.add(id)
    }
    onChange(Array.from(next))
  }

  return (
    <div
      className="flex flex-wrap gap-2 items-center"
      aria-label="Filter by kind"
      data-testid="kind-filter"
    >
      {kinds.map((kind) => (
        <div key={kind.id} className="flex items-center gap-1">
          <Checkbox
            id={`kind-${kind.id}`}
            checked={selectedSet.size === 0 || selectedSet.has(kind.id)}
            onCheckedChange={() => toggle(kind.id)}
            aria-label={`Filter ${kind.label}`}
          />
          <Label htmlFor={`kind-${kind.id}`} className="text-sm cursor-pointer">
            {kind.label}
          </Label>
        </div>
      ))}
    </div>
  )
}

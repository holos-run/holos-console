/**
 * SearchFieldsFilter — popover triggered by a filter icon next to the grid
 * title (HOL-990 AC1.3). Lets the operator choose which fields the global
 * search input matches against.
 *
 * Always-available "key" fields: Parent, Name, Display Name. Callers may
 * pass additional hidden fields (e.g. Creator) via `extraFields` so they can
 * be enabled without adding a column.
 */

import { Filter } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Label } from '@/components/ui/label'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'

import {
  DEFAULT_SEARCH_FIELD_IDS,
  type ExtraSearchField,
} from './types'

const KEY_FIELD_LABELS: Record<string, string> = {
  parent: 'Parent',
  name: 'Name',
  displayName: 'Display Name',
}

export interface SearchFieldsFilterProps {
  /** Caller-supplied hidden fields (e.g. Creator) appended to the key fields. */
  extraFields: ExtraSearchField[]
  /** IDs currently included in the global search match. */
  selectedIds: string[]
  /** Called with the new selection when a checkbox toggles. */
  onChange: (ids: string[]) => void
}

export function SearchFieldsFilter({
  extraFields,
  selectedIds,
  onChange,
}: SearchFieldsFilterProps) {
  const allFields = [
    ...DEFAULT_SEARCH_FIELD_IDS.map((id) => ({ id, label: KEY_FIELD_LABELS[id] })),
    ...extraFields,
  ]
  const selectedSet = new Set(selectedIds)

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
    <Popover>
      <PopoverTrigger asChild>
        <Button
          variant="ghost"
          size="icon"
          aria-label="Search fields"
          data-testid="search-fields-filter-trigger"
        >
          <Filter className="h-4 w-4" />
        </Button>
      </PopoverTrigger>
      <PopoverContent
        align="start"
        className="w-56"
        data-testid="search-fields-filter"
      >
        <div className="text-sm font-medium mb-2">Search in fields</div>
        <div className="flex flex-col gap-2">
          {allFields.map((field) => (
            <div key={field.id} className="flex items-center gap-2">
              <Checkbox
                id={`search-field-${field.id}`}
                checked={selectedSet.has(field.id)}
                onCheckedChange={() => toggle(field.id)}
                aria-label={`Search ${field.label}`}
              />
              <Label
                htmlFor={`search-field-${field.id}`}
                className="text-sm cursor-pointer"
              >
                {field.label}
              </Label>
            </div>
          ))}
        </div>
      </PopoverContent>
    </Popover>
  )
}

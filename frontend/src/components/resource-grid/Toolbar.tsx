import { Link } from '@tanstack/react-router'
import { ChevronDown } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'

import { KindFilter } from './KindFilter'
import type { NewButtonProps, ResourceGridToolbarProps } from './types'

export function Toolbar({
  title,
  kinds,
  selectedKindIds,
  globalFilter,
  onGlobalFilterChange,
  onKindIdsChange,
}: ResourceGridToolbarProps) {
  return (
    <div className="mb-3 flex flex-col sm:flex-row gap-2 sm:items-center flex-wrap">
      <Input
        placeholder={`Search ${title.toLowerCase()}…`}
        value={globalFilter}
        onChange={(e) => onGlobalFilterChange(e.target.value)}
        className="max-w-sm"
        aria-label={`Search ${title}`}
      />

      {kinds.length > 1 && (
        <KindFilter
          kinds={kinds}
          selectedKindIds={selectedKindIds}
          onChange={onKindIdsChange}
        />
      )}
    </div>
  )
}

/** "New" button - single link when one kind, dropdown when multiple. */
export function NewButton({ kinds }: NewButtonProps) {
  if (kinds.length === 0) return null

  if (kinds.length === 1) {
    const kind = kinds[0]
    return (
      <Link to={kind.newHref!} search={kind.newSearch}>
        <Button size="sm">New {kind.label}</Button>
      </Link>
    )
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button size="sm">
          New <ChevronDown className="ml-1 h-4 w-4" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {kinds.map((kind) => (
          <DropdownMenuItem key={kind.id} asChild>
            <Link to={kind.newHref!} search={kind.newSearch}>
              {kind.label}
            </Link>
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

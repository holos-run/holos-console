import * as React from 'react'
import { CheckIcon, ChevronsUpDownIcon } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'

export interface ComboboxItem {
  value: string
  label: string
}

export interface ComboboxProps {
  /** The list of items to display in the dropdown. */
  items: ComboboxItem[]
  /** The currently selected value. */
  value: string
  /** Callback invoked when the user selects an item. */
  onValueChange: (value: string) => void
  /** Placeholder text shown on the trigger when nothing is selected. */
  placeholder?: string
  /** Placeholder shown in the search input. */
  searchPlaceholder?: string
  /** Message shown when no items match the search filter. */
  emptyMessage?: string
  /** Accessible label for the trigger button. */
  'aria-label'?: string
  /** Additional class names for the trigger button. */
  className?: string
}

/**
 * Combobox is a searchable single-select dropdown built on shadcn Command and
 * Popover. It follows the Linear-style pattern: a trigger button that opens a
 * popover with a text search input and a keyboard-navigable list of items.
 *
 * Use Combobox for all collection selection inputs that present more than 3
 * static choices. See AGENTS.md "Selection Components" guardrail.
 */
export function Combobox({
  items,
  value,
  onValueChange,
  placeholder = 'Select…',
  searchPlaceholder = 'Search…',
  emptyMessage = 'No results found.',
  className,
  'aria-label': ariaLabel,
}: ComboboxProps) {
  const [open, setOpen] = React.useState(false)

  const selectedLabel = items.find((item) => item.value === value)?.label ?? value

  const handleSelect = (selectedValue: string) => {
    onValueChange(selectedValue)
    setOpen(false)
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          role="combobox"
          aria-expanded={open}
          aria-label={ariaLabel}
          className={cn('w-full justify-between', className)}
        >
          <span className={cn(!value && 'text-muted-foreground')}>
            {value ? selectedLabel : placeholder}
          </span>
          <ChevronsUpDownIcon className="ml-2 h-4 w-4 shrink-0 opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-[--radix-popover-trigger-width] p-0" align="start">
        <Command>
          <CommandInput placeholder={searchPlaceholder} />
          <CommandList>
            <CommandEmpty>{emptyMessage}</CommandEmpty>
            <CommandGroup>
              {items.map((item) => (
                <CommandItem
                  key={item.value}
                  value={item.value}
                  onSelect={handleSelect}
                >
                  <CheckIcon
                    className={cn(
                      'mr-2 h-4 w-4',
                      value === item.value ? 'opacity-100' : 'opacity-0',
                    )}
                  />
                  {item.label}
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}

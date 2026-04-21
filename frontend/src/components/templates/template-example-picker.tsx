import * as React from 'react'
import { ChevronsUpDownIcon } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { cn } from '@/lib/utils'
import { useListTemplateExamples } from '@/queries/templates'
import type { TemplateExample } from '@/queries/templates'

export interface TemplateExamplePickerProps {
  /**
   * Called with the full example payload when the user selects an item.
   * Consumers are responsible for populating their own form fields
   * (displayName, name/slug, description, cueTemplate) from the payload in a
   * single action.
   */
  onSelect: (example: TemplateExample) => void
  /** Trigger button copy. Defaults to "Load Example". */
  label?: string
  /** Placeholder shown in the search input. */
  searchPlaceholder?: string
  /** Disable the picker (e.g. when the viewer lacks write access). */
  disabled?: boolean
  /** Additional class names for the trigger button. */
  className?: string
}

/**
 * TemplateExamplePicker renders a single trigger button that expands into a
 * searchable list of built-in CUE example templates from the backend
 * `ListTemplateExamples` RPC. Selecting an example invokes `onSelect` with
 * the full payload so the parent form can populate Display Name, Name (slug),
 * Description, and CUE Template in one action.
 *
 * Built on the shared `cmdk` Command + shadcn Popover primitives — the same
 * foundation as `components/ui/combobox.tsx`. The picker renders a two-line
 * row per example: the `displayName` as the primary label and `description`
 * as dimmed secondary text. Search matches against both fields.
 *
 * Introduced in HOL-798 as part of the HOL-795 example-picker initiative.
 * The server is the sole source of example content (HOL-796, HOL-797); the
 * frontend never hard-codes CUE bodies.
 */
export function TemplateExamplePicker({
  onSelect,
  label = 'Load Example',
  searchPlaceholder = 'Search examples…',
  disabled = false,
  className,
}: TemplateExamplePickerProps) {
  const [open, setOpen] = React.useState(false)
  const { data: examples, isPending, error } = useListTemplateExamples()

  const handleSelect = (example: TemplateExample) => {
    onSelect(example)
    setOpen(false)
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          size="sm"
          type="button"
          role="combobox"
          aria-expanded={open}
          aria-label={label}
          disabled={disabled}
          className={cn('justify-between gap-2', className)}
        >
          <span>{label}</span>
          <ChevronsUpDownIcon className="h-4 w-4 shrink-0 opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-[22rem] p-0" align="end">
        <Command
          // Match against both the display name and description so typing
          // either a title fragment ("HTTPRoute") or a word from the
          // description ("ingress") filters the list.
          filter={(value, search) => {
            if (!search) return 1
            return value.toLowerCase().includes(search.toLowerCase()) ? 1 : 0
          }}
        >
          <CommandInput placeholder={searchPlaceholder} aria-label={searchPlaceholder} />
          <CommandList>
            {isPending ? (
              <div
                role="status"
                aria-live="polite"
                className="py-6 text-center text-sm text-muted-foreground"
              >
                Loading examples…
              </div>
            ) : error ? (
              <div
                role="alert"
                className="py-6 text-center text-sm text-destructive"
              >
                Failed to load examples.
              </div>
            ) : (
              <>
                <CommandEmpty>No examples found.</CommandEmpty>
                {examples && examples.length > 0 && (
                  <CommandGroup>
                    {examples.map((example) => (
                      <CommandItem
                        key={example.name}
                        value={`${example.displayName} ${example.description} ${example.name}`}
                        onSelect={() => handleSelect(example)}
                        className="flex flex-col items-start gap-0.5 py-2"
                      >
                        <span className="text-sm font-medium">
                          {example.displayName}
                        </span>
                        {example.description && (
                          <span className="text-xs text-muted-foreground">
                            {example.description}
                          </span>
                        )}
                      </CommandItem>
                    ))}
                  </CommandGroup>
                )}
              </>
            )}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}

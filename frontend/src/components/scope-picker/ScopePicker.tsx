import { ChevronDown } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { useProject } from '@/lib/project-context'
import { cn } from '@/lib/utils'

/** The two scope options — organization or project. No folder option. */
export type Scope = 'organization' | 'project'

export interface ScopePickerProps {
  /** Currently selected scope. */
  value: Scope
  /** Called when the user selects a different scope. */
  onChange: (next: Scope) => void
  /** When true the entire picker is non-interactive. */
  disabled?: boolean
  /** Additional class names forwarded to the trigger button. */
  className?: string
}

const SCOPE_LABELS: Record<Scope, string> = {
  organization: 'Organization',
  project: 'Project',
}

/**
 * ScopePicker renders a controlled dropdown button that lets a "new resource"
 * form choose between Organization and Project scope. It reads the currently-
 * selected project from `useProject()` to determine whether the Project item
 * should be enabled, but it never writes to either context store.
 *
 * Usage:
 * ```tsx
 * <ScopePicker value={scope} onChange={setScope} />
 * ```
 */
export function ScopePicker({ value, onChange, disabled, className }: ScopePickerProps) {
  const { selectedProject } = useProject()
  const projectDisabled = selectedProject === null

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild disabled={disabled}>
        <Button
          variant="outline"
          size="sm"
          className={cn('flex items-center gap-1', className)}
          data-testid="scope-picker-trigger"
        >
          {SCOPE_LABELS[value]}
          <ChevronDown className="h-4 w-4 opacity-60" aria-hidden="true" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start">
        <DropdownMenuItem
          data-testid="scope-picker-item-organization"
          onSelect={() => onChange('organization')}
        >
          Organization
        </DropdownMenuItem>
        {projectDisabled ? (
          <TooltipProvider>
            <Tooltip>
              <TooltipTrigger asChild>
                {/*
                  Radix DropdownMenuItem with `disabled` does not forward the
                  mouse-enter event needed for the tooltip, so we wrap with a
                  span that serves as the tooltip anchor.
                */}
                <span
                  className="block"
                  data-testid="scope-picker-item-project-wrapper"
                >
                  <DropdownMenuItem
                    data-testid="scope-picker-item-project"
                    disabled
                  >
                    Project
                  </DropdownMenuItem>
                </span>
              </TooltipTrigger>
              <TooltipContent side="right">
                Select a project first
              </TooltipContent>
            </Tooltip>
          </TooltipProvider>
        ) : (
          <DropdownMenuItem
            data-testid="scope-picker-item-project"
            onSelect={() => onChange('project')}
          >
            Project
          </DropdownMenuItem>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

import type { ReactNode } from 'react'

interface ToggleOption {
  value: string
  label: string
  icon?: ReactNode
}

interface ViewModeToggleProps {
  value: string
  onValueChange: (value: string) => void
  options: [ToggleOption, ToggleOption]
}

/**
 * Pill-style two-option toggle used across detail and profile pages.
 * The primary label (e.g. "Editor", "Data", "Claims") is injected via options[0];
 * the secondary label (e.g. "Raw", "Resource") via options[1].
 */
export function ViewModeToggle({ value, onValueChange, options }: ViewModeToggleProps) {
  return (
    <div className="inline-flex items-center rounded-md border border-border bg-muted/40 p-0.5">
      {options.map((opt) => (
        <button
          key={opt.value}
          onClick={() => onValueChange(opt.value)}
          className={`inline-flex items-center gap-1.5 rounded-[5px] px-3 py-1 text-xs font-medium transition-colors ${
            value === opt.value
              ? 'bg-background text-foreground shadow-sm'
              : 'text-muted-foreground hover:text-foreground'
          }`}
        >
          {opt.icon}
          {opt.label}
        </button>
      ))}
    </div>
  )
}

import { useState, useCallback, useRef, useEffect } from 'react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Checkbox } from '@/components/ui/checkbox'
import { Eye, EyeOff, Copy, Plus, Trash2 } from 'lucide-react'

interface Entry {
  id: string
  key: string
  value: string
}

export interface SecretDataGridProps {
  data: Record<string, Uint8Array>
  onChange: (data: Record<string, Uint8Array>) => void
  readOnly?: boolean
}

const encoder = new TextEncoder()
const decoder = new TextDecoder()

function dataToEntries(data: Record<string, Uint8Array>, genId: () => string): Entry[] {
  return Object.entries(data)
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([key, value]) => ({ id: genId(), key, value: decoder.decode(value) }))
}

function entriesToData(entries: Entry[], trailingNewline: boolean): Record<string, Uint8Array> {
  const result: Record<string, Uint8Array> = {}
  for (const entry of entries) {
    if (entry.key !== '') {
      let value = entry.value
      if (trailingNewline && value.length > 0 && !value.endsWith('\n')) {
        value += '\n'
      }
      result[entry.key] = encoder.encode(value)
    }
  }
  return result
}

function AutoExpandTextarea({
  value,
  onChange,
  placeholder,
  readOnly,
  className,
}: {
  value: string
  onChange?: (value: string) => void
  placeholder?: string
  readOnly?: boolean
  className?: string
}) {
  const ref = useRef<HTMLTextAreaElement>(null)

  useEffect(() => {
    const el = ref.current
    if (!el) return
    el.style.height = 'auto'
    const maxHeight = window.innerWidth < 640 ? 160 : 192 // ~5 rows mobile, ~8 rows desktop
    el.style.height = `${Math.min(el.scrollHeight, maxHeight)}px`
  }, [value])

  return (
    <textarea
      ref={ref}
      rows={1}
      value={value}
      onChange={(e) => onChange?.(e.target.value)}
      placeholder={placeholder}
      readOnly={readOnly}
      className={`flex w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50 font-mono resize-none overflow-auto ${className ?? ''}`}
    />
  )
}

export function SecretDataGrid({ data, onChange, readOnly = false }: SecretDataGridProps) {
  const nextIdRef = useRef(0)
  const genId = useCallback(() => `grid-${++nextIdRef.current}`, [])

  const [entries, setEntries] = useState<Entry[]>(() => {
    const parsed = dataToEntries(data, genId)
    // Show one empty row by default when no data
    return parsed.length > 0 ? parsed : [{ id: genId(), key: '', value: '' }]
  })
  const [trailingNewline, setTrailingNewline] = useState(true)
  const [revealedKeys, setRevealedKeys] = useState<Set<string>>(new Set())

  const emitChange = useCallback(
    (newEntries: Entry[], newTrailingNewline?: boolean) => {
      setEntries(newEntries)
      onChange(entriesToData(newEntries, newTrailingNewline ?? trailingNewline))
    },
    [onChange, trailingNewline],
  )

  const handleKeyChange = (id: string, key: string) => {
    emitChange(entries.map((e) => (e.id === id ? { ...e, key } : e)))
  }

  const handleValueChange = (id: string, value: string) => {
    emitChange(entries.map((e) => (e.id === id ? { ...e, value } : e)))
  }

  const handleAddRow = () => {
    emitChange([...entries, { id: genId(), key: '', value: '' }])
  }

  const handleRemoveRow = (id: string) => {
    const newEntries = entries.filter((e) => e.id !== id)
    emitChange(newEntries.length > 0 ? newEntries : [{ id: genId(), key: '', value: '' }])
  }

  const handleTrailingNewlineChange = (checked: boolean) => {
    setTrailingNewline(checked)
    onChange(entriesToData(entries, checked))
  }

  const toggleReveal = (key: string) => {
    setRevealedKeys((prev) => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }

  const handleCopy = (value: string) => {
    navigator.clipboard.writeText(value)
  }

  // Detect duplicate keys
  const keyCounts = new Map<string, number>()
  for (const entry of entries) {
    if (entry.key !== '') {
      keyCounts.set(entry.key, (keyCounts.get(entry.key) || 0) + 1)
    }
  }

  if (readOnly) {
    const keys = Object.keys(data).sort()
    if (keys.length === 0) {
      return <p className="text-sm text-muted-foreground">No data. Switch to edit mode to add key-value pairs.</p>
    }
    return (
      <div className="space-y-1">
        {/* Header */}
        <div className="grid grid-cols-[1fr_2fr_auto] gap-2 px-1">
          <span className="text-xs font-medium text-muted-foreground uppercase tracking-wider">Key</span>
          <span className="text-xs font-medium text-muted-foreground uppercase tracking-wider">Value</span>
          <span className="w-16" />
        </div>
        {keys.map((key) => {
          const isRevealed = revealedKeys.has(key)
          const rawValue = decoder.decode(data[key])
          return (
            <div key={key} className="grid grid-cols-[1fr_2fr_auto] gap-2 items-start border rounded-md p-2">
              <span className="text-sm font-medium font-mono truncate py-1">{key}</span>
              <div className="min-w-0">
                {isRevealed ? (
                  <pre className="font-mono text-sm whitespace-pre-wrap break-all bg-muted p-2 rounded-md">
                    {rawValue}
                  </pre>
                ) : (
                  <p className="font-mono text-sm text-muted-foreground py-1">{'••••••••'}</p>
                )}
              </div>
              <div className="flex gap-1">
                <Button variant="ghost" size="icon" aria-label={isRevealed ? 'hide' : 'reveal'} onClick={() => toggleReveal(key)}>
                  {isRevealed ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                </Button>
                <Button variant="ghost" size="icon" aria-label="copy" onClick={() => handleCopy(rawValue)}>
                  <Copy className="h-4 w-4" />
                </Button>
              </div>
            </div>
          )
        })}
      </div>
    )
  }

  return (
    <div className="space-y-1">
      {/* Header */}
      <div className="grid grid-cols-[1fr_2fr_auto] gap-2 px-1">
        <span className="text-xs font-medium text-muted-foreground uppercase tracking-wider">Key</span>
        <span className="text-xs font-medium text-muted-foreground uppercase tracking-wider">Value</span>
        <span className="w-9" />
      </div>

      {entries.map((entry) => {
        const isDuplicate = (keyCounts.get(entry.key) || 0) > 1
        return (
          <div key={entry.id} className="grid grid-cols-[1fr_2fr_auto] gap-2 items-start">
            <div>
              <Input
                placeholder="key"
                value={entry.key}
                onChange={(e) => handleKeyChange(entry.id, e.target.value)}
                className={`font-mono ${isDuplicate ? 'border-destructive' : ''}`}
              />
              {isDuplicate && (
                <p className="text-xs text-destructive mt-1">Duplicate key</p>
              )}
            </div>
            <AutoExpandTextarea
              value={entry.value}
              onChange={(v) => handleValueChange(entry.id, v)}
              placeholder="value"
            />
            <Button
              variant="ghost"
              size="icon"
              aria-label="remove row"
              onClick={() => handleRemoveRow(entry.id)}
            >
              <Trash2 className="h-4 w-4" />
            </Button>
          </div>
        )
      })}

      <div className="flex items-center gap-4 pt-2">
        <Button variant="outline" size="sm" onClick={handleAddRow}>
          <Plus className="h-4 w-4 mr-1" />
          Add Row
        </Button>
        <div className="flex items-center gap-2">
          <Checkbox
            id="grid-trailing-newline"
            checked={trailingNewline}
            onCheckedChange={(checked) => handleTrailingNewlineChange(checked === true)}
          />
          <label htmlFor="grid-trailing-newline" className="text-sm">
            Ensure trailing newline
          </label>
        </div>
      </div>
    </div>
  )
}

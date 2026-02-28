import { useState, useCallback } from 'react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Checkbox } from '@/components/ui/checkbox'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import { Plus, Trash2, Info } from 'lucide-react'

interface Entry {
  id: string
  filename: string
  content: string
}

export interface SecretDataEditorProps {
  initialData: Record<string, Uint8Array>
  onChange: (data: Record<string, Uint8Array>) => void
}

let nextId = 0
function genId(): string {
  return `entry-${++nextId}`
}

function entriesToData(entries: Entry[], trailingNewline: boolean): Record<string, Uint8Array> {
  const encoder = new TextEncoder()
  const data: Record<string, Uint8Array> = {}
  for (const entry of entries) {
    if (entry.filename !== '') {
      let value = entry.content
      if (trailingNewline && value.length > 0 && !value.endsWith('\n')) {
        value += '\n'
      }
      data[entry.filename] = encoder.encode(value)
    }
  }
  return data
}

function dataToEntries(data: Record<string, Uint8Array>): Entry[] {
  const decoder = new TextDecoder()
  return Object.entries(data).map(([filename, value]) => ({
    id: genId(),
    filename,
    content: decoder.decode(value),
  }))
}

export function SecretDataEditor({ initialData, onChange }: SecretDataEditorProps) {
  const [entries, setEntries] = useState<Entry[]>(() => dataToEntries(initialData))
  const [trailingNewline, setTrailingNewline] = useState(true)

  const update = useCallback(
    (newEntries: Entry[], newTrailingNewline?: boolean) => {
      setEntries(newEntries)
      onChange(entriesToData(newEntries, newTrailingNewline ?? trailingNewline))
    },
    [onChange, trailingNewline],
  )

  const handleTrailingNewlineChange = (checked: boolean) => {
    setTrailingNewline(checked)
    onChange(entriesToData(entries, checked))
  }

  const handleAdd = () => {
    update([...entries, { id: genId(), filename: '', content: '' }])
  }

  const handleRemove = (id: string) => {
    update(entries.filter((e) => e.id !== id))
  }

  const handleFilenameChange = (id: string, filename: string) => {
    update(entries.map((e) => (e.id === id ? { ...e, filename } : e)))
  }

  const handleContentChange = (id: string, content: string) => {
    update(entries.map((e) => (e.id === id ? { ...e, content } : e)))
  }

  // Detect duplicate keys
  const keyCounts = new Map<string, number>()
  for (const entry of entries) {
    if (entry.filename !== '') {
      keyCounts.set(entry.filename, (keyCounts.get(entry.filename) || 0) + 1)
    }
  }

  return (
    <div>
      {entries.map((entry) => {
        const isDuplicate = (keyCounts.get(entry.filename) || 0) > 1
        return (
          <div key={entry.id} className="flex flex-col sm:flex-row gap-2 items-start mb-3">
            <div className="sm:w-48">
              <Input
                placeholder="key"
                value={entry.filename}
                onChange={(e) => handleFilenameChange(entry.id, e.target.value)}
                className={isDuplicate ? 'border-destructive' : ''}
              />
              {isDuplicate && (
                <p className="text-xs text-destructive mt-1">Duplicate key</p>
              )}
            </div>
            <Textarea
              placeholder="value"
              rows={3}
              value={entry.content}
              onChange={(e) => handleContentChange(entry.id, e.target.value)}
              className="flex-1 font-mono text-sm"
            />
            <Button
              variant="ghost"
              size="icon"
              aria-label="remove key entry"
              onClick={() => handleRemove(entry.id)}
            >
              <Trash2 className="h-4 w-4" />
            </Button>
          </div>
        )
      })}
      <div className="flex items-center gap-2">
        <Button variant="outline" size="sm" onClick={handleAdd}>
          <Plus className="h-4 w-4 mr-1" />
          Add Key
        </Button>
        <TooltipProvider>
          <Tooltip>
            <TooltipTrigger asChild>
              <Info className="h-4 w-4 text-muted-foreground" />
            </TooltipTrigger>
            <TooltipContent>
              <p>A key is often a filename or environment variable name, e.g. .env, config.yaml, or API_KEY</p>
            </TooltipContent>
          </Tooltip>
        </TooltipProvider>
      </div>
      <div className="flex items-center gap-2 mt-2">
        <Checkbox
          id="trailing-newline"
          checked={trailingNewline}
          onCheckedChange={(checked) => handleTrailingNewlineChange(checked === true)}
        />
        <label htmlFor="trailing-newline" className="text-sm">
          Ensure trailing newline
        </label>
      </div>
    </div>
  )
}

import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Checkbox } from '@/components/ui/checkbox'
import { Eye, EyeOff, Copy, Pencil, Plus } from 'lucide-react'

export interface SecretDataViewerProps {
  data: Record<string, Uint8Array>
  onChange: (data: Record<string, Uint8Array>) => void
}

const decoder = new TextDecoder()
const encoder = new TextEncoder()

export function SecretDataViewer({ data, onChange }: SecretDataViewerProps) {
  const [revealedKeys, setRevealedKeys] = useState<Set<string>>(new Set())
  const [editingKey, setEditingKey] = useState<string | null>(null)
  const [editValue, setEditValue] = useState('')
  const [trailingNewline, setTrailingNewline] = useState(true)
  const [addingKey, setAddingKey] = useState(false)
  const [newKeyName, setNewKeyName] = useState('')
  const [newKeyValue, setNewKeyValue] = useState('')

  const toggleReveal = (key: string) => {
    setRevealedKeys((prev) => {
      const next = new Set(prev)
      if (next.has(key)) {
        next.delete(key)
      } else {
        next.add(key)
      }
      return next
    })
  }

  const handleCopy = (key: string) => {
    const value = decoder.decode(data[key])
    navigator.clipboard.writeText(value)
  }

  const handleEditStart = (key: string) => {
    setEditValue(decoder.decode(data[key]))
    setEditingKey(key)
  }

  const handleEditSave = (key: string) => {
    let value = editValue
    if (trailingNewline && value.length > 0 && !value.endsWith('\n')) {
      value += '\n'
    }
    const newData = { ...data, [key]: encoder.encode(value) }
    onChange(newData)
    setEditingKey(null)
  }

  const handleEditCancel = () => {
    setEditingKey(null)
    setEditValue('')
  }

  const handleAddKeySave = () => {
    if (newKeyName === '') return
    let value = newKeyValue
    if (trailingNewline && value.length > 0 && !value.endsWith('\n')) {
      value += '\n'
    }
    onChange({ ...data, [newKeyName]: encoder.encode(value) })
    setAddingKey(false)
    setNewKeyName('')
    setNewKeyValue('')
  }

  const handleAddKeyCancel = () => {
    setAddingKey(false)
    setNewKeyName('')
    setNewKeyValue('')
  }

  const keys = Object.keys(data).sort()

  return (
    <div>
      {keys.map((key) => {
        const isRevealed = revealedKeys.has(key)
        const isEditing = editingKey === key

        return (
          <div key={key} className="mb-3 p-3 border rounded-md">
            <p className="text-sm font-medium mb-2">{key}</p>

            {isEditing ? (
              <div>
                <Textarea
                  rows={3}
                  value={editValue}
                  onChange={(e) => setEditValue(e.target.value)}
                  className="font-mono text-sm mb-2"
                />
                <div className="flex items-center gap-2 mb-2">
                  <Checkbox
                    id={`trailing-newline-${key}`}
                    checked={trailingNewline}
                    onCheckedChange={(checked) => setTrailingNewline(checked === true)}
                  />
                  <label htmlFor={`trailing-newline-${key}`} className="text-sm">
                    Ensure trailing newline
                  </label>
                </div>
                <div className="flex gap-2">
                  <Button size="sm" onClick={() => handleEditSave(key)}>Done</Button>
                  <Button size="sm" variant="ghost" onClick={handleEditCancel}>Cancel</Button>
                </div>
              </div>
            ) : isRevealed ? (
              <div>
                <pre className="font-mono text-sm whitespace-pre-wrap break-all bg-muted p-2 rounded-md">
                  {decoder.decode(data[key])}
                </pre>
                <div className="flex gap-1 mt-2">
                  <Button variant="ghost" size="sm" onClick={() => toggleReveal(key)}>
                    <EyeOff className="h-4 w-4 mr-1" />
                    Hide
                  </Button>
                  <Button variant="ghost" size="icon" aria-label="copy" onClick={() => handleCopy(key)}>
                    <Copy className="h-4 w-4" />
                  </Button>
                  <Button variant="ghost" size="sm" onClick={() => handleEditStart(key)}>
                    <Pencil className="h-4 w-4 mr-1" />
                    Edit
                  </Button>
                </div>
              </div>
            ) : (
              <div>
                <p className="font-mono text-sm text-muted-foreground">{'••••••••'}</p>
                <div className="flex gap-1 mt-2">
                  <Button variant="ghost" size="sm" onClick={() => toggleReveal(key)}>
                    <Eye className="h-4 w-4 mr-1" />
                    Reveal
                  </Button>
                  <Button variant="ghost" size="icon" aria-label="copy" onClick={() => handleCopy(key)}>
                    <Copy className="h-4 w-4" />
                  </Button>
                  <Button variant="ghost" size="sm" onClick={() => handleEditStart(key)}>
                    <Pencil className="h-4 w-4 mr-1" />
                    Edit
                  </Button>
                </div>
              </div>
            )}
          </div>
        )
      })}

      {addingKey ? (
        <div className="mb-3 p-3 border rounded-md">
          <Input
            placeholder="key name"
            value={newKeyName}
            onChange={(e) => setNewKeyName(e.target.value)}
            className="mb-2"
          />
          <Textarea
            placeholder="value"
            rows={3}
            value={newKeyValue}
            onChange={(e) => setNewKeyValue(e.target.value)}
            className="font-mono text-sm mb-2"
          />
          <div className="flex items-center gap-2 mb-2">
            <Checkbox
              id="add-key-trailing-newline"
              checked={trailingNewline}
              onCheckedChange={(checked) => setTrailingNewline(checked === true)}
            />
            <label htmlFor="add-key-trailing-newline" className="text-sm">
              Ensure trailing newline
            </label>
          </div>
          <div className="flex gap-2">
            <Button size="sm" onClick={handleAddKeySave} disabled={newKeyName === ''}>Done</Button>
            <Button size="sm" variant="ghost" onClick={handleAddKeyCancel}>Cancel</Button>
          </div>
        </div>
      ) : (
        <Button variant="outline" size="sm" onClick={() => setAddingKey(true)}>
          <Plus className="h-4 w-4 mr-1" />
          Add Key
        </Button>
      )}
    </div>
  )
}

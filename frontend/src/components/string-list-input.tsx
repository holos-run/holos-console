import { useState } from 'react'
import { X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

interface StringListInputProps {
  value: string[]
  onChange: (value: string[]) => void
  placeholder?: string
  addLabel?: string
}

// StringListInput renders a dynamic list of strings with add/remove controls.
export function StringListInput({ value, onChange, placeholder, addLabel = 'Add' }: StringListInputProps) {
  const [inputValue, setInputValue] = useState('')

  const addItem = () => {
    const trimmed = inputValue.trim()
    if (trimmed) {
      onChange([...value, trimmed])
      setInputValue('')
    }
  }

  const removeItem = (idx: number) => {
    onChange(value.filter((_, i) => i !== idx))
  }

  return (
    <div className="space-y-1">
      {value.map((item, idx) => (
        <div key={idx} className="flex items-center gap-2">
          <span className="flex-1 font-mono text-sm bg-muted rounded px-2 py-1">{item}</span>
          <Button
            type="button"
            variant="ghost"
            size="icon"
            className="h-7 w-7"
            onClick={() => removeItem(idx)}
            aria-label={`remove item ${idx}`}
          >
            <X className="h-3 w-3" />
          </Button>
        </div>
      ))}
      <div className="flex gap-2">
        <Input
          className="h-8 text-sm font-mono"
          value={inputValue}
          onChange={(e) => setInputValue(e.target.value)}
          placeholder={placeholder}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault()
              addItem()
            }
          }}
          aria-label={placeholder ?? 'new item'}
        />
        <Button type="button" variant="outline" size="sm" onClick={addItem}>
          {addLabel}
        </Button>
      </div>
    </div>
  )
}

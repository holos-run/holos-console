import { useState, useCallback } from 'react'
import { Box, Button, IconButton, TextField, Stack } from '@mui/material'
import DeleteIcon from '@mui/icons-material/Delete'
import AddIcon from '@mui/icons-material/Add'

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

function entriesToData(entries: Entry[]): Record<string, Uint8Array> {
  const encoder = new TextEncoder()
  const data: Record<string, Uint8Array> = {}
  for (const entry of entries) {
    if (entry.filename !== '') {
      data[entry.filename] = encoder.encode(entry.content)
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

  const update = useCallback(
    (newEntries: Entry[]) => {
      setEntries(newEntries)
      onChange(entriesToData(newEntries))
    },
    [onChange],
  )

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

  // Detect duplicate filenames
  const filenameCounts = new Map<string, number>()
  for (const entry of entries) {
    if (entry.filename !== '') {
      filenameCounts.set(entry.filename, (filenameCounts.get(entry.filename) || 0) + 1)
    }
  }

  return (
    <Box>
      {entries.map((entry) => {
        const isDuplicate = (filenameCounts.get(entry.filename) || 0) > 1
        return (
          <Stack key={entry.id} direction="row" spacing={1} alignItems="flex-start" sx={{ mb: 2 }}>
            <TextField
              size="small"
              placeholder="filename"
              value={entry.filename}
              onChange={(e) => handleFilenameChange(entry.id, e.target.value)}
              error={isDuplicate}
              helperText={isDuplicate ? 'Duplicate filename' : undefined}
              sx={{ width: 200 }}
            />
            <TextField
              size="small"
              placeholder="file content"
              multiline
              minRows={3}
              value={entry.content}
              onChange={(e) => handleContentChange(entry.id, e.target.value)}
              slotProps={{
                input: {
                  sx: { fontFamily: 'monospace' },
                },
              }}
              sx={{ flexGrow: 1 }}
            />
            <IconButton
              aria-label="remove file entry"
              onClick={() => handleRemove(entry.id)}
              size="small"
            >
              <DeleteIcon />
            </IconButton>
          </Stack>
        )
      })}
      <Button startIcon={<AddIcon />} onClick={handleAdd} size="small">
        Add File
      </Button>
    </Box>
  )
}

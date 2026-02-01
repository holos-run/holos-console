import { useState } from 'react'
import { Box, Button, IconButton, Stack, TextField, Typography } from '@mui/material'
import VisibilityIcon from '@mui/icons-material/Visibility'
import VisibilityOffIcon from '@mui/icons-material/VisibilityOff'
import ContentCopyIcon from '@mui/icons-material/ContentCopy'
import EditIcon from '@mui/icons-material/Edit'

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
    const newData = { ...data, [key]: encoder.encode(editValue) }
    onChange(newData)
    setEditingKey(null)
  }

  const handleEditCancel = () => {
    setEditingKey(null)
    setEditValue('')
  }

  const keys = Object.keys(data).sort()

  return (
    <Box>
      {keys.map((key) => {
        const isRevealed = revealedKeys.has(key)
        const isEditing = editingKey === key

        return (
          <Box key={key} sx={{ mb: 2, p: 2, border: 1, borderColor: 'divider', borderRadius: 1 }}>
            <Typography variant="subtitle2" sx={{ mb: 1 }}>
              {key}
            </Typography>

            {isEditing ? (
              <Box>
                <TextField
                  fullWidth
                  multiline
                  minRows={3}
                  size="small"
                  value={editValue}
                  onChange={(e) => setEditValue(e.target.value)}
                  slotProps={{
                    input: {
                      sx: { fontFamily: 'monospace' },
                    },
                  }}
                  sx={{ mb: 1 }}
                />
                <Stack direction="row" spacing={1}>
                  <Button size="small" variant="contained" onClick={() => handleEditSave(key)}>
                    Save
                  </Button>
                  <Button size="small" onClick={handleEditCancel}>
                    Cancel
                  </Button>
                </Stack>
              </Box>
            ) : isRevealed ? (
              <Box>
                <pre style={{ margin: 0, fontFamily: 'monospace', whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}>
                  {decoder.decode(data[key])}
                </pre>
                <Stack direction="row" spacing={1} sx={{ mt: 1 }}>
                  <Button
                    size="small"
                    startIcon={<VisibilityOffIcon />}
                    onClick={() => toggleReveal(key)}
                  >
                    Hide
                  </Button>
                  <IconButton size="small" aria-label="copy" onClick={() => handleCopy(key)}>
                    <ContentCopyIcon fontSize="small" />
                  </IconButton>
                  <Button size="small" startIcon={<EditIcon />} onClick={() => handleEditStart(key)}>
                    Edit
                  </Button>
                </Stack>
              </Box>
            ) : (
              <Box>
                <Typography variant="body2" sx={{ fontFamily: 'monospace', color: 'text.secondary' }}>
                  ••••••••
                </Typography>
                <Stack direction="row" spacing={1} sx={{ mt: 1 }}>
                  <Button
                    size="small"
                    startIcon={<VisibilityIcon />}
                    onClick={() => toggleReveal(key)}
                  >
                    Reveal
                  </Button>
                  <IconButton size="small" aria-label="copy" onClick={() => handleCopy(key)}>
                    <ContentCopyIcon fontSize="small" />
                  </IconButton>
                  <Button size="small" startIcon={<EditIcon />} onClick={() => handleEditStart(key)}>
                    Edit
                  </Button>
                </Stack>
              </Box>
            )}
          </Box>
        )
      })}
    </Box>
  )
}

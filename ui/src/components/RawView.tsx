import { useMemo } from 'react'
import {
  Box,
  Button,
  FormControlLabel,
  Switch,
  Tooltip,
} from '@mui/material'

// Server-managed metadata fields to strip when includeAllFields is off.
const SERVER_MANAGED_FIELDS = [
  'uid',
  'resourceVersion',
  'generation',
  'creationTimestamp',
  'managedFields',
  'selfLink',
  'deletionTimestamp',
  'deletionGracePeriodSeconds',
]

interface RawViewProps {
  raw: string
  includeAllFields: boolean
  onToggleIncludeAllFields: () => void
}

export function RawView({ raw, includeAllFields, onToggleIncludeAllFields }: RawViewProps) {
  const formattedJson = useMemo(() => {
    const obj = JSON.parse(raw)

    // Convert data (base64) to stringData (plaintext) and remove data field
    // Only applies to Secret objects that have a data field
    if (obj.kind === 'Secret' && obj.data && typeof obj.data === 'object') {
      const stringData: Record<string, string> = {}
      for (const [key, value] of Object.entries(obj.data)) {
        try {
          stringData[key] = atob(value as string)
        } catch {
          stringData[key] = value as string
        }
      }
      obj.stringData = stringData
      delete obj.data
    }

    // Strip server-managed metadata fields when includeAllFields is off
    if (!includeAllFields && obj.metadata) {
      for (const field of SERVER_MANAGED_FIELDS) {
        delete obj.metadata[field]
      }
    }

    return JSON.stringify(obj, null, 2)
  }, [raw, includeAllFields])

  const handleCopy = () => {
    navigator.clipboard.writeText(formattedJson)
  }

  return (
    <Box>
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 2, mb: 1 }}>
        <Tooltip title="Include server-managed fields like uid, resourceVersion, and creationTimestamp. Turn off for clean output suitable for kubectl apply.">
          <FormControlLabel
            control={
              <Switch
                checked={includeAllFields}
                onChange={onToggleIncludeAllFields}
              />
            }
            label="Include all fields"
          />
        </Tooltip>
        <Button variant="outlined" size="small" onClick={handleCopy} aria-label="Copy to Clipboard">
          Copy to Clipboard
        </Button>
      </Box>
      <Box
        component="pre"
        role="code"
        sx={{
          fontFamily: 'monospace',
          fontSize: '0.875rem',
          bgcolor: 'action.hover',
          color: 'text.primary',
          p: 2,
          borderRadius: 1,
          overflow: 'auto',
          whiteSpace: 'pre-wrap',
          wordBreak: 'break-word',
          m: 0,
        }}
      >
        {formattedJson}
      </Box>
    </Box>
  )
}

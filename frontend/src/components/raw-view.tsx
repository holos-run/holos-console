import { useMemo } from 'react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import { Label } from '@/components/ui/label'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'

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
    let obj: Record<string, unknown>
    try {
      obj = JSON.parse(raw)
    } catch {
      return raw
    }

    if (obj.kind === 'Secret' && obj.data && typeof obj.data === 'object') {
      const stringData: Record<string, string> = {}
      for (const [key, value] of Object.entries(obj.data as Record<string, string>)) {
        try {
          stringData[key] = atob(value)
        } catch {
          stringData[key] = value
        }
      }
      obj.stringData = stringData
      delete obj.data
    }

    if (!includeAllFields && obj.metadata && typeof obj.metadata === 'object') {
      for (const field of SERVER_MANAGED_FIELDS) {
        delete (obj.metadata as Record<string, unknown>)[field]
      }
    }

    return JSON.stringify(obj, null, 2)
  }, [raw, includeAllFields])

  const handleCopy = () => {
    navigator.clipboard.writeText(formattedJson)
    toast.success('Copied to clipboard')
  }

  return (
    <div>
      <div className="flex items-center gap-4 mb-2">
        <TooltipProvider>
          <Tooltip>
            <TooltipTrigger asChild>
              <div className="flex items-center gap-2">
                <Switch
                  id="include-all-fields"
                  checked={includeAllFields}
                  onCheckedChange={onToggleIncludeAllFields}
                />
                <Label htmlFor="include-all-fields" className="text-sm">
                  Include all fields
                </Label>
              </div>
            </TooltipTrigger>
            <TooltipContent>
              <p>Include server-managed fields like uid, resourceVersion, and creationTimestamp.</p>
            </TooltipContent>
          </Tooltip>
        </TooltipProvider>
        <Button variant="outline" size="sm" onClick={handleCopy} aria-label="Copy to Clipboard">
          Copy to Clipboard
        </Button>
      </div>
      <pre
        role="code"
        className="rounded-md bg-muted p-4 text-sm font-mono overflow-auto whitespace-pre-wrap break-words"
      >
        {formattedJson}
      </pre>
    </div>
  )
}

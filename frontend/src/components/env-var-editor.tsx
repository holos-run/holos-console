import { X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { useListNamespaceSecrets, useListNamespaceConfigMaps } from '@/queries/deployments'
import type { EnvVar, SecretKeyRef, ConfigMapKeyRef } from '@/gen/holos/console/v1/deployments_pb'

type SourceType = 'value' | 'secret' | 'configmap'

interface EnvVarEditorProps {
  project: string
  value: EnvVar[]
  onChange: (value: EnvVar[]) => void
}

// EnvVarEditor renders a dynamic list of environment variable rows.
// Each row has a name, a source type selector (Value / Secret / ConfigMap),
// and source-specific fields.
export function EnvVarEditor({ project, value, onChange }: EnvVarEditorProps) {
  const { data: secrets = [] } = useListNamespaceSecrets(project)
  const { data: configMaps = [] } = useListNamespaceConfigMaps(project)

  const addRow = () => {
    onChange([...value, { name: '', source: { case: 'value', value: '' } } as unknown as EnvVar])
  }

  const removeRow = (idx: number) => {
    onChange(value.filter((_, i) => i !== idx))
  }

  const updateName = (idx: number, name: string) => {
    onChange(value.map((ev, i) => (i === idx ? ({ ...ev, name } as unknown as EnvVar) : ev)))
  }

  const updateSourceType = (idx: number, sourceType: SourceType) => {
    let source: EnvVar['source']
    if (sourceType === 'value') {
      source = { case: 'value', value: '' }
    } else if (sourceType === 'secret') {
      source = { case: 'secretKeyRef', value: { name: '', key: '' } as unknown as SecretKeyRef }
    } else {
      source = { case: 'configMapKeyRef', value: { name: '', key: '' } as unknown as ConfigMapKeyRef }
    }
    onChange(value.map((ev, i) => (i === idx ? ({ ...ev, source } as unknown as EnvVar) : ev)))
  }

  const updateLiteralValue = (idx: number, val: string) => {
    onChange(
      value.map((ev, i) =>
        i === idx ? ({ ...ev, source: { case: 'value', value: val } } as unknown as EnvVar) : ev,
      ),
    )
  }

  const updateSecretName = (idx: number, name: string) => {
    onChange(
      value.map((ev, i) =>
        i === idx
          ? ({ ...ev, source: { case: 'secretKeyRef', value: { name, key: '' } as unknown as SecretKeyRef } } as unknown as EnvVar)
          : ev,
      ),
    )
  }

  const updateSecretKey = (idx: number, key: string) => {
    const ev = value[idx]
    if (ev.source.case !== 'secretKeyRef') return
    const secretName = (ev.source.value as unknown as { name: string }).name
    onChange(
      value.map((e, i) =>
        i === idx
          ? ({ ...e, source: { case: 'secretKeyRef', value: { name: secretName, key } as unknown as SecretKeyRef } } as unknown as EnvVar)
          : e,
      ),
    )
  }

  const updateConfigMapName = (idx: number, name: string) => {
    onChange(
      value.map((ev, i) =>
        i === idx
          ? ({ ...ev, source: { case: 'configMapKeyRef', value: { name, key: '' } as unknown as ConfigMapKeyRef } } as unknown as EnvVar)
          : ev,
      ),
    )
  }

  const updateConfigMapKey = (idx: number, key: string) => {
    const ev = value[idx]
    if (ev.source.case !== 'configMapKeyRef') return
    const cmName = (ev.source.value as unknown as { name: string }).name
    onChange(
      value.map((e, i) =>
        i === idx
          ? ({ ...e, source: { case: 'configMapKeyRef', value: { name: cmName, key } as unknown as ConfigMapKeyRef } } as unknown as EnvVar)
          : e,
      ),
    )
  }

  const sourceTypeOf = (ev: EnvVar): SourceType => {
    if (ev.source.case === 'secretKeyRef') return 'secret'
    if (ev.source.case === 'configMapKeyRef') return 'configmap'
    return 'value'
  }

  return (
    <div className="space-y-2">
      {value.map((ev, idx) => {
        const sourceType = sourceTypeOf(ev)
        return (
          <div key={idx} className="flex flex-col gap-1.5 rounded-md border p-2">
            <div className="flex items-center gap-2">
              <Input
                className="h-8 text-sm font-mono flex-1"
                value={ev.name}
                onChange={(e) => updateName(idx, e.target.value)}
                placeholder="ENV_VAR_NAME"
                aria-label="env var name"
              />
              <Select value={sourceType} onValueChange={(v) => updateSourceType(idx, v as SourceType)}>
                <SelectTrigger className="w-32 h-8 text-sm" aria-label="source type">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="value">Value</SelectItem>
                  <SelectItem value="secret">Secret</SelectItem>
                  <SelectItem value="configmap">ConfigMap</SelectItem>
                </SelectContent>
              </Select>
              <Button
                type="button"
                variant="ghost"
                size="icon"
                className="h-8 w-8 shrink-0"
                onClick={() => removeRow(idx)}
                aria-label="remove env var"
              >
                <X className="h-4 w-4" />
              </Button>
            </div>

            {sourceType === 'value' && (
              <Input
                className="h-8 text-sm font-mono"
                value={ev.source.case === 'value' ? ev.source.value : ''}
                onChange={(e) => updateLiteralValue(idx, e.target.value)}
                placeholder="literal value"
                aria-label="literal value"
              />
            )}

            {sourceType === 'secret' && (
              <div className="flex gap-2">
                <Select
                  value={ev.source.case === 'secretKeyRef' ? ev.source.value.name : ''}
                  onValueChange={(v) => updateSecretName(idx, v)}
                >
                  <SelectTrigger className="h-8 text-sm flex-1" aria-label="secret name">
                    <SelectValue placeholder="Select secret..." />
                  </SelectTrigger>
                  <SelectContent>
                    {secrets.map((s) => (
                      <SelectItem key={s.name} value={s.name}>{s.name}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <Select
                  value={ev.source.case === 'secretKeyRef' ? ev.source.value.key : ''}
                  onValueChange={(v) => updateSecretKey(idx, v)}
                >
                  <SelectTrigger className="h-8 text-sm flex-1" aria-label="secret key">
                    <SelectValue placeholder="Select key..." />
                  </SelectTrigger>
                  <SelectContent>
                    {ev.source.case === 'secretKeyRef' && (() => {
                        const selectedSecretName = (ev.source.value as unknown as { name: string }).name
                        return (secrets.find((s) => s.name === selectedSecretName)?.keys ?? []).map((k) => (
                          <SelectItem key={k} value={k}>{k}</SelectItem>
                        ))
                      })()}
                  </SelectContent>
                </Select>
              </div>
            )}

            {sourceType === 'configmap' && (
              <div className="flex gap-2">
                <Select
                  value={ev.source.case === 'configMapKeyRef' ? ev.source.value.name : ''}
                  onValueChange={(v) => updateConfigMapName(idx, v)}
                >
                  <SelectTrigger className="h-8 text-sm flex-1" aria-label="configmap name">
                    <SelectValue placeholder="Select ConfigMap..." />
                  </SelectTrigger>
                  <SelectContent>
                    {configMaps.map((cm) => (
                      <SelectItem key={cm.name} value={cm.name}>{cm.name}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <Select
                  value={ev.source.case === 'configMapKeyRef' ? ev.source.value.key : ''}
                  onValueChange={(v) => updateConfigMapKey(idx, v)}
                >
                  <SelectTrigger className="h-8 text-sm flex-1" aria-label="configmap key">
                    <SelectValue placeholder="Select key..." />
                  </SelectTrigger>
                  <SelectContent>
                    {ev.source.case === 'configMapKeyRef' && (() => {
                        const selectedCmName = (ev.source.value as unknown as { name: string }).name
                        return (configMaps.find((cm) => cm.name === selectedCmName)?.keys ?? []).map((k) => (
                          <SelectItem key={k} value={k}>{k}</SelectItem>
                        ))
                      })()}
                  </SelectContent>
                </Select>
              </div>
            )}
          </div>
        )
      })}

      <Button type="button" variant="outline" size="sm" onClick={addRow}>
        Add environment variable
      </Button>
    </div>
  )
}

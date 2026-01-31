import { render, screen, fireEvent } from '@testing-library/react'
import { SecretRawView } from './SecretRawView'
import { vi } from 'vitest'

// Sample raw JSON as returned by GetSecretRaw
const sampleRaw = JSON.stringify({
  apiVersion: 'v1',
  kind: 'Secret',
  metadata: {
    name: 'my-secret',
    namespace: 'default',
    uid: 'abc-123',
    resourceVersion: '12345',
    creationTimestamp: '2025-01-01T00:00:00Z',
    managedFields: [{ manager: 'kubectl' }],
    labels: {
      'app.kubernetes.io/managed-by': 'console.holos.run',
    },
    annotations: {
      'console.holos.run/share-users': '[]',
    },
  },
  data: {
    username: btoa('admin'),
    password: btoa('secret123'),
  },
  type: 'Opaque',
})

describe('SecretRawView', () => {
  describe('data conversion', () => {
    it('converts data (base64) to stringData (plaintext) and removes data field', () => {
      render(<SecretRawView raw={sampleRaw} includeAllFields={false} onToggleIncludeAllFields={vi.fn()} />)

      const pre = screen.getByRole('code')
      const parsed = JSON.parse(pre.textContent || '')

      expect(parsed.stringData).toBeDefined()
      expect(parsed.stringData.username).toBe('admin')
      expect(parsed.stringData.password).toBe('secret123')
      expect(parsed.data).toBeUndefined()
    })
  })

  describe('formatting', () => {
    it('pretty-prints JSON with 2-space indentation', () => {
      render(<SecretRawView raw={sampleRaw} includeAllFields={true} onToggleIncludeAllFields={vi.fn()} />)

      const pre = screen.getByRole('code')
      const text = pre.textContent || ''
      // Verify it's valid JSON and re-stringify matches 2-space formatting
      const parsed = JSON.parse(text)
      const expected = JSON.stringify(parsed, null, 2)
      expect(text).toBe(expected)
    })
  })

  describe('field filtering', () => {
    it('strips server-managed metadata fields when includeAllFields is off', () => {
      render(<SecretRawView raw={sampleRaw} includeAllFields={false} onToggleIncludeAllFields={vi.fn()} />)

      const pre = screen.getByRole('code')
      const parsed = JSON.parse(pre.textContent || '')

      // Server-managed fields should be stripped
      expect(parsed.metadata.uid).toBeUndefined()
      expect(parsed.metadata.resourceVersion).toBeUndefined()
      expect(parsed.metadata.creationTimestamp).toBeUndefined()
      expect(parsed.metadata.managedFields).toBeUndefined()

      // Always-keep fields should be present
      expect(parsed.apiVersion).toBe('v1')
      expect(parsed.kind).toBe('Secret')
      expect(parsed.metadata.name).toBe('my-secret')
      expect(parsed.metadata.namespace).toBe('default')
      expect(parsed.metadata.labels).toBeDefined()
      expect(parsed.metadata.annotations).toBeDefined()
      expect(parsed.type).toBe('Opaque')
    })

    it('preserves all fields when includeAllFields is on', () => {
      render(<SecretRawView raw={sampleRaw} includeAllFields={true} onToggleIncludeAllFields={vi.fn()} />)

      const pre = screen.getByRole('code')
      const parsed = JSON.parse(pre.textContent || '')

      expect(parsed.metadata.uid).toBe('abc-123')
      expect(parsed.metadata.resourceVersion).toBe('12345')
      expect(parsed.metadata.creationTimestamp).toBe('2025-01-01T00:00:00Z')
      expect(parsed.metadata.managedFields).toBeDefined()
    })
  })

  describe('toggle', () => {
    it('has an Include all fields toggle with tooltip', () => {
      render(<SecretRawView raw={sampleRaw} includeAllFields={false} onToggleIncludeAllFields={vi.fn()} />)

      expect(screen.getByText('Include all fields')).toBeInTheDocument()
      expect(screen.getByRole('switch')).toBeInTheDocument()
    })

    it('calls onToggleIncludeAllFields when toggle is clicked', () => {
      const onToggle = vi.fn()
      render(<SecretRawView raw={sampleRaw} includeAllFields={false} onToggleIncludeAllFields={onToggle} />)

      const toggle = screen.getByRole('switch')
      fireEvent.click(toggle)
      expect(onToggle).toHaveBeenCalled()
    })
  })

  describe('copy to clipboard', () => {
    it('has a Copy to Clipboard button', () => {
      render(<SecretRawView raw={sampleRaw} includeAllFields={false} onToggleIncludeAllFields={vi.fn()} />)

      const button = screen.getByRole('button', { name: /copy to clipboard/i })
      expect(button).toBeInTheDocument()
    })
  })
})

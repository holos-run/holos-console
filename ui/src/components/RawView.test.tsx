import { render, screen, fireEvent } from '@testing-library/react'
import { RawView } from './RawView'
import { vi } from 'vitest'

// Sample Namespace JSON (no data field)
const namespaceRaw = JSON.stringify({
  apiVersion: 'v1',
  kind: 'Namespace',
  metadata: {
    name: 'org-acme',
    uid: 'ns-uid-123',
    resourceVersion: '99999',
    creationTimestamp: '2025-06-01T00:00:00Z',
    managedFields: [{ manager: 'holos-console' }],
    labels: {
      'app.kubernetes.io/managed-by': 'console.holos.run',
      'console.holos.run/resource-type': 'organization',
    },
    annotations: {
      'console.holos.run/share-users': '[]',
    },
  },
  spec: {
    finalizers: ['kubernetes'],
  },
  status: {
    phase: 'Active',
  },
})

// Sample Secret JSON (has data field)
const secretRaw = JSON.stringify({
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

describe('RawView', () => {
  describe('with Namespace (no data field)', () => {
    it('pretty-prints JSON without errors', () => {
      render(<RawView raw={namespaceRaw} includeAllFields={false} onToggleIncludeAllFields={vi.fn()} />)

      const pre = screen.getByRole('code')
      const parsed = JSON.parse(pre.textContent || '')

      expect(parsed.apiVersion).toBe('v1')
      expect(parsed.kind).toBe('Namespace')
      expect(parsed.metadata.name).toBe('org-acme')
    })

    it('strips server-managed metadata fields when includeAllFields is off', () => {
      render(<RawView raw={namespaceRaw} includeAllFields={false} onToggleIncludeAllFields={vi.fn()} />)

      const pre = screen.getByRole('code')
      const parsed = JSON.parse(pre.textContent || '')

      expect(parsed.metadata.uid).toBeUndefined()
      expect(parsed.metadata.resourceVersion).toBeUndefined()
      expect(parsed.metadata.creationTimestamp).toBeUndefined()
      expect(parsed.metadata.managedFields).toBeUndefined()

      // Non-managed fields preserved
      expect(parsed.metadata.labels).toBeDefined()
      expect(parsed.metadata.annotations).toBeDefined()
    })

    it('preserves all fields when includeAllFields is on', () => {
      render(<RawView raw={namespaceRaw} includeAllFields={true} onToggleIncludeAllFields={vi.fn()} />)

      const pre = screen.getByRole('code')
      const parsed = JSON.parse(pre.textContent || '')

      expect(parsed.metadata.uid).toBe('ns-uid-123')
      expect(parsed.metadata.resourceVersion).toBe('99999')
      expect(parsed.metadata.creationTimestamp).toBe('2025-06-01T00:00:00Z')
      expect(parsed.metadata.managedFields).toBeDefined()
    })

    it('has a Copy to Clipboard button', () => {
      render(<RawView raw={namespaceRaw} includeAllFields={false} onToggleIncludeAllFields={vi.fn()} />)

      const button = screen.getByRole('button', { name: /copy to clipboard/i })
      expect(button).toBeInTheDocument()
    })

    it('does not create a stringData field', () => {
      render(<RawView raw={namespaceRaw} includeAllFields={true} onToggleIncludeAllFields={vi.fn()} />)

      const pre = screen.getByRole('code')
      const parsed = JSON.parse(pre.textContent || '')
      expect(parsed.stringData).toBeUndefined()
      expect(parsed.data).toBeUndefined()
    })
  })

  describe('with Secret (has data field)', () => {
    it('converts data (base64) to stringData (plaintext) and removes data field', () => {
      render(<RawView raw={secretRaw} includeAllFields={false} onToggleIncludeAllFields={vi.fn()} />)

      const pre = screen.getByRole('code')
      const parsed = JSON.parse(pre.textContent || '')

      expect(parsed.stringData).toBeDefined()
      expect(parsed.stringData.username).toBe('admin')
      expect(parsed.stringData.password).toBe('secret123')
      expect(parsed.data).toBeUndefined()
    })
  })

  describe('theme-aware styling', () => {
    it('does not use hardcoded #f5f5f5 background color', () => {
      render(<RawView raw={namespaceRaw} includeAllFields={false} onToggleIncludeAllFields={vi.fn()} />)

      const pre = screen.getByRole('code')
      expect(pre.style.backgroundColor).not.toBe('#f5f5f5')
      expect(pre.style.backgroundColor).not.toBe('rgb(245, 245, 245)')
    })
  })

  describe('toggle', () => {
    it('calls onToggleIncludeAllFields when toggle is clicked', () => {
      const onToggle = vi.fn()
      render(<RawView raw={namespaceRaw} includeAllFields={false} onToggleIncludeAllFields={onToggle} />)

      const toggle = screen.getByRole('switch')
      fireEvent.click(toggle)
      expect(onToggle).toHaveBeenCalled()
    })
  })
})

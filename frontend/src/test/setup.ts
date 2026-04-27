import '@testing-library/jest-dom/vitest'
import { vi } from 'vitest'

vi.mock('@/queries/permissions', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/queries/permissions')>()
  return {
    ...actual,
    useResourcePermissions: vi.fn((attributes) => {
      const data: import('@/queries/permissions').PermissionsMap = {}
      for (const attr of attributes) {
        const key = actual.permissionKey(attr)
        data[key] = {
          $typeName: 'holos.console.v1.ResourcePermission',
          attributes: undefined as never,
          allowed: true,
          denied: false,
          reason: '',
          key,
        }
      }
      return {
        data,
        isPending: false,
        error: null,
      }
    }),
  }
})

// Polyfill ResizeObserver for jsdom (used by cmdk/Combobox)
if (!globalThis.ResizeObserver) {
  globalThis.ResizeObserver = class ResizeObserver {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
}

// Polyfill scrollIntoView for jsdom (used by cmdk)
if (!Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = () => {}
}

if (!window.matchMedia) {
  window.matchMedia = (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  })
}

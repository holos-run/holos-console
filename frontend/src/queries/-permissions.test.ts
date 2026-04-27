import { describe, it, expect } from 'vitest'

import { permissionKey, isAllowed, type PermissionsMap } from './permissions'

describe('permissionKey', () => {
  it('handles core resources without a group', () => {
    expect(permissionKey({ verb: 'list', resource: 'namespaces' })).toBe(
      'list:namespaces',
    )
  })

  it('joins group/resource for namespaced calls', () => {
    expect(
      permissionKey({
        verb: 'get',
        group: 'templates.holos.run',
        resource: 'templates',
        namespace: 'ns',
      }),
    ).toBe('get:templates.holos.run/templates:ns')
  })

  it('appends subresource and name when present', () => {
    expect(
      permissionKey({
        verb: 'update',
        group: 'apps',
        resource: 'deployments',
        subresource: 'status',
        namespace: 'ns',
        name: 'demo',
      }),
    ).toBe('update:apps/deployments/status:ns:demo')
  })
})

describe('isAllowed', () => {
  it('returns false when the perms map is undefined', () => {
    expect(isAllowed(undefined, 'list:namespaces')).toBe(false)
  })

  it('returns false when the key is not present', () => {
    const map: PermissionsMap = {}
    expect(isAllowed(map, 'list:namespaces')).toBe(false)
  })

  it('reflects the allowed flag from the map entry', () => {
    const map: PermissionsMap = {
      'list:namespaces': {
        $typeName: 'holos.console.v1.ResourcePermission',
        attributes: undefined as never,
        allowed: true,
        denied: false,
        reason: '',
        key: 'list:namespaces',
      },
      'get:secrets:ns:s1': {
        $typeName: 'holos.console.v1.ResourcePermission',
        attributes: undefined as never,
        allowed: false,
        denied: true,
        reason: 'no rolebinding',
        key: 'get:secrets:ns:s1',
      },
    }
    expect(isAllowed(map, 'list:namespaces')).toBe(true)
    expect(isAllowed(map, 'get:secrets:ns:s1')).toBe(false)
  })
})

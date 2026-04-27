import { vi } from 'vitest'

import { Role } from '@/gen/holos/console/v1/rbac_pb.js'
import {
  permissionKey,
  type PermissionsMap,
  type ResourcePermissionInput,
  useResourcePermissions,
} from '@/queries/permissions'

function makePermissionsMap(
  attributes: ResourcePermissionInput[],
  isAllowed: (attribute: ResourcePermissionInput) => boolean,
): PermissionsMap {
  const data: PermissionsMap = {}
  for (const attr of attributes) {
    const key = permissionKey(attr)
    const allowed = isAllowed(attr)
    data[key] = {
      $typeName: 'holos.console.v1.ResourcePermission',
      attributes: undefined as never,
      allowed,
      denied: !allowed,
      reason: allowed ? '' : 'denied by test fixture',
      key,
    }
  }
  return data
}

export function mockResourcePermissions(
  isAllowed: (attribute: ResourcePermissionInput) => boolean,
) {
  vi.mocked(useResourcePermissions).mockImplementation(
    (attributes) =>
      ({
        data: makePermissionsMap(attributes, isAllowed),
        isPending: false,
        error: null,
      }) as ReturnType<typeof useResourcePermissions>,
  )
}

export function mockResourcePermissionsForRole(role: Role | number) {
  mockResourcePermissions((attr) => isResourcePermissionAllowedForRole(role, attr))
}

export function isResourcePermissionAllowedForRole(
  role: Role | number,
  attr: ResourcePermissionInput,
) {
  if (role === Role.OWNER) return true
  if (role === Role.EDITOR) return attr.verb !== 'delete'
  return attr.verb === 'get' || attr.verb === 'list' || attr.verb === 'watch'
}

export function mockAllResourcePermissionsAllowed() {
  mockResourcePermissions(() => true)
}

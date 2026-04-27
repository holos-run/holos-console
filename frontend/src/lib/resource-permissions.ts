import {
  isAllowed,
  permissionKey,
  type PermissionsMap,
  type ResourcePermissionInput,
} from '@/queries/permissions'

export const TEMPLATES_API_GROUP = 'templates.holos.run'
export const DEPLOYMENTS_API_GROUP = 'deployments.holos.run'

export const templateResources = {
  templates: 'templates',
  templatePolicies: 'templatepolicies',
  templatePolicyBindings: 'templatepolicybindings',
  templateGrants: 'templategrants',
  templateDependencies: 'templatedependencies',
  templateRequirements: 'templaterequirements',
} as const

export function createTemplateResourcePermission(
  resource: string,
  namespace: string,
): ResourcePermissionInput {
  return {
    verb: 'create',
    group: TEMPLATES_API_GROUP,
    resource,
    namespace,
  }
}

export function updateTemplateResourcePermission(
  resource: string,
  namespace: string,
  name: string,
): ResourcePermissionInput {
  return {
    verb: 'update',
    group: TEMPLATES_API_GROUP,
    resource,
    namespace,
    name,
  }
}

export function deleteTemplateResourcePermission(
  resource: string,
  namespace: string,
  name: string,
): ResourcePermissionInput {
  return {
    verb: 'delete',
    group: TEMPLATES_API_GROUP,
    resource,
    namespace,
    name,
  }
}

export function createNamespacePermission(): ResourcePermissionInput {
  return {
    verb: 'create',
    resource: 'namespaces',
  }
}

export function createNamespacedResourcePermission(
  group: string,
  resource: string,
  namespace: string,
): ResourcePermissionInput {
  return {
    verb: 'create',
    group,
    resource,
    namespace,
  }
}

export function updateNamespacedResourcePermission(
  group: string,
  resource: string,
  namespace: string,
  name: string,
): ResourcePermissionInput {
  return {
    verb: 'update',
    group,
    resource,
    namespace,
    name,
  }
}

export function deleteNamespacedResourcePermission(
  group: string,
  resource: string,
  namespace: string,
  name: string,
): ResourcePermissionInput {
  return {
    verb: 'delete',
    group,
    resource,
    namespace,
    name,
  }
}

export function updateNamespacePermission(name: string): ResourcePermissionInput {
  return {
    verb: 'update',
    resource: 'namespaces',
    name,
  }
}

export function deleteNamespacePermission(name: string): ResourcePermissionInput {
  return {
    verb: 'delete',
    resource: 'namespaces',
    name,
  }
}

export function hasPermission(
  permissions: PermissionsMap | undefined,
  attr: ResourcePermissionInput,
): boolean {
  return isAllowed(permissions, permissionKey(attr))
}

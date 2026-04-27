// permissions.ts — TanStack Query hook for the bulk SelfSubjectAccessReview
// endpoint exposed by PermissionsService.ListResourcePermissions.
//
// Components that previously hid action buttons from the wrong persona via
// claim-based logic should call useResourcePermissions(attributes) instead.
// The hook returns a map keyed by the deterministic permission key
// "verb:group/resource[:namespace[:name]]" so a button can look up its own
// allowed status in O(1).
//
// Per ADR 036 the API server is the single arbiter of access; this hook is
// purely advisory. Surfaces that prefer the optimistic+toast pattern can
// keep buttons in place and translate connect.CodePermissionDenied to the
// access-denied toast at click time. The two patterns compose: a button that
// is hidden today via this hook will still get the toast tomorrow if the
// underlying RoleBinding is removed between hook resolution and click.

import { useMemo } from 'react'
import { createClient } from '@connectrpc/connect'
import { useTransport } from '@connectrpc/connect-query'
import { useQuery } from '@tanstack/react-query'

import {
  PermissionsService,
  type ResourceAttributes,
  type ResourcePermission,
} from '@/gen/holos/console/v1/permissions_pb.js'
import { useAuth } from '@/lib/auth'
import { keys } from '@/queries/keys'

// ResourcePermissionInput accepts the same field set as the proto's
// ResourceAttributes message but without forcing callers to import the
// proto type. The hook converts inputs to the wire shape internally.
export interface ResourcePermissionInput {
  verb: string
  group?: string
  resource: string
  subresource?: string
  namespace?: string
  name?: string
}

// permissionKey returns the deterministic key the backend uses to index
// every ResourcePermission. Mirrors console/permissions.PermissionKey on
// the Go side so a frontend component can look up its decision without
// reading the response array order.
export function permissionKey(attr: ResourcePermissionInput): string {
  const groupResource =
    attr.group && attr.group !== ''
      ? `${attr.group}/${attr.resource}`
      : attr.resource
  const withSub =
    attr.subresource && attr.subresource !== ''
      ? `${groupResource}/${attr.subresource}`
      : groupResource
  const parts = [attr.verb, withSub]
  if (attr.namespace) parts.push(attr.namespace)
  if (attr.name) parts.push(attr.name)
  return parts.join(':')
}

// PermissionsMap is the shape returned by useResourcePermissions's `data`
// selector: a map from permissionKey() to its ResourcePermission decision.
export type PermissionsMap = Record<string, ResourcePermission>

// useResourcePermissions fans out one SelfSubjectAccessReview per attribute
// in `attributes` against the impersonating client and returns a map keyed
// by the deterministic permission key. The query is disabled until the user
// is authenticated and the attributes list is non-empty.
export function useResourcePermissions(attributes: ResourcePermissionInput[]) {
  const { isAuthenticated } = useAuth()
  const permissionKeys = useMemo(
    () => attributes.map(permissionKey),
    [attributes],
  )
  const transport = useTransport()
  const client = useMemo(
    () => createClient(PermissionsService, transport),
    [transport],
  )
  return useQuery({
    queryKey: keys.permissions.list(permissionKeys),
    queryFn: async (): Promise<PermissionsMap> => {
      const response = await client.listResourcePermissions({
        attributes: attributes.map(
          (a): ResourceAttributes => ({
            $typeName: 'holos.console.v1.ResourceAttributes',
            verb: a.verb,
            group: a.group ?? '',
            resource: a.resource,
            subresource: a.subresource ?? '',
            namespace: a.namespace ?? '',
            name: a.name ?? '',
          }),
        ),
      })
      const map: PermissionsMap = {}
      for (const p of response.permissions) {
        map[p.key] = p
      }
      return map
    },
    enabled: isAuthenticated && attributes.length > 0,
    // Permission decisions change rarely; cache for a minute so the UI
    // does not refetch on every re-render of a parent component.
    staleTime: 60_000,
  })
}

// isAllowed is a small helper that returns true when the given permission
// key was both resolved and allowed by the backend. Use this in components
// to gate render of an action button:
//
//   const { data: perms } = useResourcePermissions([{ verb: 'create', ... }])
//   const allowed = isAllowed(perms, permissionKey({ verb: 'create', ... }))
//
export function isAllowed(
  perms: PermissionsMap | undefined,
  key: string,
): boolean {
  return !!perms?.[key]?.allowed
}

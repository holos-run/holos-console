import { Role } from '@/gen/holos/console/v1/rbac_pb'

// isOwner returns true if the user has the owner role on a secret.
// It checks both direct user grants (by email) and role grants (by group membership).
export function isOwner(
  email: string | undefined,
  groups: string[],
  userGrants: { principal: string; role: Role }[],
  roleGrants: { principal: string; role: Role }[],
): boolean {
  if (email != null && userGrants.some((g) => g.principal === email && g.role === Role.OWNER)) {
    return true
  }
  return roleGrants.some((g) => groups.includes(g.principal) && g.role === Role.OWNER)
}

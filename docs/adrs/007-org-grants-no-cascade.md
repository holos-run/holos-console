# ADR 007: Organization Grants Do Not Cascade

## Status

Accepted

## Context

The three-tier RBAC model (organization, project, secret) uses annotation-based grants on Kubernetes resources. The original implementation included empty cascade tables (`OrgCascadeSecretPerms`, `OrgCascadeProjectPerms`) and code paths that checked org grants via `CheckCascadeAccess` for project and secret operations. Because the cascade tables were empty, these code paths always denied access — making them dead code that created false expectations about cascade behavior.

The projects handler also checked org grants directly via `CheckAccessGrants` for `CreateProject`, which is a separate authorization path (not cascade). This direct check remains.

## Decision

Organization-level grants authorize only organization-level operations: viewing the org, managing IAM bindings on the org, and granting project creation permission. They do not cascade to project or secret operations.

The dead cascade code paths and empty cascade tables have been removed. The secrets handler no longer accepts an `OrgResolver` — org grants are architecturally unable to reach secret access checks.

## Rationale

- **Principle of least privilege**: Org-level access is for IAM administration, not resource access. A user with OWNER on an org can manage who has access to the org but cannot read secrets in projects under that org solely through the org grant.
- **Safe default**: Empty cascade tables were already deny-by-default. Removing the dead code makes this policy explicit rather than implicit.
- **Clarity**: Code paths that always deny are confusing. Removing them eliminates the risk of someone assuming cascade is active and introducing a bug by populating the tables without understanding the security implications.

## Consequences

- Users who need access to projects or secrets must be granted explicit project-level or secret-level grants. Org grants cannot be used as a shortcut to grant broad access to all child resources.
- If cascade from org to project/secret is desired in the future, it must be deliberately designed and implemented with new cascade tables and security review.
- The `bestRoleWithOrg` function in the projects handler still considers org grants when computing the user's effective role for display purposes. This does not affect authorization.

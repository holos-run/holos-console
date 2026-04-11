# Permission Design Guidelines

This document covers the principles for designing and implementing permissions in the console.

## Narrow Scoping

Each permission should control a single capability. Avoid bundling unrelated actions into one permission.

**Good**: `PERMISSION_PROJECT_DEPLOYMENTS_ENABLE` controls only the ability to toggle the deployments feature flag on a project.

**Good**: `PERMISSION_REPARENT` controls only the ability to move a folder or project to a different parent. It is separate from WRITE because reparenting changes RBAC inheritance chains — an EDITOR who can modify folder metadata should not be able to move a subtree into a scope where they gain elevated permissions (ADR 022 Decision 6).

**Bad**: `PERMISSION_PROJECT_SETTINGS_WRITE` covering both deployments toggle and future settings (too broad to delegate independently).

When a new action could reasonably be granted independently of existing permissions, create a dedicated permission rather than reusing an existing one.

## Multi-Level Grantability

Permissions must be designed so they can be granted at multiple levels:

1. **Organization level** -- via org role bindings in the cascade table
2. **Project level** -- via project role bindings in a project cascade table
3. **Resource level** -- via direct grants on the resource

The cascade table pattern makes this possible without code changes at each level.

## Cascade Table Pattern

A `CascadeTable` maps roles at a parent scope to permissions at a child scope. This makes cascade policy explicit and readable at a glance.

```go
var OrgCascadeProjectSettingsPerms = rbac.CascadeTable{
    rbac.RoleOwner: {
        rbac.PermissionProjectDeploymentsEnable: true,
    },
}
```

A second cascade table controls template access at all scope levels (org, folder, project):

```go
var TemplateCascadePerms = rbac.CascadeTable{
    rbac.RoleViewer: {
        rbac.PermissionTemplatesList: true,
        rbac.PermissionTemplatesRead: true,
    },
    rbac.RoleEditor: {
        rbac.PermissionTemplatesList:  true,
        rbac.PermissionTemplatesRead:  true,
        rbac.PermissionTemplatesWrite: true,
    },
    rbac.RoleOwner: {
        rbac.PermissionTemplatesList:   true,
        rbac.PermissionTemplatesRead:   true,
        rbac.PermissionTemplatesWrite:  true,
        rbac.PermissionTemplatesDelete: true,
        rbac.PermissionTemplatesAdmin:  true,
    },
}
```

`PERMISSION_TEMPLATES_WRITE` grants the ability to create, update, and delete templates. The
same table (`TemplateCascadePerms`) applies uniformly at every scope level (ADR 021 Decision 2)
— a VIEWER can read, an EDITOR can write, and an OWNER has full control regardless of whether
the template is org-scoped, folder-scoped, or project-scoped.

A third cascade table controls reparenting access at organization and folder scope:

```go
var ReparentCascadePerms = rbac.CascadeTable{
    rbac.RoleOwner: {
        rbac.PermissionReparent: true,
    },
}
```

`PERMISSION_REPARENT` is granted only to OWNERs. The caller must hold this permission on both the
source parent and the destination parent to move a folder or project. This is deliberately more
restrictive than WRITE because reparenting changes RBAC inheritance chains (ADR 022 Decision 6).

To check access: resolve the user's best role from grants at the parent scope, then look up permissions in the cascade table.

```go
rbac.CheckCascadeAccess(email, roles, orgUsers, orgRoles, permission, table)
```

## Naming Convention

Permissions follow the pattern: `PERMISSION_{SCOPE}_{RESOURCE}_{ACTION}`

Examples:
- `PERMISSION_SECRETS_READ` -- scope: implicit (project), resource: secrets, action: read
- `PERMISSION_PROJECT_DEPLOYMENTS_ENABLE` -- scope: project, resource: deployments, action: enable
- `PERMISSION_ORGANIZATIONS_ADMIN` -- scope: organizations, resource: (self), action: admin

## When to Add a New Permission

Add a new permission when:
- The capability could reasonably be granted independently of other capabilities in an existing permission
- Different roles at different levels should be able to grant the capability
- The action is distinct enough that bundling it would create unintended side effects

Reuse an existing permission when:
- The actions are always granted together and there is no foreseeable need to separate them

## Role Hierarchy

Roles are ordered: `VIEWER (1) < EDITOR (2) < OWNER (3)`.

Higher roles inherit all permissions of lower roles in the direct permission table (`rolePermissions`). Cascade tables are independent -- each role explicitly lists the permissions it grants at the child scope.

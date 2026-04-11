# Guardrail: Resource Naming

**Slug-based identifiers with collision suffix.** Folder and project identifiers are derived from the display name by slugifying it (lowercase, hyphens for spaces, strip non-alphanumeric). If the resulting namespace name is already taken globally, the server appends `-NNNNNN` (six random digits) to ensure uniqueness. This follows the Google Cloud project naming model (ADR 022 Decisions 1-2).

| Resource     | Prefix       | Identifier source      | Example (no collision)      | Example (collision)              |
|--------------|--------------|------------------------|-----------------------------|----------------------------------|
| Organization | `holos-org-` | user-chosen slug       | `holos-org-acme`            | n/a (must be unique)             |
| Folder       | `holos-fld-` | slug from display name | `holos-fld-default`         | `holos-fld-default-482917`       |
| Project      | `holos-prj-` | slug from display name | `holos-prj-frontend`        | `holos-prj-frontend-731204`      |

**Do NOT use random-only numeric identifiers** for folders or projects. The identifier must always start with the slug derived from the display name. A six-digit random suffix is only appended when the slug is already taken.

## CheckIdentifier RPCs

`FolderService.CheckFolderIdentifier` and `ProjectService.CheckProjectIdentifier` let callers check availability before creation and get a server-suggested alternative if the slug is taken. The server generates suggestions to normalize behavior across all callers.

**Triggers**: Apply this rule when writing or editing any code that creates folders or projects, any proto definitions for folder/project creation, or any documentation that describes the namespace naming scheme.

## Related

- [RBAC](rbac.md) — Namespace prefix scheme used by the access control model
- [Package Structure](package-structure.md) — `console/resolver/` handles namespace translation

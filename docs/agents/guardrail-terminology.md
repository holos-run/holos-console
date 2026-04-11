# Guardrail: Terminology

**Authoritative reference**: `docs/glossary.md`

**Rule**: In documentation, Go comments, and TypeScript comments, use "platform template" (not "system template") when referring to the concept of an organization-level or folder-level CUE template. The unified service is `TemplateService` in `console/templates/` (proto: `templates.proto`). Scope-specific concepts may use `org-scoped`, `folder-scoped`, or `project-scoped` as adjectives.

**Triggers**: Apply this rule when writing or editing any `.md` file, Go comment, or TypeScript comment that mentions templates at the organization or folder level.

## Examples

| Context | Correct | Incorrect |
|---------|---------|-----------|
| Prose / docs | "Create a platform template to enforce labels..." | "Create a system template..." |
| Code identifier | `TemplateService.CreateTemplate` | `OrgTemplateService.CreateOrgTemplate` |
| Mixed reference | "`TemplateService` (platform template) controls..." | "system template controls..." |

## Related

- [Template Service](template-service.md) — The unified TemplateService

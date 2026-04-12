# Guardrail: Template Linking

**When adding new fields that affect which platform templates are included in a render**, ensure:

1. The linking list is read from the `console.holos.run/linked-templates` annotation on the deployment template ConfigMap (JSON array of `{scope, scope_name, name}` objects — v1alpha2 format).
2. `OrgTemplateProvider.ListOrgTemplateSourcesForRender(ctx, org, linkedNames)` is called with the resolved linked names (not `ListEnabledOrgTemplateSources` — that method no longer exists).
3. `docs/cue-template-guide.md` "Linking Platform Templates" section and the render set formula remain accurate.

When adding new fields that affect template linking, update `docs/cue-template-guide.md` and the AGENTS.md context map.

## Scoped Link Permissions

Modifying linked template references requires scoped link permissions in addition to `PERMISSION_TEMPLATES_WRITE`:

- `PERMISSION_TEMPLATES_LINK_ORG_WRITE` — required when any linked ref targets an organization-scope template.
- `PERMISSION_TEMPLATES_LINK_FOLDER_WRITE` — required when any linked ref targets a folder-scope template.

Both permissions are checked at the template's owning scope (not the linked template's scope). Only OWNERs have these permissions via the `TemplateCascadePerms` cascade table. EDITORs can create and update templates but cannot modify linked template references.

The `update_linked_templates` flag on `UpdateTemplateRequest` controls whether the backend processes the `linked_templates` field. When false (default), existing links are preserved regardless of the request payload. When true, the handler enforces scoped link permissions against both the existing linked refs (being removed) and the new linked refs (being added), then replaces the stored links.

## Related

- [Template Service](template-service.md) — Explicit linking model and render set formula
- [Guardrail: Template Fields](guardrail-template-fields.md) — New fields must be added across the pipeline
- [Guardrail: Template Docs](guardrail-template-docs.md) — Keep cue-template-guide.md current

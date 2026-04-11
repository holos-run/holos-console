# Guardrail: Template Linking

**When adding new fields that affect which platform templates are included in a render**, ensure:

1. The linking list is read from the `console.holos.run/linked-templates` annotation on the deployment template ConfigMap (JSON array of `{scope, scope_name, name}` objects — v1alpha2 format).
2. `OrgTemplateProvider.ListOrgTemplateSourcesForRender(ctx, org, linkedNames)` is called with the resolved linked names (not `ListEnabledOrgTemplateSources` — that method no longer exists).
3. `docs/cue-template-guide.md` "Linking Platform Templates" section and the render set formula remain accurate.

When adding new fields that affect template linking, update `docs/cue-template-guide.md` and the AGENTS.md context map.

## Related

- [Template Service](template-service.md) — Explicit linking model and render set formula
- [Guardrail: Template Fields](guardrail-template-fields.md) — New fields must be added across the pipeline
- [Guardrail: Template Docs](guardrail-template-docs.md) — Keep cue-template-guide.md current

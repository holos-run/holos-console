# Guardrail: Template Fields

**When adding new fields to `CreateDeploymentRequest`, `TemplateDefaults`, or related template proto messages**, the field must also be:

1. Added to the `ProjectInput` (user-provided fields) or `PlatformInput` (platform fields) Go struct in `api/v1alpha2/types.go` — CUE schema is generated from these types via `cue get go`
2. Included in the rendering pipeline in `console/deployments/render.go`
3. Reflected in the template editor preview's Project Input or Platform Input default values in the frontend (see `frontend/src/routes/`)
4. Added to the `ExtractDefaults` mapping in `console/templates/defaults.go` if it should be extractable from the CUE `defaults` block (see [ADR 027](../adrs/027-template-defaults-prefill.md) for the authoritative pre-fill behavior)

This ensures template authors always see new fields in the preview, that the CUE schema stays in sync with the proto interface, and that the `defaults` block extraction covers all form fields. See `docs/cue-template-guide.md` for the full template interface.

**Note:** Request-level flags (e.g., `update_linked_templates` on `UpdateTemplateRequest`) that control *how* the backend processes a request are not template fields. They do not need to propagate through CUE types, the render pipeline, frontend preview, or defaults extraction. They only need handler logic and tests.

## Related

- [ADR 027: Authoritative Template Defaults Pre-Fill Behavior](../adrs/027-template-defaults-prefill.md) — Inline authority for the defaults extraction and pre-fill rule.
- [ADR 018](../adrs/018-cue-template-default-values.md) — Superseded by ADR 027; retained for the schema decisions that originally introduced `TemplateDefaults` and `ProjectInput`.
- [Template Service](template-service.md) — The service these fields belong to
- [Deployment Service](deployment-service.md) — The rendering pipeline that consumes these fields
- [Guardrail: Template Defaults Pre-Fill](guardrail-template-defaults.md) — Per-field defaults extraction must cover all form fields
- [Guardrail: Template Linking](guardrail-template-linking.md) — Another template guardrail
- [Guardrail: Template Docs](guardrail-template-docs.md) — Keep docs in sync with template changes

# Guardrail: Template Fields

**When adding new fields to `CreateDeploymentRequest`, `DeploymentDefaults`, or related template proto messages**, the field must also be:

1. Added to the `ProjectInput` (user-provided fields) or `PlatformInput` (platform fields) Go struct in `api/v1alpha2/types.go` — CUE schema is generated from these types via `cue get go`
2. Included in the rendering pipeline in `console/deployments/render.go`
3. Reflected in the template editor preview's Project Input or Platform Input default values in the frontend (see `frontend/src/routes/`)
4. Added to the `ExtractDefaults` mapping in `console/templates/defaults.go` if it should be extractable from the CUE `defaults` block (ADR 018)

This ensures template authors always see new fields in the preview, that the CUE schema stays in sync with the proto interface, and that the `defaults` block extraction covers all form fields. See `docs/cue-template-guide.md` for the full template interface.

## Related

- [Template Service](template-service.md) — The service these fields belong to
- [Deployment Service](deployment-service.md) — The rendering pipeline that consumes these fields
- [Guardrail: Template Linking](guardrail-template-linking.md) — Another template guardrail
- [Guardrail: Template Docs](guardrail-template-docs.md) — Keep docs in sync with template changes

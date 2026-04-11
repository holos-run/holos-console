# Guardrail: Template Doc Completeness

**When making any of the following changes**, verify that the "Writing a Custom Template" section of `docs/cue-template-guide.md` remains end-to-end complete for a product engineer trying to deploy a web service:

## Triggers

1. Adding or removing kinds from the allowed kinds list in `console/deployments/render.go` or `apply.go`.
2. Modifying `console/templates/default_template.cue` (the default resource set).
3. Editing any section of `docs/cue-template-guide.md`.

## Required Content

After any of the above, confirm the "Writing a Custom Template" section still includes:

- A complete working template with `ServiceAccount`, `Deployment`, and `Service`.
- An explanation of the port flow: `input.port` -> container `containerPort` -> Service `targetPort` -> HTTPRoute (optional).
- Guidance on `HTTPRoute`: when to add one (external access) versus relying on the Service ClusterIP (cluster-internal), with a minimal CUE example.

If any of the above is missing or stale after your changes, update the doc as part of the same commit.

## Related

- [Template Service](template-service.md) — The service whose docs must stay current
- [Guardrail: Template Fields](guardrail-template-fields.md) — Field additions also require doc updates
- [Guardrail: Template Linking](guardrail-template-linking.md) — Linking changes require doc updates

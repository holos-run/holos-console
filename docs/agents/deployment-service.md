# Deployment Service

`DeploymentService` in `console/deployments/` manages Kubernetes Deployments: CRUD, status polling, log streaming, CUE render and apply (structured `projectResources`/`platformResources` output), container command/args override, container env vars (literal values, SecretKeyRef, ConfigMapKeyRef), container port configuration, listing project-namespace Secrets/ConfigMaps for env var references, and `GetDeploymentRenderPreview` (returns the CUE template, platform input, project input, rendered YAML, and rendered JSON for a live deployment).

## CUE Rendering

CUE render uses split `PlatformInput` (project, namespace, gatewayNamespace, claims) and `ProjectInput` (name, image, tag, etc.) — see `docs/cue-template-guide.md`.

At render time, `OrgTemplateProvider.ListOrgTemplateSourcesForRender(ctx, org, linkedNames)` is called with the linked names resolved from the deployment template's `console.holos.run/linked-templates` annotation; platform templates may define resources under `platformResources` and/or `projectResources` — the renderer reads both collections when processing platform templates (ADR 016 Decision 8).

## Lifecycle Semantics

- `CreateDeployment` is all-or-nothing: if render or apply fails, partially-applied K8s resources and the deployment ConfigMap are rolled back via `Applier.Cleanup`.
- `UpdateDeployment` uses `Applier.Reconcile` (apply desired resources then delete orphaned owned resources), so removing a resource from a template causes it to be cleaned up.
- `DeleteDeployment` uses `Applier.Cleanup` (delete all owned resources unconditionally).

## Related

- [Template Service](template-service.md) — Provides the CUE templates that deployments render
- [Guardrail: Template Fields](guardrail-template-fields.md) — New proto fields must flow through the render pipeline
- [Package Structure](package-structure.md) — Where `console/deployments/` fits in the codebase

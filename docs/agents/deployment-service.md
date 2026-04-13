# Deployment Service

`DeploymentService` in `console/deployments/` manages Kubernetes Deployments: CRUD, status polling, log streaming, CUE render and apply (structured `projectResources`/`platformResources` output), container command/args override, container env vars (literal values, SecretKeyRef, ConfigMapKeyRef), container port configuration, listing project-namespace Secrets/ConfigMaps for env var references, and `GetDeploymentRenderPreview` (returns the CUE template, platform input, project input, rendered YAML/JSON, and per-collection platform/project resources YAML/JSON for a live deployment).

## CUE Rendering

CUE render uses split `PlatformInput` (project, namespace, gatewayNamespace, claims) and `ProjectInput` (name, image, tag, etc.) — see `docs/cue-template-guide.md`.

At render time, the handler builds a `PlatformInput` that includes `Folders` (resolved via `AncestorWalker`) and resolves platform template CUE sources. The `AncestorTemplateProvider` walks the full ancestor chain (org + folders) from the project namespace via `ListAncestorTemplateSources` and applies the render set formula at each ancestor scope. Linked refs are read from the deployment template's `console.holos.run/linked-templates` annotation. Platform templates may define resources under `platformResources` and/or `projectResources` -- the renderer reads both collections when processing platform templates (ADR 016 Decision 8). `GetDeploymentRenderPreview` returns per-collection fields (`platform_resources_yaml`, `platform_resources_json`, `project_resources_yaml`, `project_resources_json`) that partition resources by origin.

## Multi-Namespace Support

Resources are applied to each resource's own `metadata.namespace` (ADR 026). Templates may produce resources across multiple namespaces in a single render pass. `Apply`, `Reconcile`, and `Cleanup` operate across all namespaces that appear in the desired resource set. The `Reconcile` function accepts optional `previousNamespaces` to ensure orphan cleanup covers namespaces that were used in prior renders but are no longer present.

## Lifecycle Semantics

- `CreateDeployment` is all-or-nothing: if render or apply fails, partially-applied K8s resources and the deployment ConfigMap are rolled back via `Applier.Cleanup`.
- `UpdateDeployment` uses `Applier.Reconcile` (apply desired resources then delete orphaned owned resources across all namespaces), so removing a resource from a template causes it to be cleaned up even if it was in a different namespace.
- `DeleteDeployment` uses `Applier.Cleanup` (delete all owned resources unconditionally across all tracked namespaces).

## Related

- [Template Service](template-service.md) — Provides the CUE templates that deployments render
- [Guardrail: Template Fields](guardrail-template-fields.md) — New proto fields must flow through the render pipeline
- [Package Structure](package-structure.md) — Where `console/deployments/` fits in the codebase

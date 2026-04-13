# Deployment Service

`DeploymentService` in `console/deployments/` manages Kubernetes Deployments: CRUD, status polling (including K8s events and per-pod container status), log streaming, CUE render and apply (structured `projectResources`/`platformResources` output), container command/args override, container env vars (literal values, SecretKeyRef, ConfigMapKeyRef), container port configuration, listing project-namespace Secrets/ConfigMaps for env var references, and `GetDeploymentRenderPreview` (returns the CUE template, platform input, project input, rendered YAML/JSON, and per-collection platform/project resources YAML/JSON for a live deployment).

## Status Polling

Two RPCs expose deployment status:

- `GetDeploymentStatusSummary` returns a lightweight snapshot (phase, replica counts, message) suitable for list views. It reads exclusively from the in-process informer cache in `console/deployments/statuscache/` (see below) and never issues a direct K8s API call. A cache miss returns an empty summary with `DEPLOYMENT_PHASE_UNSPECIFIED` so the frontend renders a stable "Unknown" placeholder instead of branching on a nil summary.
- `GetDeploymentStatus` returns live replica counts, conditions, per-pod status, Kubernetes events, and container status. Events are fetched via field selectors for both the Deployment resource and each pod. Container statuses include init containers and regular containers, mapped to `waiting`, `running`, or `terminated` states with reason, message, and image details. The phase summary on the detail response is also sourced from the informer cache so all status RPCs share one derivation path; scalar replica fields fall back to the live `apps/v1.Deployment.Status` values when the cache has not yet observed the Deployment. The frontend displays events in a table with warning/normal icons and shows container status inline under each pod with color-coded state badges.

`ListDeployments` and `GetDeployment` also populate `Deployment.status_summary` (field 14) from the same cache so list rendering avoids a second round trip.

### Status Cache

`console/deployments/statuscache/` runs a `SharedInformerFactory` scoped to `apps/v1.Deployment` objects carrying the console-managed label selector. The watch is deliberately narrow: Deployments only, no Pods, no ReplicaSets, and no Events. Detail-page data that requires those (per-pod status, events) still flows through direct API calls in `GetDeploymentStatus`. `statuscache.New` blocks on the initial cache sync (bounded by a timeout) so RPC handlers can read immediately after server startup. When the K8s client is nil (dummy-secret-only mode), `NewNop` returns a cache that always reports misses.

### Deprecated Fields

`Deployment.phase` (field 8) and `Deployment.message` (field 9) are marked `[deprecated = true]` in the proto. The backend stopped populating them when the status cache landed (parent plan #912); new code must read `Deployment.status_summary.phase` and `status_summary.message` instead. The field numbers are retained so older clients continue to deserialize — do not remove or renumber them.

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

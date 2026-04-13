# Template Service

Unified `TemplateService` (ADR 021) managing CUE-based templates at organization, folder, and project scopes, stored as K8s ConfigMaps.

## Scope Model

Scope is encoded in the `console.holos.run/template-scope` label (`organization|folder|project`) and each template carries a `TemplateScopeRef{scope, scope_name}` discriminator in its proto representation.

## Embedded Templates

- `default_template.cue` — built-in project template with ServiceAccount, Deployment, Service, and ReferenceGrant
- `example_httpbin.cue` — go-httpbin project example
- `default_referencegrant.cue` — built-in org-level HTTPRoute platform template
- `example_httpbin_platform.cue` — go-httpbin org-level platform template

## Deployment Defaults

Templates can carry `TemplateDefaults` (name, description, image, tag, command, args, port) extracted from the `defaults` block in the CUE source (ADR 018). The backend uses per-field extraction (ADR 025) to read each field independently, so a non-concrete field does not prevent extraction of concrete siblings. The extracted defaults pre-fill the Create Deployment form.

The `RenderTemplate` RPC returns rendered resources as both YAML (`rendered_yaml`) and JSON (`rendered_json`), plus per-collection fields (`platform_resources_yaml`, `platform_resources_json`, `project_resources_yaml`, `project_resources_json`) that partition resources by origin (platform templates vs project templates).

The default template adds a `console.holos.run/deployer-email` annotation to all resources from `platform.claims.email`. The default template includes a `ReferenceGrant` (using `platform.gatewayNamespace`, default "istio-ingress") that allows HTTPRoute resources from the gateway namespace to reference Services in the project namespace.

## Org Seeding via `populate_defaults`

When `CreateOrganization` is called with `populate_defaults: true`, the backend seeds example resources into the new org in the following order (issue #920 / plan #919):

1. The org namespace's `console.holos.run/default-share-roles` annotation is populated with the three standard role grants (Owner, Editor, Viewer — no `nbf`, no `exp`) *before* any folder or project is created. This ensures the seeded default folder and default project pick up the org-level default role grants via the ancestor-default-share merge.
2. The default folder is created as a direct child of the org, inheriting the org's default role grants as both its active share grants and its own default-share cascade.
3. An org-level platform template (HTTPRoute ReferenceGrant, enabled) via `SeedOrgTemplate`.
4. A default project in the org's default folder, inheriting the org default role grants.
5. An example project-level deployment template (go-httpbin) via `SeedProjectTemplate`.

The frontend exposes this as a "Populate with example resources" checkbox in the Create Organization dialog. When `populate_defaults` is false or unset, the default-share-roles annotation is *not* written and the non-seeded code path is preserved.

## Mandatory and Enabled Flags

Platform templates (org-scoped or folder-scoped) can be marked `mandatory` (applied to project namespaces at creation time; always unified at render time) and/or `enabled` (available for linking and render-time unification).

## Explicit Linking Model

ADR 019, extended to cross-level refs: each deployment template ConfigMap may carry the annotation `console.holos.run/linked-templates` (JSON array of `{scope, scope_name, name, version_constraint}` objects); at render time these refs are resolved via `ListAncestorTemplateSourcesForRender` which walks the full ancestor chain (org + folders) and applies the render set formula at each ancestor scope. The template handler's `renderTemplateGrouped` falls back to `ListOrgTemplateSourcesForRender` (org-scope only) when the ancestor walker is not configured. The `version_constraint` field is optional; when present it selects the latest Release satisfying the semver range instead of the mutable working copy.

The render set formula is: `(mandatory AND enabled) UNION (enabled AND ref IN linked_list)`.

`MandatoryTemplateApplier` walks the full ancestor hierarchy (org + folder ancestors) applying mandatory+enabled templates at project creation.

## Folder Template Management

The folder templates page (`/folders/$folderName/templates`) lists platform templates at folder scope with full CRUD (create, edit, delete) for authorized users. Mandatory templates are marked with a lock badge.

## Versioning, Releases, and Version Constraints

ADR 024 introduces template versioning on top of the unified TemplateService:

- **Semantic versioning** -- templates carry a `version` field using `MAJOR.MINOR.PATCH`. New templates start at `0.1.0`; the `0.x` series signals pre-stable development. Versions are immutable once released.
- **Release objects** -- a Release is an immutable snapshot stored as a separate ConfigMap in the same namespace as the parent template. The ConfigMap name encodes `<template-name>--v<MAJOR>-<MINOR>-<PATCH>`. Each release captures the CUE source, defaults, changelog, and upgrade advice.
- **Version constraints on LinkedTemplateRef** -- the `version_constraint` field on `LinkedTemplateRef` accepts semver range expressions (e.g. `^1.2.0`, `>=1.0.0 <2.0.0`). At render time the resolver selects the latest release satisfying the constraint. Empty means latest released version.
- **Safe update propagation** -- MINOR and PATCH releases propagate automatically to consumers whose constraints permit them. MAJOR releases require explicit consumer action (updating the constraint).
- **CheckUpdates RPC** -- returns available updates for all linked templates in a given scope, powering the "updates available" badge and upgrade dialog in the UI.

## Permissions

Edit access requires `PERMISSION_TEMPLATES_WRITE`, enforced via the unified `TemplateCascadePerms` table (Viewer=read-only, Editor=read/write, Owner=full control) applied uniformly at org, folder, and project scope (ADR 021 Decision 2).

## Related

- [Deployment Service](deployment-service.md) — Consumes templates to render and apply K8s resources
- [RBAC](rbac.md) — Template permissions use the cascade table pattern
- [Guardrail: Template Fields](guardrail-template-fields.md) — New fields must be added across the full pipeline
- [Guardrail: Template Linking](guardrail-template-linking.md) — Linked templates annotation handling
- [Guardrail: Template Docs](guardrail-template-docs.md) — Keep cue-template-guide.md current
- [Guardrail: Template Defaults Pre-Fill](guardrail-template-defaults.md) — Per-field defaults extraction and frontend pre-fill
- [Guardrail: Terminology](guardrail-terminology.md) — Use "platform template" not "system template"
- [Tool Dependencies](tool-dependencies.md) — CUE runtime dependency
- [ADR 024](../adrs/024-template-versioning.md) — Versioning, releases, and version constraints design

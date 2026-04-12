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

Templates can carry `DeploymentDefaults` (name, description, image, tag, command, args, env, port) extracted from the `defaults` block in the CUE source (ADR 018) that pre-fill the Create Deployment form.

The `RenderDeploymentTemplate` RPC returns rendered resources as both YAML (`rendered_yaml`) and JSON (`rendered_json`).

The default template adds a `console.holos.run/deployer-email` annotation to all resources from `platform.claims.email`. The default template includes a `ReferenceGrant` (using `platform.gatewayNamespace`, default "istio-ingress") that allows HTTPRoute resources from the gateway namespace to reference Services in the project namespace.

## Org Seeding via `populate_defaults`

When `CreateOrganization` is called with `populate_defaults: true`, the backend seeds example resources into the new org:

1. An org-level platform template (HTTPRoute ReferenceGrant, enabled) via `SeedOrgTemplate`
2. A default project in the org's default folder
3. An example project-level deployment template (go-httpbin) via `SeedProjectTemplate`

The frontend exposes this as a "Populate with example resources" checkbox in the Create Organization dialog.

## Mandatory and Enabled Flags

Platform templates (org-scoped or folder-scoped) can be marked `mandatory` (applied to project namespaces at creation time; always unified at render time) and/or `enabled` (available for linking and render-time unification).

## Explicit Linking Model

ADR 019, extended to cross-level refs: each deployment template ConfigMap may carry the annotation `console.holos.run/linked-templates` (JSON array of `{scope, scope_name, name}` objects); at render time these refs are resolved and passed to `ListOrgTemplateSourcesForRender`.

The render set formula is: `(mandatory AND enabled) UNION (enabled AND ref IN linked_list)`.

`MandatoryTemplateApplier` walks the full ancestor hierarchy (org + folder ancestors) applying mandatory+enabled templates at project creation.

## Folder Template Management

The folder templates page (`/folders/$folderName/templates`) provides a read-only list of platform templates at folder scope. Mandatory templates are marked with a lock badge.

## Permissions

Edit access requires `PERMISSION_TEMPLATES_WRITE`, enforced via the unified `TemplateCascadePerms` table (Viewer=read-only, Editor=read/write, Owner=full control) applied uniformly at org, folder, and project scope (ADR 021 Decision 2).

## Related

- [Deployment Service](deployment-service.md) — Consumes templates to render and apply K8s resources
- [RBAC](rbac.md) — Template permissions use the cascade table pattern
- [Guardrail: Template Fields](guardrail-template-fields.md) — New fields must be added across the full pipeline
- [Guardrail: Template Linking](guardrail-template-linking.md) — Linked templates annotation handling
- [Guardrail: Template Docs](guardrail-template-docs.md) — Keep cue-template-guide.md current
- [Guardrail: Terminology](guardrail-terminology.md) — Use "platform template" not "system template"
- [Tool Dependencies](tool-dependencies.md) — CUE runtime dependency

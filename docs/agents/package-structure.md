# Package Structure

The Go backend is organized into these packages:

- `api/v1alpha2/` — Centralized API types, constants (labels, annotations, resource types), and CUE schema generation. Go types (`PlatformInput`, `ProjectInput`, `Claims`, `EnvVar`) are the source of truth for CUE schema generated via `cue get go`. Embeds the generated CUE schema as `GeneratedSchema` for the renderer to prepend.
- `cmd/` — Main entrypoint, calls into cli package
- `cli/` — Cobra CLI setup with flags for listen addr, TLS, OIDC, RBAC, logging config
- `console/` — Core server package
  - `console.go` — HTTP server setup, TLS, route registration, embedded UI serving
  - `version.go` — Version info with embedded version files and ldflags
  - `rpc/` — ConnectRPC handler implementations and auth interceptor
  - `oidc/` — Embedded Dex OIDC provider
  - `folders/` — FolderService with K8s Namespace backend, slug-based identifiers, reparenting, and depth enforcement (ADR 020, ADR 022)
  - `organizations/` — OrganizationService with K8s Namespace backend, annotation-based grants, and default folder auto-creation
  - `projects/` — ProjectService with K8s Namespace backend, annotation-based grants, and default-folder resolution for new projects
  - `resolver/` — Namespace prefix resolver translating user-facing names to K8s namespace names (`{namespace-prefix}{organization-prefix}{name}` for orgs, `{namespace-prefix}{folder-prefix}{name}` for folders, `{namespace-prefix}{project-prefix}{name}` for projects)
  - `secrets/` — SecretsService with K8s backend and annotation-based RBAC
  - `settings/` — ProjectSettingsService managing per-project feature flags (e.g. deployments toggle) stored as annotations on the project Namespace; deployments toggle requires org-level OWNER via `PERMISSION_PROJECT_DEPLOYMENTS_ENABLE`
  - `templates/` — Unified TemplateService; see [Template Service](template-service.md)
  - `deployments/` — DeploymentService; see [Deployment Service](deployment-service.md)
  - `dist/` — Embedded static files served at `/` (build output from frontend, not source)
- `proto/` — Protobuf source files
  - `holos/console/v1/organizations.proto` — OrganizationService
  - `holos/console/v1/folders.proto` — FolderService (CRUD, hierarchy, reparenting, identifier check)
  - `holos/console/v1/projects.proto` — ProjectService
  - `holos/console/v1/secrets.proto` — SecretsService
  - `holos/console/v1/project_settings.proto` — ProjectSettingsService
  - `holos/console/v1/templates.proto` — unified TemplateService (organization, folder, and project scopes)
  - `holos/console/v1/deployments.proto` — DeploymentService
  - `holos/console/v1/rbac.proto` — Role definitions (VIEWER, EDITOR, OWNER)
  - `holos/console/v1/version.proto` — VersionService
- `gen/` — Generated protobuf Go code (do not edit)
- `frontend/` — React frontend source; see [UI Architecture](ui-architecture.md)

## Related

- [Project Overview](project-overview.md) — What holos-console is
- [Template Service](template-service.md) — CUE-based templates at org, folder, and project scopes
- [Deployment Service](deployment-service.md) — Kubernetes Deployment CRUD and CUE rendering
- [Code Generation](code-generation.md) — Protobuf code generation pipeline

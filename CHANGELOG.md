# Changelog

All notable changes to holos-console are documented here.

## [Unreleased]

### Added — Ancestor-aware TemplatePolicyBinding policy picker (HOL-833)

`BindingForm` now calls `ListLinkableTemplatePolicies` (scope + ancestor walk)
instead of the single-scope `ListTemplatePolicies` RPC, so a folder-scoped
binding can select policies stored at any ancestor folder or org scope.
`AncestorChainResolver` validation on `CreateTemplatePolicyBinding` /
`UpdateTemplatePolicyBinding` ensures the referenced policy is reachable from
the binding's storage scope at authoring time (HOL-836).

**References**: PRs #1119 (backend `ListLinkableTemplatePolicies`), #1120
(frontend hook + BindingForm wiring), #1121 (ancestor-chain authoring
validation).

### Added — ProjectNamespace TemplatePolicyBinding for new Projects (HOL-806)

Operators can now attach a `TemplatePolicyBinding` with
`targetRefs.kind: ProjectNamespace` to an org- or folder-scoped ancestor
namespace. When `CreateProject` is called, the console:

1. Resolves all `ProjectNamespace` bindings that match the new project's
   name (wildcards supported via `projectName: "*"`).
2. Renders each referenced `Template` with platform inputs and collects
   `platformResources` (cluster-scoped resources, the namespace itself,
   and namespace-scoped resources).
3. Merges any template-produced `Namespace` object with the
   RPC-constructed `Namespace`. Conflicting field values are a hard error.
4. Applies cluster-scoped resources, then the unified `Namespace`, then
   namespace-scoped resources — in that order — using Server-Side Apply.
5. Waits for `Namespace.status.phase == Active` before applying
   namespace-scoped resources, then retries with exponential back-off on
   transient API server errors (mirrors the ADR 034 §4 retry strategy).

If no bindings match, `CreateProject` falls through to the existing typed
namespace-create path unchanged.

**New `TemplatePolicyBindingTargetKind` value**: `ProjectNamespace` joins
the existing `ProjectTemplate` and `Deployment` values. No migration of
existing bindings is required.

**Frontend**: the `BindingForm` and `TargetRefEditor` components now
surface `ProjectNamespace` as a selectable kind. Selecting it renders a
project-name input with wildcard (`*`) support.

**Two built-in example templates** are available in the UI picker:

- `project-namespace-description-annotation-v1` — adds a `description`
  annotation to the new namespace. Minimal starting point.
- `project-namespace-reference-grant-v1` — emits a Gateway API
  `ReferenceGrant` in the project namespace so HTTPRoutes in the org
  gateway namespace can reference Services in the project namespace.

**References**: ADR 034
(`docs/adrs/034-namespace-template-policy-binding-for-new-projects.md`),
PRs #1091 (ADR), #1093 (API types), #1096 (resolver), #1098 (render),
#1100 (applier), #1107 (RPC wire-up), #1109 (examples), #1112 (frontend).

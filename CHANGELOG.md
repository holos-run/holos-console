# Changelog

All notable changes to holos-console are documented here.

## [Unreleased]

### Fixed — holos-secret-injector HOL-752 review follow-ups (HOL-839)

- **`holos-secret-injector` CLI now exposes `--mesh-trust-domain`**
  (`HOLOS_SECRETINJECTOR_MESH_TRUST_DOMAIN`) plumbed into
  `controller.Options.MeshTrustDomain`. Operators running a re-pegged
  mesh (anything other than `cluster.local`) can now override the trust
  domain stamped into emitted `AuthorizationPolicy.source.principals`
  without rebuilding the controller. Default remains `cluster.local` so
  upstream Istio installations are unaffected.
- **`ruleEqual` drift detection tightened** in the
  `SecretInjectionPolicyBindingReconciler`. The previous
  `len(a.When) == len(b.When)` compare masked in-place mutations of
  fixed-length `When` slices; the helper now treats any non-empty `When`
  on either side as drift, matching the inline contract that the
  reconciler never populates `Rule.When` today. Guarded by a new table
  test in `binding_controller_test.go` so a future M3 decision to emit
  `When` predicates will force an explicit element-wise compare.

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

### Fixed — `crypto.Params` JSON shape pins required uint fields (HOL-838)

Removed `,omitempty` from the `Time`, `Memory`, `Parallelism`, `KeyLength`,
and `Iterations` JSON tags on `internal/secretinjector/crypto.Params`. A
zero value — which `validateArgon2idParams` rejects on both Hash and
Verify — is now serialized as an explicit `0` instead of being silently
dropped. The envelope JSON shape now faithfully reflects the required
fields, so a future KDF that forgets a zero-rejection check cannot
silently round-trip a degenerate envelope. No on-wire break: envelopes
produced by earlier code either contain the fields already (non-zero
values pass `omitempty`) or fail `Verify` at the validator as before.

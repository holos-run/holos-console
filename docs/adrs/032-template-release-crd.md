<!--
Copyright 2026 The Holos Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
-->

# ADR 032: TemplateRelease as a sibling CRD (HOL-693)

- Status: Accepted
- Date: 2026-04-19
- Binary: `holos-console` (`console/templates/`)
- Follows: [ADR 030 — Template CRD](https://github.com/holos-run/holos-console-docs/blob/main/docs/adrs/030-template-crd.md)
- Supersedes: Release ConfigMap storage introduced in HOL-615's first pass

## Context

HOL-615 set the project direction: move Template storage off ad-hoc
ConfigMaps onto first-class `templates.holos.run/v1alpha1` CRDs served
by the embedded controller-runtime manager. HOL-618 introduced the
`Template` CRD. HOL-661 rewrote the live storage layer to read and
write `Template` through `controller-runtime`'s `client.Client` backed
by an informer cache. HOL-663 introduced a shared envtest helper so
packages can exercise the real API server in-process.

The one remaining ConfigMap storage path after HOL-661 is the
**release history** — the immutable snapshots produced each time an
operator publishes a new version of a Template. Each release snapshot
carries:

1. The CUE source payload at publish time.
2. The structured `TemplateDefaults` at publish time (project-scope
   only).
3. Operator-authored metadata: `changelog` and `upgradeAdvice`.

Today each release is stored as a separate ConfigMap whose name is
`{templateName}--v{major}-{minor}-{patch}`, with custom labels
(`console.holos.run/resource-type=template-release`,
`console.holos.run/release-of={templateName}`) and custom data keys
(`cueTemplate`, `defaults`, `changelog`, `upgrade-advice`).

Two shapes were considered for the migration target.

### Option A: `.status.releases[]` on `Template`

A slice on the owning Template's `.status` (or `.spec`) carrying the
snapshot payloads inline.

Pros:

- Single parent object. List/Get of a Template gives the full
  release history in one call.
- No new CRD to maintain.

Cons:

- **CRD object-size ceiling**. etcd caps a single object at 1 MiB.
  A Template's CUE source is user-authored and can be tens of KiB.
  Ten or twenty releases plus changelogs and upgrade advice can
  approach the ceiling over a Template's lifetime, at which point
  `CreateRelease` begins to fail with a cryptic server error.
- **Spec/Status misuse**. Release payloads are user-authored content
  published by an operator action — they are Spec-material, not
  observed state reported by a controller. Putting them in `.status`
  conflicts with the Gateway-API convention adopted in ADR 030.
- **Partial update friction**. Every publish becomes an
  `UpdateStatus` on the Template, which races with the reconciler's
  own status writes and forces retry loops in the handler. Each
  operator-facing error surfaces as an optimistic-concurrency
  conflict, not a domain error.

### Option B: `TemplateRelease` sibling CRD

A new CRD in the same group and namespace. The object name is a
deterministic function of `(templateName, version)`. Spec carries the
snapshot (CUE, defaults, changelog, upgrade advice). Status follows
the Gateway-API pattern (observedGeneration + Conditions).

Pros:

- **Per-release size budget**. Each release is its own object. Large
  CUE payloads scale linearly across many named objects rather than
  piling up inside a single parent.
- **Spec carries spec-material**. Operator-authored content lives in
  `.spec`. Controller-reported state is free to live in `.status` as
  ADR 030 prescribes.
- **Immutability maps cleanly**. "Releases are immutable after
  publish" becomes a create-only object convention: operators create
  new `TemplateRelease` objects, they do not update existing ones.
  That semantic can later be enforced by a
  `ValidatingAdmissionPolicy` without changing the shape.
- **Label-selector lookups**. `ListReleases(templateName)` is a
  `client.List` with a label selector on
  `console.holos.run/release-of={templateName}` — no unbounded read
  of a mutable parent.

Cons:

- One more CRD in the group. Reconciler and CODEOWNERS footprint
  grows slightly.
- Callers that want both Template and release history take two
  reads (or a list).

## Decision

Adopt **Option B**: `TemplateRelease` as a sibling CRD in
`templates.holos.run/v1alpha1`.

The deterministic name helper is renamed from `ReleaseConfigMapName`
to `ReleaseObjectName` and continues to return
`{templateName}--v{major}-{minor}-{patch}`. The two labels that select
release objects migrate 1:1 from the ConfigMap form to the CRD form:

- `console.holos.run/managed-by=holos-console`
- `console.holos.run/resource-type=template-release`
- `console.holos.run/release-of={templateName}`

The three former ConfigMap data keys and the template-version
annotation become structured fields on `TemplateReleaseSpec`:

| Former key (ConfigMap)      | TemplateReleaseSpec field |
|-----------------------------|---------------------------|
| `data.cueTemplate`          | `spec.cueTemplate`        |
| `data.defaults` (JSON)      | `spec.defaults`           |
| `data.changelog`            | `spec.changelog`          |
| `data.upgrade-advice`       | `spec.upgradeAdvice`      |
| `annotations[template-version]` | `spec.version`        |

The following `api/v1alpha2` constants retire as part of this change:

- `ResourceTypeTemplateRelease` → literal `"template-release"` used
  in the CRD label (no Go constant consumer survives).
- `LabelReleaseOf` → literal
  `"console.holos.run/release-of"` used in the list label selector.
- `AnnotationTemplateVersion`, `ChangelogKey`, `UpgradeAdviceKey` →
  structured fields supersede them.

The `templates.NewK8sClient` constructor drops its `kubernetes.Interface`
argument: after this migration the last non-Namespace call through
that client is gone. Namespace reads already route through
`resolver.Resolver`, which the constructor still accepts.

### Conventions specific to `TemplateRelease`

- **Namespace**: always the owning Template's namespace.
- **Name**: `ReleaseObjectName(templateName, version) =
  {templateName}--v{major}-{minor}-{patch}` (unchanged).
- **Listing by Template**: label selector
  `console.holos.run/release-of={templateName}`.
- **Immutability**: create-only convention. The handler does not
  expose an update path, and the controller does not mutate `.spec`.
- **Status**: follows Gateway-API status pattern from ADR 030.

## Consequences

- Existing operators carrying release ConfigMaps will need a
  migration or a fresh install. Per HOL-615 the pre-release migration
  policy is "no backwards compatibility"; release history authored
  against the ConfigMap shape must be republished against the CRD.
- New CRD manifest ships in `config/crd/`; `make manifests`
  regenerates it alongside the existing Template and TemplatePolicy
  manifests.
- `handler_release_test.go` moves from fake-clientset ConfigMap
  fixtures to the shared envtest helper (HOL-663) so the round-trip
  exercises the real CRD validation and the controller-runtime
  client path.

## Why colocate this ADR

Per the criteria in `docs/adrs/README.md`: the binary
(`holos-console`) and the CRD types (`api/templates/v1alpha1/`) live
in this repository, and the review boundary matches the CODEOWNERS
boundary for `api/templates/` and `console/templates/`. Cross-binary
storage contracts remain in `holos-console-docs/docs/adrs/`.

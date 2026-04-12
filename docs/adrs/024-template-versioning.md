# ADR 024: Template Versioning, Releases, and Dependency Constraints

## Status

Accepted

## Context

Templates in the Holos Console are currently mutable. A platform engineer
updates an organization-level template and the change takes effect immediately
for every deployment that links against it. There is no mechanism to:

1. **Pin a deployment to a known-good template revision.** A project template
   links to an org template by name (ADR 019, extended by ADR 021 Decision 5).
   The renderer resolves the link to whatever CUE source is stored in the
   ConfigMap at render time. A breaking change to the org template breaks all
   linked deployments simultaneously.

2. **Communicate what changed between revisions.** Platform engineers modify
   templates iteratively but there is no changelog or upgrade advice visible to
   product engineers who depend on those templates.

3. **Roll out breaking changes safely.** A MAJOR structural change (renaming a
   field, removing a resource kind) requires every consumer to update their
   project template in lockstep with the platform template change. There is no
   way to publish a new version alongside the old one and let consumers migrate
   at their own pace.

4. **Know when updates are available.** Product engineers have no signal that a
   newer, compatible version of a linked template exists. They discover changes
   only when rendering fails or when a platform engineer notifies them out of
   band.

ADR 019 deferred template versioning and pinning explicitly:

> **Deferred: Template versioning and pinning.** Linked templates are resolved
> by name to the latest version at render time. Pinning to a specific template
> content hash is deferred to a future release.

This ADR pins down the design decisions for template versioning so that
subsequent implementation phases have a clear specification to build against.

### Design Goals

1. **Semantic versioning** gives consumers a machine-readable signal about
   compatibility: PATCH and MINOR updates are safe to adopt automatically;
   MAJOR updates require explicit consumer action.

2. **Immutable releases** provide a stable snapshot that cannot change after
   publication. This eliminates the class of bugs where a "fix" to a live
   template silently breaks consumers.

3. **Version constraints** let consumers express "give me the latest compatible
   version" without coupling to a specific patch number.

4. **Safe update propagation** balances platform agility (ship improvements
   quickly) with consumer stability (do not break working deployments).

5. **Update visibility** closes the feedback loop: product engineers can see
   when compatible updates are available and when breaking changes require
   action.

## Decisions

### Decision 1: Semantic versioning for templates

Templates carry a `version` field using semantic versioning (`MAJOR.MINOR.PATCH`).
Version semantics follow the standard semver contract:

- **MAJOR** — incompatible changes. Removing a CUE field, renaming a resource
  kind, changing the schema in a way that causes existing project templates to
  fail unification.
- **MINOR** — backwards-compatible additions. Adding a new optional CUE field,
  adding a new Kubernetes resource to `platformResources`, introducing a new
  default value.
- **PATCH** — backwards-compatible fixes. Correcting a label value, fixing a
  typo in a generated annotation, adjusting a resource quantity.

New templates start at version `0.1.0`. The `0.x` series signals pre-stable
development where MINOR bumps may include breaking changes (following the semver
specification for major version zero). Templates graduate to `1.0.0` when the
platform engineer considers the interface stable.

Versions are immutable once released. To change a template at a given version,
the author must publish a new version. The mutable working copy (the current
ConfigMap) is the draft that has not yet been released.

**Rationale**: Semver is widely understood, tooling exists in every language for
parsing and comparing semver strings, and the MAJOR/MINOR/PATCH distinction maps
cleanly to the compatibility semantics that matter for CUE template unification.

### Decision 2: Release object — immutable version snapshot

A **Release** is an immutable snapshot of a template at a specific version. It
contains:

- **CUE source** — the exact CUE template source at the time of release.
- **Defaults** — the `TemplateDefaults` values extracted from the CUE source.
- **Changelog** — a human-readable description of what changed since the
  previous release in this MAJOR line.
- **Upgrade advice** — instructions for consumers upgrading from the previous
  MAJOR version. Empty for MINOR and PATCH releases.

A Release is stored as a separate Kubernetes ConfigMap in the same namespace as
the parent template. The ConfigMap name encodes the template name and version:

```
<template-name>--v<MAJOR>-<MINOR>-<PATCH>
```

For example, template `microservice-v2` at version `1.2.0` is stored as:

```
microservice-v2--v1-2-0
```

Dots are replaced with hyphens in the ConfigMap name because Kubernetes DNS
label rules prohibit dots in ConfigMap names.

Labels on the Release ConfigMap:

```yaml
console.holos.run/managed-by: holos-console
console.holos.run/resource-type: template-release
console.holos.run/template-name: microservice-v2
console.holos.run/template-version: 1.2.0
```

The `template-name` label links the release back to the parent template. The
`template-version` label stores the semver string for label-selector queries
(e.g., find all releases of a template, find a specific version).

Data keys in the Release ConfigMap:

```yaml
data:
  template.cue: |
    <CUE source at this version>
  defaults.json: |
    <TemplateDefaults JSON>
  changelog.md: |
    <What changed in this release>
  upgrade-advice.md: |
    <Migration instructions for MAJOR bumps; empty for MINOR/PATCH>
```

Annotations on the Release ConfigMap:

```yaml
console.holos.run/display-name: <inherited from template at release time>
console.holos.run/description: <inherited from template at release time>
console.holos.run/released-at: <RFC 3339 timestamp>
console.holos.run/released-by: <email of the user who published the release>
```

**Immutability enforcement**: Once created, a Release ConfigMap is never
updated. The `CreateRelease` handler rejects requests for versions that already
exist with `AlreadyExists`. To fix a mistake in a release, the author publishes
a new PATCH version.

**Rationale**: Storing each release as a separate ConfigMap reuses the existing
Kubernetes storage pattern (ADR 021 Decision 4) without requiring a new storage
backend. The per-version ConfigMap is independently addressable, which simplifies
garbage collection and version-specific lookups. The `template-name` label
enables efficient listing of all releases for a given template.

### Decision 3: Version constraints on LinkedTemplateRef

The `LinkedTemplateRef` message (ADR 021 Decision 5) gains a
`version_constraint` field:

```protobuf
message LinkedTemplateRef {
  TemplateScope scope = 1;
  string scope_name = 2;
  string name = 3;
  // version_constraint is a semver range expression (e.g., "^1.2.0",
  // ">=1.0.0 <2.0.0", "~1.2"). Empty means "latest released version."
  string version_constraint = 4;
}
```

The constraint syntax follows the npm/Cargo semver range convention:

| Constraint | Meaning |
|---|---|
| `^1.2.0` | `>=1.2.0, <2.0.0` (compatible with 1.2.0) |
| `~1.2.0` | `>=1.2.0, <1.3.0` (approximately 1.2.x) |
| `>=1.0.0 <2.0.0` | Explicit range |
| `1.2.0` | Exactly version 1.2.0 |
| (empty) | Latest released version (no constraint) |

At render time, the resolver finds all releases of the linked template and
selects the latest version that satisfies the constraint. If no release matches,
the render fails with a descriptive error identifying the template, constraint,
and available versions.

**Rationale**: Semver range constraints are the standard mechanism for expressing
compatibility requirements in package managers. The `^` (caret) operator is the
recommended default because it permits MINOR and PATCH updates while preventing
MAJOR breaks. Using an established syntax avoids inventing a custom constraint
language.

### Decision 4: Safe update propagation

MINOR and PATCH updates propagate automatically to consumers whose constraints
permit them. MAJOR updates require explicit consumer action.

**Automatic propagation (MINOR and PATCH)**: When a platform engineer publishes
a new MINOR or PATCH release, the resolver automatically picks it up for any
consumer whose constraint matches. For example, a consumer with constraint
`^1.2.0` automatically receives `1.3.0` and `1.2.1` but not `2.0.0`.

**Manual propagation (MAJOR)**: When a platform engineer publishes a MAJOR
release (e.g., `2.0.0`), consumers with `^1.x` constraints continue resolving
to the latest `1.x` release. The consumer must update their `version_constraint`
to `^2.0.0` (or another range that includes `2.x`) to adopt the new major
version.

**Empty constraint behavior**: A consumer with no version constraint always
resolves to the latest released version regardless of MAJOR version. This is
the default for backwards compatibility with existing linked templates that
have no constraint. It preserves the current behavior where updates take effect
immediately. Consumers who want stability must set an explicit constraint.

**Pre-release (`0.x`) behavior**: Per the semver specification, `0.x` releases
make no stability guarantees. A `^0.1.0` constraint matches `>=0.1.0, <0.2.0`,
meaning MINOR bumps in the `0.x` series are treated as potentially breaking.
This is intentional: pre-stable templates should not auto-update across MINOR
boundaries.

**Rationale**: Automatic MINOR/PATCH propagation matches the expectation that
compatible changes should flow to consumers without manual intervention. Blocking
MAJOR propagation prevents silent breakage. This is the same model used by
npm, Cargo, and Go modules (with different syntax).

### Decision 5: CheckUpdates RPC

A new `CheckUpdates` RPC returns available updates for all linked templates in
a given scope:

```protobuf
rpc CheckUpdates(CheckUpdatesRequest) returns (CheckUpdatesResponse);

message CheckUpdatesRequest {
  // scope identifies the template whose linked templates should be checked.
  TemplateScopeRef scope = 1;
  // name is the template name within the scope.
  string name = 2;
}

message CheckUpdatesResponse {
  repeated TemplateUpdate updates = 1;
}

message TemplateUpdate {
  // ref identifies the linked template.
  LinkedTemplateRef ref = 1;
  // current_version is the version currently resolved by the constraint.
  string current_version = 2;
  // latest_compatible is the newest version matching the constraint.
  // Equal to current_version when no compatible update is available.
  string latest_compatible = 3;
  // latest_available is the absolute newest release regardless of constraint.
  string latest_available = 4;
  // breaking indicates that latest_available is a MAJOR bump from current.
  bool breaking = 5;
  // changelog is the changelog text from the latest_compatible release
  // (or latest_available if breaking).
  string changelog = 6;
  // upgrade_advice is non-empty when breaking is true, containing migration
  // instructions from the MAJOR release.
  string upgrade_advice = 7;
}
```

The UI uses this RPC to display an "updates available" indicator on templates
that have newer compatible versions, and a "breaking update available" warning
for templates where a new MAJOR version exists.

**Rationale**: Without an explicit check-updates mechanism, consumers have no
visibility into available updates. The RPC returns structured data (not just a
boolean) so the UI can show changelogs and upgrade advice inline, reducing the
friction of adopting updates.

### Decision 6: Storage layout for Release ConfigMaps

Release ConfigMaps are stored in the same namespace as the parent template
ConfigMap, following ADR 021 Decision 4:

| Template scope | Release stored in namespace |
|---|---|
| `TEMPLATE_SCOPE_ORGANIZATION` | Organization namespace (`holos-org-<name>`) |
| `TEMPLATE_SCOPE_FOLDER` | Folder namespace (`holos-fld-<hash>-<slug>`) |
| `TEMPLATE_SCOPE_PROJECT` | Project namespace (`holos-prj-<name>`) |

The `console.holos.run/template-name` label on each Release ConfigMap links it
to the parent template. Listing all releases for a template is a single
label-selector query:

```
console.holos.run/resource-type=template-release,console.holos.run/template-name=<name>
```

The parent template ConfigMap continues to store the mutable working copy (the
draft). Publishing a release snapshots the current working copy into a new
Release ConfigMap. The working copy and the releases coexist in the same
namespace with distinct `resource-type` labels (`template` vs.
`template-release`).

**Garbage collection**: Releases are not automatically deleted. A future
housekeeping RPC or policy may prune old releases, but this is deferred. The
number of releases per template is expected to be small (tens, not thousands)
for the foreseeable future.

**Rationale**: Co-locating releases with the parent template in the same
namespace preserves the RBAC ownership model (ADR 021). The namespace's grant
annotations determine who can create releases, which is the same set of users
who can update the template. No additional authorization path is needed.

## Consequences

### Positive

- **Stable deployments.** Consumers can pin to a known-good version and upgrade
  on their own schedule. A platform template update no longer risks breaking
  every linked deployment simultaneously.

- **Safe rollout of breaking changes.** Platform engineers publish a new MAJOR
  version alongside the old one. Consumers migrate incrementally. The old MAJOR
  line continues to receive PATCH fixes if needed.

- **Audit trail.** Each Release ConfigMap is an immutable record of what was
  deployed. Combined with the `released-at` and `released-by` annotations, this
  provides a lightweight change history without requiring a separate audit
  database.

- **Update visibility.** The `CheckUpdates` RPC and the "updates available" UX
  close the feedback loop between platform engineers (who publish improvements)
  and product engineers (who consume them).

- **Backwards compatible rollout.** Existing templates with no releases and no
  version constraints continue to work exactly as they do today. The versioning
  system is additive; consumers opt in by setting a `version_constraint`.

### Negative

- **Increased storage footprint.** Each release creates a new ConfigMap. A
  template with 20 releases stores 20 additional ConfigMaps in the namespace.
  Mitigated by the expectation that release frequency is low (weekly or monthly,
  not per-commit) and ConfigMap sizes are small (CUE source is typically under
  10 KB).

- **Resolver complexity.** The render pipeline must now resolve version
  constraints against a set of releases rather than reading a single ConfigMap
  by name. This adds a list-and-filter step per linked template. Mitigated by
  caching release lists per namespace during a single render request.

- **Semver learning curve.** Teams unfamiliar with semver range syntax may
  misconfigure constraints. Mitigated by defaulting to empty (latest) for
  backwards compatibility and recommending `^MAJOR.MINOR.PATCH` as the standard
  constraint in documentation and UI hints.

- **No automatic garbage collection.** Old releases accumulate indefinitely
  until a future housekeeping mechanism is implemented. Acceptable for the
  near term given the expected low release volume.

## Alternatives Rejected

### Content-addressable versioning (hash-based)

Each release identified by a content hash (SHA-256 of the CUE source) rather
than a semver string. Rejected because:

- Hashes carry no compatibility semantics. A consumer cannot express "give me
  the latest compatible version" with a hash.
- Hashes are not human-readable. Changelogs and upgrade advice require a
  mapping from hash to version that re-invents semver.
- The storage benefit (deduplication of identical releases) does not justify the
  usability cost given the expected low release volume.

### Git-based versioning (tags on a template repository)

Store templates in a Git repository and use Git tags for versioning. Rejected
because:

- The existing storage model is Kubernetes ConfigMaps. Introducing a Git
  repository as a parallel storage backend adds operational complexity
  (repository hosting, access control, sync mechanisms).
- The Holos Console is designed to work with Kubernetes as the single source of
  truth for configuration. Adding Git as a second source of truth creates
  consistency challenges.
- Git tags provide versioning but not the structured metadata (changelog,
  upgrade advice, defaults) that the Release object carries.

### Inline version history (all versions in a single ConfigMap)

Store all releases as keys within a single ConfigMap (e.g.,
`v1.0.0/template.cue`, `v1.1.0/template.cue`). Rejected because:

- Kubernetes ConfigMaps have a 1 MiB size limit. A template with many releases
  or large CUE sources could exceed this limit.
- Concurrent updates to different versions would conflict on the same ConfigMap
  resource version.
- Separate ConfigMaps per release are independently addressable and cacheable.

### No version constraints (always pin to exact version)

Require consumers to specify an exact version in `LinkedTemplateRef` rather
than a range. Rejected because:

- Exact pinning eliminates automatic propagation of MINOR and PATCH fixes.
  Every bug fix in a platform template would require every consumer to update
  their pinned version manually.
- Range constraints are the standard approach in package management for
  balancing stability with currency. Exact pinning is available as a special
  case (`1.2.0` with no range operator).

## References

- [ADR 019: Explicit Platform Template Linking](019-explicit-template-linking.md) — deferred versioning and pinning to a future release
- [ADR 021: Unified Template Service](021-unified-template-service.md) — current template architecture, ConfigMap storage, and linked template model
- [proto/holos/console/v1/templates.proto](../../proto/holos/console/v1/templates.proto) — current proto definitions including `LinkedTemplateRef`
- [console/templates/k8s.go](../../console/templates/k8s.go) — current ConfigMap storage patterns for templates
- [Semantic Versioning 2.0.0](https://semver.org/) — the versioning specification referenced by this ADR

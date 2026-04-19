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

# ADR 031: Secret Injection Service — Architecture Pre-Decisions (HOL-674)

## Status

Accepted

## Context

The [Secret Injection Service MVP][hol-669] adds a second binary,
`holos-secret-injector`, to the `holos-console` repository. The injector
reconciles a new `secrets.holos.run/v1alpha1` API group and, at request time,
renders Kubernetes `Secret` values from references held on custom resources
so that sensitive material never rides on the custom resource itself.

The project-level guardrail is non-negotiable: in `holos-run`, custom
resources carry metadata plus references to `v1.Secret` only. All secret
material lives in native Kubernetes `Secret` objects. This rule exists
because of the Dex refresh-token incident — secrets that were inlined onto
custom resources became un-rotatable and non-auditable, and the recovery
work is what motivates this project. ADR 031 freezes the architectural
choices that make that guardrail enforceable in code.

[HOL-674][hol-674] is the M0 foundation plan that establishes the repo
layout, binary split, build / test plumbing, and ownership boundaries for
the new binary. Every later M0 phase (scaffold, `cmd/` split, Dockerfile
split, Makefile targets, CODEOWNERS, CI) references decisions made here.
Downstream milestones (M1 CRD types, M2 reconcilers, M3 ext_authz) inherit
the same decisions without re-opening them.

Four architectural choices are pre-decided by the maintainer for the
Secret Injection Service. This ADR records them with a rationale for each.
There are no open alternatives. A short Rejected Approaches appendix at the
bottom records what was considered and dropped; the Decision section is a
recording exercise, not a debate.

The structural shape of this ADR follows [ADR 030][adr-030] in
`holos-console-docs`, which records the analogous pre-decisions for the
`templates.holos.run` CRDs and controller-runtime manager that the console
already ships. The Secret Injection Service inherits that controller-runtime
pattern verbatim; this ADR only records the deltas specific to the injector
binary.

## Decision

Ship the Secret Injection Service as a second binary in this repository,
`holos-secret-injector`, in API group `secrets.holos.run` at version
`v1alpha1`. Co-locate the ext_authz gRPC server with the CRD controllers
in the same binary as a `manager.Runnable`. Run the binary with a
two-replica minimum, podAntiAffinity, and a PodDisruptionBudget. Install
via kustomize for the MVP. No Helm chart for MVP.

### Decisions inherited from the maintainer

Four architectural choices are pre-decided. This ADR records them with a
one-to-two-paragraph rationale for each.

#### 1. API group: `secrets.holos.run/v1alpha1`

The API group for the injector's custom resources is
`secrets.holos.run`, at version `v1alpha1`. This matches the convention
set by [ADR 030][adr-030] and [HOL-615][hol-615] for the
`templates.holos.run/v1alpha1` group — one DNS-style group per binary's
domain of concern, `v1alpha1` while the shape is still in flux.

**Rationale.** The group name makes the kind identifiable in a mixed
cluster on sight: an operator running `kubectl get -A
<kind>.secrets.holos.run` immediately knows which binary owns the
resource. Keeping the group distinct from `templates.holos.run` also makes
CODEOWNERS boundaries trivial: `api/templates/**` is owned by console
maintainers, `api/secrets/**` is owned by injector maintainers, and a PR
touching one never requires review from the other. `v1alpha1` signals that
both the types and their status contracts may change without migration
support while the MVP hardens; the [holos-console pre-release
posture][hc-agents] applies verbatim — no backwards-compatibility shims
during alpha. When a kind stabilises it graduates to `v1beta1` in the same
group, following the same idiom already used in `templates.holos.run`.

#### 2. ext_authz placement: `manager.Runnable` in the same binary

The ext_authz gRPC server — the authorisation path that downstream
clients call to ask "may this workload read this secret?" — runs inside
the `holos-secret-injector` binary as a
[`manager.Runnable`][runnable-docs] registered on the controller-runtime
manager. There is no separate `holos-secret-authz` binary, no sidecar
container, and no out-of-process hop between the authz decision and the
CRD cache.

**Rationale.** The authz decision reads the same CRDs the reconcilers
write. A `manager.Runnable` co-located with those reconcilers reads
straight from `mgr.GetClient()` — the cache-backed typed `client.Client`
— so an authz request hits memory, not the API server. Co-location also
means the authz server and the reconcilers share one lifecycle:
`mgr.Start(ctx)` blocks until every runnable (reconcilers plus authz
server) has shut down cleanly, `/readyz` gates on cache sync for both,
and there is one set of RBAC permissions to audit. Splitting authz into a
second binary would force either a duplicate cache (double memory, double
watch connections to the API server) or a network hop for every authz
call (higher tail latency and a new failure mode). The [ADR 030
manager-embedding pattern][adr-030-manager] already proves the shape in
`holos-console`; the injector inherits it unchanged. The binary remains
single-purpose from an operator's perspective — one Deployment, one
Service, one PodDisruptionBudget, one set of probes — even though it
hosts two logical workloads.

#### 3. Replica topology: two-replica minimum, podAntiAffinity, PDB `minAvailable: 1`

The `holos-secret-injector` Deployment ships with `replicas: 2` as its
published minimum, a required `podAntiAffinity` rule spreading replicas
across nodes, and a `PodDisruptionBudget` with `minAvailable: 1`.

**Rationale.** The ext_authz path is on the request path for downstream
secret reads — a zero-replica window blocks production workloads, not
just operator writes. Two replicas with anti-affinity means a single
node reboot, spot-instance reclaim, or a kubelet upgrade cannot take the
injector offline; the PDB makes that guarantee explicit to the cluster
autoscaler and to `kubectl drain`. `minAvailable: 1` is the smallest PDB
that keeps one replica up during voluntary disruptions without blocking
all rolling updates (a `minAvailable: 2` with `replicas: 2` would
deadlock node drains). The `replicas: 2` floor is the **Deployment
manifest's** value, not a hardcoded guard in the binary; operators who
want more replicas for throughput scale up freely without patching the
injector. The controller-runtime manager uses leader election so only
one replica performs reconciliation at a time; both replicas serve authz
from their local cache without coordination.

#### 4. Install method: kustomize for MVP (no Helm)

The injector installs via plain kustomize overlays under
`config/secret-injector/` in this repository. No Helm chart is shipped
for the MVP. The `manifests/` directory continues to hold
kustomize-rendered output as it does for `holos-console` today.

**Rationale.** Kustomize is already the idiomatic install method for
every other component in this repo; adding Helm as a second packaging
surface for the MVP would double the install-path test matrix without
delivering new operator functionality that kustomize cannot express.
Kustomize also composes cleanly with the CRD-first bootstrap order
[ADR 030][adr-030] established — CRDs, CEL
`ValidatingAdmissionPolicy` resources, RBAC, then the rolling Deployment
— all as plain YAML with no templating DSL between the operator and the
cluster. A Helm chart is an explicit post-MVP deliverable: once the
overlay shape is stable and adopters ask for one, the chart is a
mechanical wrap of the kustomize output, not a new architecture. Until
then, operators get a single well-lit path: `kubectl apply -k
config/secret-injector/overlays/<env>/`.

### Conventions inherited from ADR 030

The Secret Injection Service does not re-derive conventions already
recorded in [ADR 030][adr-030]. The following list is the complete set of
conventions the injector inherits verbatim. Readers who have not read ADR
030 should read it before extending the injector.

- **Framework.** [`sigs.k8s.io/controller-runtime`][cr-docs] directly. No
  `client-go` informer harness, no bespoke workqueue code.
- **Scaffolding.** Hand-authored Kubebuilder layout plus `PROJECT` file.
  `kubebuilder init` is not run. The `PROJECT` file grows a new
  `resources:` entry per kind added to `secrets.holos.run/v1alpha1`
  (placeholder entries land in [HOL-687][hol-687]).
- **Reconciler shape.** One reconciler per kind under
  `internal/secretinjector/controller/`. Gateway-API-style status with
  `.status.observedGeneration`, typed
  `.status.conditions []metav1.Condition`,
  [`meta.SetStatusCondition`][meta-setstatuscondition] for writes, and
  the `if conditions changed then .Status().Update()` guard against
  reconcile hot loops.
- **Manager lifecycle.** `ctrl.NewManager` with
  `ctrl.SetupSignalHandler()`, cache-backed client via
  `mgr.GetClient()`, uncached reads via `mgr.GetAPIReader()` where
  stale-cache correctness matters.
- **Logging.** `log.FromContext(ctx)` /
  `log.IntoContext(ctx, logger)` per controller-runtime idiom. No
  ad-hoc loggers in reconciler or authz code.
- **Admission.** CEL
  [`ValidatingAdmissionPolicy`][vap-docs] for cross-object or
  namespace-annotation checks. No console-served or injector-served
  validating webhook.
- **Generation.** `make manifests` runs `controller-gen` over the source
  tree; `+kubebuilder:*` markers on the Go types drive CRDs, deepcopy,
  RBAC, and printer columns. Generated files are never hand-edited.
- **Testing.** [`envtest`][envtest-docs] for integration tests that need
  a real API server; [`pkg/client/fake`][fake-client-docs] for unit
  tests. Idiomatic Go `testing` with table-driven cases per the
  [testing-patterns agent doc][testing-patterns]. Not Ginkgo.
- **Readiness gate.** `/readyz` returns `503` until
  `mgr.GetCache().WaitForCacheSync(ctx)` reports all caches synced. The
  rule applies to this binary the same way it applies to `holos-console`.

### Conventions specific to the injector

The following conventions are new in this ADR and are specific to the
`holos-secret-injector` binary.

#### Project layout

- API types under `api/secrets/v1alpha1/`, one file per kind, plus
  `groupversion_info.go` and `zz_generated.deepcopy.go`.
- Reconcilers under `internal/secretinjector/controller/`, one file per
  kind.
- Ext_authz server under `internal/secretinjector/authz/`, registered
  with the manager via `mgr.Add(&authz.Server{...})` so it participates
  in the manager's `Start(ctx)` / leader election / `/readyz` lifecycle.
- CLI / `cobra` wiring under `internal/secretinjector/cli/`, entry point
  at `cmd/secret-injector/main.go`. The binary name is
  `holos-secret-injector`.
- Manifests under `config/secret-injector/`, with subdirectories:
  - `config/secret-injector/crd/` — generated CRD YAML.
  - `config/secret-injector/rbac/` — generated `Role`, `ClusterRole`,
    and binding YAML.
  - `config/secret-injector/admission/` — hand-authored CEL
    `ValidatingAdmissionPolicy` / binding manifests.
  - `config/secret-injector/samples/` — hand-authored example instances.
  - `config/secret-injector/overlays/<env>/` — per-environment kustomize
    overlays that produce the Deployment, Service,
    PodDisruptionBudget, and anti-affinity rules described under
    Decision §3.

Nothing under `internal/secretinjector/` imports
`internal/controller/`, and nothing under `internal/controller/` imports
`internal/secretinjector/`. The CODEOWNERS phase ([HOL-690][hol-690])
makes this a mechanically enforceable review boundary.

#### Printer column conventions

Printer columns follow the [HOL-615][hol-615] conventions recorded in
ADR 030: each kind surfaces `Ready` (from conditions), `Generation`,
and `Age`; kinds with cross-object references also surface
`ResolvedRefs`. The exact printcolumn sets per kind are defined in M1
([HOL-687][hol-687] scaffolds empty `_types.go` files with the marker
comments in place; the concrete kinds and their condition contracts
land with the M1 CRD ticket under the parent project).

#### RBAC and SA scope

The injector's ServiceAccount receives only the RBAC that
`controller-gen rbac` generates from the reconciler markers, plus
explicit read access to `v1.Secret` objects it needs to resolve. It
does **not** receive cluster-wide secret write permission. The
`v1.Secret` objects it authors are owned via
`controllerutil.SetControllerReference` so garbage collection runs
through the owning CRD, not through the injector's ServiceAccount
identity. This matches the "CRs carry refs, not material" guardrail at
the RBAC layer.

#### Deployment, Service, PDB

The kustomize overlay under `config/secret-injector/overlays/<env>/`
composes:

- A `Deployment` with `replicas: 2` and a required
  `podAntiAffinity` rule on `kubernetes.io/hostname`. Container runs as
  distroless / nonroot per the injector's `Dockerfile.secret-injector`.
- A `Service` exposing the ext_authz gRPC port (consumed by workloads
  that call the authz path). The Service targets both pods; authz
  decisions are stateless reads from cache so either replica can serve
  any request.
- A `PodDisruptionBudget` with `minAvailable: 1`, targeting the
  Deployment's pod selector.
- Leader election enabled on the controller-runtime manager
  (`LeaderElection: true`, `LeaderElectionID` derived from the binary
  name) so only one replica reconciles at a time. Both replicas serve
  authz.

The Deployment does not run `holos-console`. `holos-console` and
`holos-secret-injector` ship as separate Deployments, with separate
ServiceAccounts, separate RBAC bundles, and separate images. CODEOWNERS
partitions the Dockerfiles: `Dockerfile.console` vs
`Dockerfile.secret-injector` ([HOL-689][hol-689]).

## Consequences

### Positive

- **Boundary clarity.** A PR touching
  `api/secrets/**`, `internal/secretinjector/**`,
  `cmd/secret-injector/**`, `Dockerfile.secret-injector`, or
  `config/secret-injector/**` does not require console reviewers and
  vice versa. The package tree, the Dockerfile, and CODEOWNERS all
  agree.
- **One process, two jobs.** The ext_authz server sees the same
  cache-backed snapshot the reconcilers see. No duplicate informer,
  no network hop on the authz hot path, one ServiceAccount to audit.
- **Availability on a request path.** Two replicas with
  anti-affinity and a `minAvailable: 1` PDB means node reboots, spot
  reclaims, and drains never zero out the authz surface. Rolling
  updates continue to work; `kubectl drain` continues to work.
- **Install is a single kubectl command.** `kubectl apply -k
  config/secret-injector/overlays/<env>/` produces every resource the
  operator needs: CRDs, CEL policies, RBAC, Deployment, Service,
  PDB, ServiceAccount. No Helm dependency, no templating DSL, no
  release hooks.
- **ADR 030 leverage.** The controller-runtime conventions,
  generation rules, readiness gate, and testing pattern land in this
  binary with zero re-derivation. New readers cross-reference ADR 030
  once and the injector code behaves the way they expect.

### Negative

- **Two replicas is the floor, even in a dev cluster.** Operators
  running a single-node kind cluster need a dev overlay that relaxes
  `podAntiAffinity` (kind has one node), or they run the binary
  outside the cluster against a kind API server. The relaxation is a
  per-overlay concern; the production overlay keeps the required
  rule.
- **No Helm means no `helm install`.** Some operator audiences
  expect Helm. Until the post-MVP chart lands, they get kustomize
  only. Documented here so the cost is visible; the chart ships when
  the overlay shape stabilises.
- **Two Deployments in the same repo.** `holos-console` and
  `holos-secret-injector` have separate rollouts, separate image
  tags, and separate CI matrices. The parent plan ([HOL-674][hol-674])
  takes on the build / Dockerfile / Makefile / CI split so the
  operator story stays clean; this ADR records that the cost is
  intentional.
- **Leader election on every start.** The injector takes a lease in
  `kube-system` (or the injector's own namespace, configured at
  install time) at start. Cluster admins who audit leases see a new
  lease on every rollout. Standard controller-runtime behaviour;
  documented for operators so the lease is not a surprise.

### Breaking

- **No pre-ADR deployments exist.** The Secret Injection Service has
  never shipped; no dual-path or migration story is needed.
- **CRs must not carry secret material.** The
  `holos-run` guardrail is enforced end-to-end: custom resources
  under `secrets.holos.run/v1alpha1` reference `v1.Secret` objects
  and never inline secret values. Any future schema that proposes an
  inline-secret field is rejected at ADR review.

### Operational

- **Install ordering.** CRDs apply before Deployment rollout; CEL
  admission policies apply before the first CR create (same pattern
  as [ADR 030][adr-030]).
- **Readiness gates on cache sync.** Pods take slightly longer to
  enter rotation on startup while the cache syncs. Correct behaviour
  for a cache-backed authz path — pre-sync, the authz decision is
  wrong.
- **Monitoring.** Operators watch two deployments
  (`holos-console` and `holos-secret-injector`) and two PDBs.
  Dashboards and alerting rules split along the same boundary; no
  shared SLO between the binaries.
- **Upgrades.** The two binaries ship with separate image tags. A
  `holos-console` release does not imply a `holos-secret-injector`
  release, and vice versa. CODEOWNERS and the Makefile matrix make
  the separation explicit.

## Rejected Approaches

All four choices at the top of this ADR had alternatives that were
considered and rejected. For completeness:

- **`holos.run` umbrella API group with a `secrets` sub-resource.**
  Rejected — collapses two binaries' ownership into one group, makes
  CODEOWNERS globs ambiguous, and fights the existing
  `templates.holos.run` precedent that ADR 030 already ships.
- **Separate `holos-secret-authz` binary (authz out of process).**
  Rejected — duplicates the cache (double watch load on the API
  server and double memory footprint for the same informer), adds a
  gRPC hop between the authz decision and the CRD snapshot, and
  invents a second lifecycle to test. A `manager.Runnable` achieves
  the same deployment-shape flexibility (the authz server can run on
  a dedicated port with its own readiness) without the duplication.
- **Single replica with `maxUnavailable: 0` rolling update.**
  Rejected — zero-downtime rolling updates do not cover node
  reboots, spot reclaims, or `kubectl drain`. For an authz path on
  a request hot path, the PDB is the control that matters; a single
  replica has no PDB semantics worth writing down.
- **Helm chart for MVP.** Rejected — adds a second packaging
  surface and a templating DSL between the operator and the cluster
  with no functional gain over kustomize for MVP scope. A post-MVP
  chart that wraps the kustomize output is an explicit deliverable
  once the overlay shape is stable.

## References

- [HOL-669 — Secret Injection Service MVP (project)][hol-669] — parent
  project this ADR records.
- [HOL-674 — plan: M0 — Foundation (repo layout, binary split,
  ADR)][hol-674] — parent ticket; every M0 phase references this ADR.
- [HOL-687 — chore(secret-injector): scaffold disjoint package tree
  and PROJECT entries][hol-687] — first phase that consumes this
  ADR; creates the empty `internal/secretinjector/` and
  `api/secrets/v1alpha1/` packages.
- [HOL-688 — refactor(cmd): split cmd/main.go][hol-688] — produces
  `cmd/secret-injector/main.go` wiring the Cobra CLI and
  controller-runtime manager stub.
- [HOL-689 — build(docker): split Dockerfile][hol-689] — produces
  `Dockerfile.secret-injector` (distroless / nonroot, Apache 2.0
  header).
- [HOL-690 — chore(repo): add CODEOWNERS][hol-690] — enforces the
  ownership boundary recorded in Decision §1 and the "Conventions
  specific to the injector" section.
- [HOL-691 — ci(container): publish holos-secret-injector image and
  verify M0 AC][hol-691] — closes M0 by publishing the image and
  walking the acceptance criteria.
- [HOL-692 — build(make): add injector targets and segregate
  manifests output][hol-692] — routes `make manifests` output into
  `config/secret-injector/crd/` and `config/secret-injector/rbac/`.
- [HOL-615 — feat: unify Template and TemplatePolicy as Kubernetes
  CRDs with informer-backed controller][hol-615] — kind naming,
  printer column, and group-version conventions this ADR aligns to.
- [ADR 030: Template and TemplatePolicy Controllers on
  controller-runtime][adr-030] — the embedded-manager pattern,
  generation rules, readiness gate, and testing pattern this binary
  inherits verbatim.
- [controller-runtime `manager.Runnable` docs][runnable-docs] — the
  interface the ext_authz server implements so it runs inside the
  manager's lifecycle.
- [controller-runtime documentation][cr-docs] — framework reference.
- [Kubernetes ValidatingAdmissionPolicy][vap-docs] — admission
  pattern.
- [`meta.SetStatusCondition`][meta-setstatuscondition] — the
  idempotent condition-write helper.
- [controller-runtime envtest][envtest-docs] — integration test
  harness.
- [controller-runtime fake client][fake-client-docs] — unit test
  harness.
- [holos-console AGENTS.md][hc-agents] — pre-release posture: no
  backwards-compatibility shims during alpha.

[hol-669]: https://linear.app/holos-run/issue/HOL-669/flesh-out-this-project
[hol-674]: https://linear.app/holos-run/issue/HOL-674/plan-m0-foundation-repo-layout-binary-split-adr
[hol-687]: https://linear.app/holos-run/issue/HOL-687/choresecret-injector-scaffold-disjoint-package-tree-and-project
[hol-688]: https://linear.app/holos-run/issue/HOL-688/refactorcmd-split-cmdmaingo-into-cmdholos-console-and-add-cmdsecret
[hol-689]: https://linear.app/holos-run/issue/HOL-689/builddocker-split-dockerfile-into-dockerfileconsole-and
[hol-690]: https://linear.app/holos-run/issue/HOL-690/chorerepo-add-codeowners-with-non-overlapping-templates-secret
[hol-691]: https://linear.app/holos-run/issue/HOL-691/cicontainer-publish-holos-secret-injector-image-and-verify-m0
[hol-692]: https://linear.app/holos-run/issue/HOL-692/buildmake-add-injector-targets-and-segregate-manifests-output-for
[hol-615]: https://linear.app/holos-run/issue/HOL-615/feat-unify-template-and-templatepolicy-as-kubernetes-crds-with
[adr-030]: https://github.com/holos-run/holos-console-docs/blob/main/docs/adrs/030-template-crds-controller-runtime.md
[adr-030-manager]: https://github.com/holos-run/holos-console-docs/blob/main/docs/adrs/030-template-crds-controller-runtime.md#manager-lifecycle
[cr-docs]: https://pkg.go.dev/sigs.k8s.io/controller-runtime
[runnable-docs]: https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/manager#Runnable
[vap-docs]: https://kubernetes.io/docs/reference/access-authn-authz/validating-admission-policy/
[meta-setstatuscondition]: https://pkg.go.dev/k8s.io/apimachinery/pkg/api/meta#SetStatusCondition
[envtest-docs]: https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/envtest
[fake-client-docs]: https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/client/fake
[testing-patterns]: ../agents/testing-patterns.md
[hc-agents]: https://github.com/holos-run/holos-console/blob/main/AGENTS.md

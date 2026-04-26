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

# ADR 036: Kubernetes RBAC + OIDC Impersonation as the Holos Access-Control Model (HOL-1029)

> **Colocated copy.** The canonical copy of this ADR lives in
> [`holos-run/holos-console-docs` docs/adrs/033-kubernetes-rbac-oidc-impersonation.md](https://github.com/holos-run/holos-console-docs/blob/main/docs/adrs/033-kubernetes-rbac-oidc-impersonation.md).
> This file is a verbatim mirror kept colocated with the `holos-console` binary
> per the colocation rule in `docs/adrs/README.md`. If the two files diverge,
> the `holos-console-docs` copy is authoritative.

- Status: Accepted
- Date: 2026-04-26
- Binary: `holos-console` (all ConnectRPC handlers)
- Supersedes: [ADR 007 — Organization Grants Do Not Cascade](https://github.com/holos-run/holos-console-docs/blob/main/docs/adrs/007-org-grants-no-cascade.md),
  [ADR 017 — Configuration Management RBAC Levels](https://github.com/holos-run/holos-console-docs/blob/main/docs/adrs/017-config-management-rbac-levels.md)
- Tracked by: [HOL-1028](https://linear.app/holos-run/issue/HOL-1028)
- Canonical copy: [`holos-run/holos-console-docs` docs/adrs/033-kubernetes-rbac-oidc-impersonation.md](https://github.com/holos-run/holos-console-docs/blob/main/docs/adrs/033-kubernetes-rbac-oidc-impersonation.md)

## Context

`holos-console` currently enforces access control inside its Go process.
The custom `console/rbac` package evaluates Owner / Editor / Viewer roles
against per-request OIDC claims, and secrets are shared via JSON-encoded
`console.holos.run/share-users` and `console.holos.run/share-roles`
annotations on Kubernetes Namespace objects. The console's service account
holds a single broad `ClusterRole` and acts as itself for every Kubernetes
API call.

This design has three structural problems:

1. **No native enforcement.** All authorization decisions live in the
   console's Go process. A bug or bypass in `console/rbac` silently grants
   or denies access; the Kubernetes API server never sees the human
   principal.

2. **No audit trail tied to the human.** Every Kubernetes API call is made
   as the console service account. Kubernetes audit logs record the service
   account, not the human who triggered the action. Compliance tooling
   cannot answer "which user created this resource."

3. **Duplicate model.** The `share-users` / `share-roles` annotation scheme
   reimplements in JSON what Kubernetes already provides natively via `Role`
   and `RoleBinding`. Two models for the same thing create drift and
   confusion.

This ADR records the decision to migrate to **Kubernetes RBAC + OIDC
impersonation** as the single source of truth for access control.
It explicitly supersedes ADR 007 (organization grants do not cascade) and
ADR 017 (configuration management RBAC levels) because both ADRs describe
behavior of the custom `console/rbac` enforcement package that is removed
by this migration. ADR 007's cascade table semantics and ADR 017's
hierarchy-walk authorization model are replaced by native Kubernetes RBAC
evaluation.

### Product context

The product is pre-release. No backwards compatibility is required.
The migration can make breaking changes to the Go API, the RBAC model,
and the Kubernetes object shape without a deprecation period.

## Decisions

### Decision 1 — Kubernetes RBAC is the single access-control authority

All authorization decisions are delegated to the Kubernetes API server.
The custom `console/rbac` package and its cascade tables are removed.
No handler performs in-process role checks; all denials originate from
the Kubernetes API server returning 403 on the impersonated client's call.

The `console.holos.run/share-users` and `console.holos.run/share-roles`
annotations are replaced by native `Role` and `RoleBinding` objects.

### Decision 2 — OIDC impersonation: `sub` is the user identity

The console's service account is granted `impersonate` permission on
`users`, `groups`, and `serviceaccounts`. On every ConnectRPC request,
the console validates the OIDC ID token and constructs a per-request
Kubernetes client whose `rest.Config.Impersonate` is set to:

```
UserName: "oidc:" + idToken.Subject   // e.g. "oidc:Cg1hZG1pbkBsb2NhbA"
Groups:   ["oidc:" + g for g in idToken.Groups]
Extra:    {"email": [idToken.Email]}  // forwarded as Impersonate-Extra-Email
```

**`sub` is chosen as the impersonation username** because:

- Kubernetes OIDC integration (`--oidc-username-claim`) defaults to `sub`.
  A `kubectl --as oidc:$SUB` command and a console RPC against the same
  principal produce identical authorization decisions.
- `sub` is a stable, opaque identifier that does not change when a user
  updates their email address. Keying `RoleBinding.subjects[].name` on `sub`
  prevents privilege orphaning when emails are recycled.
- `email` is a human-readable secondary attribute forwarded in
  `Impersonate-Extra-Email` so audit logs and UI can display it. It is not
  used as the primary principal identifier.

**Identity prefix is `oidc:`** — the same prefix Kubernetes uses for OIDC
principals — so a single principal namespace covers both directly-authenticated
(`kubectl`) and impersonated (console) callers.

### Decision 3 — Group claim forwarding

The OIDC token carries a `groups` claim (default claim name: `groups`,
configurable via `--oidc-groups-claim`). All group values are forwarded with
the `oidc:` prefix:

```
Impersonate-Group: oidc:platform-engineers
Impersonate-Group: oidc:product-engineers
```

The Kubernetes API server must be configured with the same `--oidc-groups-claim`
so that RBAC policies written against `oidc:platform-engineers` apply equally
to `kubectl` users and to console RPC callers.

Dev-mode personas (`platform@localhost`, `product@localhost`, `sre@localhost`)
are granted roles in the dev cluster via `ClusterRoleBinding` or `RoleBinding`
objects referencing their `oidc:$sub` identities, so that E2E tests exercise
the same impersonation path used in production.

### Decision 4 — Per-resource RBAC layout

Per-project and per-resource `Role` / `RoleBinding` objects are provisioned
automatically by the relevant handlers and controllers.

#### 4a. Per-project Role for `v1.Secret`s

When a project is created, the projects handler creates a `Role` in the
project namespace that grants `get`, `list`, `watch`, `create`, `update`,
`patch`, and `delete` on `v1.Secret` objects within that namespace. The
Role name is deterministic: `holos:project-secrets`.

Each entry in the project's sharing list produces a `RoleBinding` that
binds the `holos:project-secrets` Role to the resolved principal:

```yaml
kind: RoleBinding
metadata:
  name: holos:project-secrets:<subject-hash>
  namespace: prj-<project-name>
subjects:
- kind: User
  name: "oidc:<sub>"   # or Group: "oidc:<group>"
roleRef:
  kind: Role
  name: holos:project-secrets
```

#### 4b. Per-Deployment-CR Role

When a Deployment CR is created, the deployments handler creates a `Role`
in the project namespace that grants `get`, `update`, `patch`, and `delete`
on the named `Deployment` custom resource (apiGroup `console.holos.run`,
resource `deployments`). The Role name is deterministic:
`holos:deployment:<deployment-name>`.

Each sharing entry on the Deployment produces a `RoleBinding` for that Role
using the same subject format as 4a. The `RoleBinding` name is
`holos:deployment:<deployment-name>:<subject-hash>`.

#### 4c. Subject format

Principals from the sharing UI are always stored as Kubernetes RBAC subjects
using the `oidc:` prefix:

| Source | `subjects[].kind` | `subjects[].name` |
|--------|------------------|------------------|
| Human user (`sub`) | `User` | `oidc:<sub>` |
| OIDC group | `Group` | `oidc:<group-name>` |
| ServiceAccount | `ServiceAccount` | unchanged (no prefix) |

The subject hash appended to `RoleBinding` names is the first 8 hex digits
of the SHA-256 of the subject name, used to avoid name collisions when a
principal name would exceed Kubernetes 253-character limits.

### Decision 5 — Non-human / service-account flows

Controllers and internal operators that do not have a human principal
(template rendering, secret-injector reconciliation, background jobs) use
the console's own service account identity **without impersonation**. They
are **not** given an `oidc:` prefix. Their `ClusterRole` grants only the
specific verbs they need; they do not hold the `impersonate` permission.

Rationale: introducing a synthetic `oidc:system:...` identity for internal
callers would conflate human and machine principals in RBAC policies, making
audit logs harder to parse. Keeping them as plain service accounts makes
their permissions auditable and separately revocable.

### Decision 6 — UI button gating via SelfSubjectAccessReview

The UI cannot synchronously know whether a user may perform an action
without calling the Kubernetes API. The chosen strategy is:

1. **Per-row `SelfSubjectAccessReview` (SSAR) on list pages.** When a list
   page loads, the console issues one SSAR per resource row for the
   actions that gate UI buttons (e.g., `update`, `delete`). The SSAR uses
   the impersonated client so the check reflects the real user's
   permissions. Buttons are rendered only when the SSAR returns `allowed: true`.

2. **Optimistic actions with 403 → toast fallback.** For actions on detail
   pages where the SSAR cost would add latency (e.g., a single-resource edit
   page), action buttons are shown optimistically. If the underlying RPC
   returns 403, the UI surfaces a toast notification: "You do not have
   permission to perform this action." The user is not redirected away.

This dual strategy means:
- List pages give accurate button visibility without extra round-trips
  (SSAR results are batched alongside the list RPC).
- Detail pages are fast (no blocking SSAR) with a recoverable error path.

Phase 6 (HOL-1034) implements this contract.

### Decision 7 — Migration path for existing Secret sharing annotations

A documented one-shot migration tool (Phase 7, HOL-1035) translates existing
`console.holos.run/share-users` and `console.holos.run/share-roles`
annotations into `RoleBinding` objects in each project namespace, then strips
the annotations. The migration tool is idempotent: re-running it on an already
migrated namespace is a no-op.

No other resource kind requires a migration — Deployment sharing entries are
managed by the deployments handler going forward.

## Consequences

### Positive

- **Native enforcement.** The Kubernetes API server enforces every access
  decision. A bug in the console's Go code cannot silently grant or deny
  access beyond the RBAC policy.
- **Accurate audit logs.** Every Kubernetes API call is stamped with the
  impersonated human principal. Audit tooling can answer "which user
  deleted this secret" from the Kubernetes audit log alone.
- **Single model.** `Role` and `RoleBinding` replace the custom annotation
  scheme. Platform engineers already know Kubernetes RBAC; there is no
  second model to learn.
- **kubectl compatibility.** A user with a valid kubeconfig and
  `--as oidc:$SUB` has the same access as the same user through the
  console. RBAC policies are portable between interactive kubectl use and
  the console UI.
- **Removal of console/rbac package.** ~700 lines of custom authorization
  code (cascade tables, permission constants, hierarchy-walk logic) are
  deleted, replaced by standard Kubernetes RBAC objects.

### Negative

- **SSAR round-trips on list pages.** Per-row access checks add N API calls
  for an N-row list. Mitigated by batching SSAR calls alongside the list
  RPC and caching results for the duration of the page render.
- **RoleBinding proliferation.** Each sharing entry creates one `RoleBinding`
  object. A project with many sharing entries will accumulate many
  `RoleBinding` objects in its namespace. This is manageable at expected
  scale (tens to hundreds per project) and is auditable.
- **Migration required for existing deployments.** The one-shot migration
  tool (Phase 7) must be run before Phase 8 (removal of the legacy surface)
  or access to secrets will be lost. Operator documentation must call this
  out prominently.

### Risks

- **Token replay.** Impersonation tokens are as sensitive as the OIDC ID
  tokens they are derived from. The console must not log or persist the
  impersonation headers. Existing token validation and HTTPS enforcement
  mitigate this.
- **Clock skew.** OIDC token validation is sensitive to clock skew between
  the console host and the identity provider. Existing NTP requirements
  apply.

## Superseded ADRs

### ADR 007 — Organization Grants Do Not Cascade

ADR 007 describes the behavior of the `console/rbac` cascade tables and
the `console.holos.run/share-users` / `console.holos.run/share-roles`
annotation scheme. Both are removed by this migration. The concept of
"org grants that do not cascade" is replaced by Kubernetes RBAC policy:
a `ClusterRoleBinding` grants cluster-wide access; a `RoleBinding` is
namespace-scoped. Cascade semantics are expressed naturally by RBAC scope.

### ADR 017 — Configuration Management RBAC Levels

ADR 017 describes a hierarchy-walk authorization model (Organization →
Folder → Project) implemented in the `console/rbac` package. This model
is removed. Template authoring permissions are now expressed as
`Role` / `RoleBinding` objects in the relevant namespace. The three-role
model (Viewer / Editor / Owner) is not mapped to Kubernetes RBAC roles in
this ADR; the migration removes custom role enforcement entirely. Future
per-resource role differentiation is expressed through Kubernetes RBAC
verbs on specific resources.

## References

- [HOL-1028](https://linear.app/holos-run/issue/HOL-1028) — parent migration plan
- [HOL-1029](https://linear.app/holos-run/issue/HOL-1029) — this ADR
- [ADR 007 — Organization Grants Do Not Cascade](https://github.com/holos-run/holos-console-docs/blob/main/docs/adrs/007-org-grants-no-cascade.md) (superseded)
- [ADR 017 — Configuration Management RBAC Levels](https://github.com/holos-run/holos-console-docs/blob/main/docs/adrs/017-config-management-rbac-levels.md) (superseded)
- [Kubernetes RBAC documentation](https://kubernetes.io/docs/reference/access-authn-authz/rbac/)
- [Kubernetes User Impersonation](https://kubernetes.io/docs/reference/access-authn-authz/authentication/#user-impersonation)
- [OIDC Authenticator (Kubernetes)](https://kubernetes.io/docs/reference/access-authn-authz/authentication/#openid-connect-tokens)

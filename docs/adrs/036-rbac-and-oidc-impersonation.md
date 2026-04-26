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

# ADR 036: Kubernetes RBAC + OIDC Impersonation as the Holos Access-Control Model (HOL-1028)

- Status: Accepted
- Date: 2026-04-26
- Binary: `holos-console` (workspace-wide decision; affects every ConnectRPC handler that touches the Kubernetes API)
- Follows: [ADR 031 — Secret Injection Service](031-secret-injection-service.md)
- Supersedes:
  - [ADR 007 — Organization Grants Do Not Cascade](007-org-grants-no-cascade.md)
  - [ADR 017 — Configuration Management RBAC Levels](017-config-management-rbac-levels.md)

## Context

`holos-console` currently enforces access control inside its own Go process. A custom
`console/rbac` package evaluates Owner / Editor / Viewer roles against per-request OIDC
claims, and shares per-resource access through JSON-encoded
`console.holos.run/share-users` and `console.holos.run/share-roles` annotations on
`v1.Namespace` and `v1.Secret` objects. The console's pod runs as a single
`ServiceAccount` bound to a broad `ClusterRole`, and every Kubernetes API call is made
as that service account — so the effective principal in `etcd` and in the audit log is
always the console pod, never the human who made the request.

This model has accumulated three problems:

1. **Two sources of truth.** Annotations duplicate the model that Kubernetes already
   provides via `Role` and `RoleBinding`. Operators who debug an authorization decision
   must read the console's Go code, not the cluster's RBAC graph. `kubectl auth can-i`
   answers the wrong question — the console's enforcement does not run in the API
   server.
2. **No audit trail tied to the human.** Every API call lands in the API server's audit
   log as the console's service account, with the human identity at best mentioned in a
   custom log line emitted by the console itself. Compliance auditors cannot reconstruct
   "who deleted this Secret" from `kube-apiserver` logs alone.
3. **Cascade rules conflict between ADRs.** ADR 007 says org grants do not cascade.
   ADR 017 says template permissions do cascade. The product engineer cannot predict
   whether their Owner-on-org grant lets them delete a Secret in a project they have
   never been invited to. Each new feature has accreted its own cascade table
   (`OrgCascadeSecretPerms`, `ProjectCascadeSecretPerms`,
   `TemplateCascadePerms`, …) and the package has become hard to reason about.

The product is not in production. There is no need to preserve any of the existing
machinery; we have one opportunity to replace it with the model Kubernetes already
provides natively.

This ADR establishes that single replacement model and is binding for every subsequent
phase of [HOL-1028](https://linear.app/holos-run/issue/HOL-1028/holos-rbac-migration-to-kubernetes-rbac-oidc-impersonation).

## Decision

**Holos uses Kubernetes RBAC as the single source of truth for authorization, and
`holos-console` impersonates the authenticated principal on every Kubernetes API call.**

The following sections pin the concrete contract that subsequent phases (HOL-1029
through HOL-1036) implement against. Each numbered decision is normative — later
phases may not relitigate it.

### Decision 1 — Identity prefix is `oidc:`

Every principal name and group name that appears in an `Impersonate-User`,
`Impersonate-Group`, or `subject` field is prefixed with the literal string `oidc:`.
This matches the prefix that Kubernetes API server applies to OIDC-authenticated
principals when configured with `--oidc-username-prefix=oidc:` /
`--oidc-groups-prefix=oidc:` (the recommended configuration).

A single principal namespace therefore covers two callers:

| Caller | How they reach the API server | Principal name |
|---|---|---|
| Direct `kubectl --as oidc:alice@example.com` (rare, for support / debug) | Kubernetes API server's own OIDC authenticator | `oidc:<sub>` |
| ConnectRPC call routed through `holos-console` | `holos-console` impersonates on behalf of the validated ID token | `oidc:<sub>` |

Because both paths produce the same principal name, a `RoleBinding` written for
`oidc:alice@example.com` authorizes both. There is no separate "Holos user" namespace
to keep in sync with cluster identity.

The Dex deployment used in development MUST be configured with the same prefix and
the same groups claim that the API server expects. The Phase 2 RBAC configuration
([HOL-1030](https://linear.app/holos-run/issue/HOL-1030/configrbac-grant-the-holos-console-service-account-impersonation))
documents the exact `--oidc-*` flags that must be set on the API server, and the Dex
config that emits matching claims.

### Decision 2 — User identity field is OIDC `sub` (the email claim is dropped from the impersonation envelope)

The username impersonated on every downstream Kubernetes call is `oidc:<sub>`, where
`<sub>` is the validated `sub` claim from the user's OIDC ID token.

Rationale:

- Kubernetes' own OIDC authenticator maps the `sub` claim to `username` by default,
  and that is the only mapping that is durable across a user changing their email
  address (e.g. marriage, name change, employer transition between companies that
  share an SSO).
- Holos' current `share-users` annotations key on `email`. This is the existing pain
  point: after Dex emits a refresh token tied to one email and the user later changes
  email, every share annotation breaks. Keying on `sub` removes the failure mode.
- The `email` claim is not stable. RFC 6749 / OIDC core specifically warns that
  `email` may change.

The `email` claim is dropped from the impersonation envelope entirely. It is not
forwarded as `Impersonate-Extra-Email`. Holos does not surface a use case for the
Kubernetes API server, an admission webhook, or an audit consumer to read the email
claim — every authorization decision should be made from `sub` and groups. Operators
who need to recover the human-readable email can join `sub` to Dex's user store
out-of-band; making the email part of the impersonation envelope creates a second
authoritative-looking field that is in fact unstable.

UI surfaces continue to display the email and the human name pulled from the ID
token's `email` and `name` claims (or from a directory lookup against Dex). Display
is independent from authorization.

### Decision 3 — Groups are forwarded as `oidc:<group>`, sourced from the `groups` claim

Every entry in the validated ID token's `groups` claim is prefixed with `oidc:` and
forwarded as `Impersonate-Group`. The system groups
`system:authenticated` and `system:basic-user` are NOT forwarded — they are added
automatically by the Kubernetes API server when the impersonated user successfully
authorizes the impersonate verb.

Holos does not modify, filter, or augment the `groups` claim. If Dex emits
`platform-admins`, the impersonation envelope contains
`Impersonate-Group: oidc:platform-admins`. This keeps Holos' authorization decisions
identical to a `kubectl --as oidc:<sub> --as-group oidc:platform-admins` call.

The Dex configuration in development is responsible for emitting the same groups
that the Kubernetes API server's `--oidc-groups-claim` is configured to read. Phase 2
documents the exact configuration.

### Decision 4 — UI button gating uses `SelfSubjectAccessReview` per row, with optimistic action buttons that translate 403s into toasts

Once the API server is the only authority, the UI cannot synchronously evaluate
"may this user delete?" from in-process state. The console offers two enforcement
points:

1. **`SelfSubjectAccessReview` (SSAR) on list/get.** When the UI loads a list of
   resources, the ConnectRPC handler runs one SSAR per row per non-trivial verb
   (`update`, `delete`, plus `create` for actions like "create binding") using the
   per-request impersonating client. The result is returned to the UI as a
   `permissions: { canUpdate: bool, canDelete: bool, canShare: bool }` block on
   each row. The row uses these flags to enable or disable in-line action buttons.
   The same flags drive whether action items appear in any row-level kebab menu.
2. **Optimistic 403 → toast on submit.** Action buttons that pass the SSAR gate
   still hit the API server, and a 403 returned by the API server (because RBAC
   changed mid-session, because SSAR was advisory and the policy turned out to deny
   on the actual verb, etc.) is caught at the mutation boundary and rendered as a
   toast: "You no longer have permission to delete <resource>. Refresh to see the
   current state."

This is the only model the codebase implements. Specifically:

- Handlers do not perform in-process role checks. They make the API call as the
  impersonated user and surface whatever Kubernetes returns. SSAR is an
  advisory-only optimization for the UI; it never substitutes for the real call.
- A handler MUST NOT mix SSAR-as-gate with an in-process role check as a fallback.
  If SSAR is unavailable (e.g. the user has no read access to
  `selfsubjectaccessreviews`, which would be a misconfiguration), the handler
  returns the `permissions` block with all `false` values; the UI hides the buttons,
  and the user reaches the resource through direct URL navigation if they are
  authorized.
- SSAR is batched per request: handlers issue at most one SSAR per `(resource,
  verb)` pair per row, and rows are evaluated in a single fan-out goroutine pool
  (size 8) to keep tail latency bounded. Phase 6
  ([HOL-1034](https://linear.app/holos-run/issue/HOL-1034/refactorconsole-switch-remaining-connectrpc-handlers-to-impersonated))
  pins the exact batching contract.

We considered three alternatives and rejected each:

| Alternative | Why rejected |
|---|---|
| Always show buttons; rely on 403 toast only | Bad UX — a Viewer sees a "Delete" button on every row. |
| Hide buttons unless SSAR is run client-side | Browser cannot run SSAR without round-tripping through the console anyway, and doubles the request count. |
| Pre-compute role per scope and gate buttons on role | This is exactly the in-process check we are replacing. Kubernetes RBAC is more expressive than three roles, so any local role mapping drifts from the truth. |

### Decision 5 — Service-account / non-human flows continue as the console's own service account; impersonation is opt-in per call

Some control loops do not have a human principal in the request path:

- The Phase 4 / Phase 5 reconcilers that materialize `Role` and `RoleBinding`
  objects in response to ConnectRPC create/update/delete calls.
- The `holos-secret-injector` controller's reconcile loops.
- The template renderer's evaluation of `platformResources` from folder /
  organization templates.
- Background sweepers (e.g. annotation-to-RoleBinding migration job, Phase 7).

These flows continue to use the console's own ServiceAccount identity. They do not
synthesize an `oidc:system:*` principal. Reasons:

- Synthesizing a principal that is not in any directory creates an audit-log entry
  that cannot be traced back to a real account holder. Auditors prefer
  "ServiceAccount `holos-console`" — which has a `ServiceAccount` object, RBAC
  bindings, and an owner — to a synthetic `oidc:system:reconciler` that exists
  nowhere.
- The reconcilers are intentionally privileged: their job is to create RBAC
  *for* humans. Pretending to be one of those humans would be a privilege
  inversion.
- The least-privilege envelope for a reconciler is enforced by the
  `ClusterRole` bound to its `ServiceAccount`, which is exactly the Kubernetes
  norm.

The console's pod-level ServiceAccount therefore carries two distinct sets of
permissions, and the `ClusterRole` provisioned in Phase 2
([HOL-1030](https://linear.app/holos-run/issue/HOL-1030/configrbac-grant-the-holos-console-service-account-impersonation))
must be the union of both:

| Capability set | Verbs | Reason |
|---|---|---|
| Impersonate humans | `impersonate` on `users`, `groups`, `serviceaccounts` | Required for every per-request impersonating client. |
| Reconcile RBAC | `get`, `list`, `watch`, `create`, `update`, `patch`, `delete` on `roles`, `rolebindings` (rbac.authorization.k8s.io); plus `escalate` on `roles` | The handler that creates the per-project Secret `Role` runs as the console's SA, not as the calling human, because the human typically does not have permission to create RBAC in the new namespace. |

`escalate` on `roles` is required because the Phase 4 handler creates a `Role`
that grants verbs on `secrets`, and Kubernetes' RBAC bootstrap protection requires
either `escalate` or that the creator already hold every verb the new `Role`
grants. The console does not hold every Secret verb on every namespace, so
`escalate` is the correct grant. This is documented in Phase 2 and in the RBAC
package itself.

The "elevation moment" is bounded: ConnectRPC handlers that need to create RBAC
(project creation, deployment creation, sharing) explicitly switch from the
impersonating client back to the in-cluster client only for the RBAC `create`
call, then return to the impersonating client for any further work. Switching is
explicit at the call site — there is no automatic fallback.

### Decision 6 — Per-resource RBAC layout

The console provisions per-resource `Role` and `RoleBinding` objects so that every
authorization decision is a single API server lookup against the actual resource the
user is touching. The layout is:

#### 6.1 — Per-project `Role` for `v1.Secret`

When a project is created (Phase 4), the project-creation handler also creates a
single `Role` in the project namespace (`prj-<name>` by default — see
[ADR 020](020-v1alpha2-folder-hierarchy.md) for namespace conventions). Naming and
shape:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: holos-project-secrets
  namespace: prj-<project-name>
  labels:
    app.kubernetes.io/managed-by: holos-console
    console.holos.run/role-purpose: project-secrets
rules:
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
```

The `Role` is created once per project lifetime. It is owned (via
`ownerReferences`) by the project `Namespace`, so deleting the project deletes the
`Role` and all `RoleBindings` that reference it.

There is exactly one `Role` per project for Secrets. Sharing happens by adding
`RoleBinding` objects (Decision 6.3), not by creating per-Secret `Role`s, because
the project namespace IS the granularity of secret access in Holos: every Secret
in a project namespace is intended for that project's deployments to consume.
Per-Secret access control would be possible (using `resourceNames` in the rule,
as we do for Deployments below), but it adds an `O(secrets)` Role-creation cost
that we explicitly defer until a product requirement appears for it. If
finer-than-project granularity becomes necessary, the migration is additive:
introduce a second `Role` with `resourceNames`, leave `holos-project-secrets` in
place as the project-wide grant.

#### 6.2 — Per-Deployment-CR `Role` for that named CR

When a Deployment CR is created (Phase 5), the deployment-creation handler creates
a `Role` in the project namespace whose rules are scoped by `resourceNames` to the
single CR being created:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: holos-deployment-<deployment-name>
  namespace: prj-<project-name>
  labels:
    app.kubernetes.io/managed-by: holos-console
    console.holos.run/role-purpose: deployment
    console.holos.run/deployment: <deployment-name>
  ownerReferences:
    - apiVersion: holos.run/v1alpha2
      kind: Deployment
      name: <deployment-name>
      uid: <deployment-uid>
      controller: true
      blockOwnerDeletion: true
rules:
  - apiGroups: ["holos.run"]
    resources: ["deployments"]
    resourceNames: ["<deployment-name>"]
    verbs: ["get", "list", "watch", "update", "patch", "delete"]
  - apiGroups: ["holos.run"]
    resources: ["deployments/status"]
    resourceNames: ["<deployment-name>"]
    verbs: ["get"]
  - apiGroups: ["holos.run"]
    resources: ["renderstates"]
    verbs: ["get", "list", "watch"]
```

`list` and `watch` are included in the verb list even with `resourceNames` because
Kubernetes RBAC's `list`/`watch` selection happens *before* `resourceNames`
filtering — without these verbs in the rule, an authorized user cannot list any
deployments at all. The API server filters the result set by the
`resourceNames` rule. (See the Kubernetes RBAC documentation, "Restrictions on
resource names that take effect on collection requests".)

The `RenderState` rule is namespace-wide (no `resourceNames`) because the
RenderState index is keyed by the deployment that owns it; reading a single
RenderState requires listing them and filtering client-side. The handler that
fetches a single RenderState passes the calling user's impersonating client to
the API server, which still enforces "you have RenderState read in this
namespace, here is the one you asked for". RenderState contains no secret
material (ADR 033), so namespace-wide read is acceptable.

The `Role` is owned by the Deployment CR via `ownerReferences`. Deleting the
Deployment deletes the `Role` and every `RoleBinding` that references it; the
garbage-collector cascade is the only delete path. Handlers do not delete the
`Role` directly.

#### 6.3 — One `RoleBinding` per share-entry

Every `RoleBinding` binds exactly one `Role` to exactly one principal. Naming and
shape:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: <stable-hash>
  namespace: prj-<project-name>
  labels:
    app.kubernetes.io/managed-by: holos-console
    console.holos.run/role-purpose: <project-secrets | deployment>
    console.holos.run/share-target: <user | group>
    console.holos.run/share-target-name: <sub-or-group>
  ownerReferences:
    - <same as the bound Role>
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: <holos-project-secrets | holos-deployment-<name>>
subjects:
  - kind: User      # or Group
    apiGroup: rbac.authorization.k8s.io
    name: oidc:<sub-or-group>
```

The `RoleBinding` name is `<role-purpose>-<u|g>-<base32-of-sha256(name)[0:10]>` —
deterministic, lowercase, DNS-1123-compliant, and bounded to ≤ 63 chars. The hash
is sufficient to disambiguate; the labels carry the human-readable subject for
operator queries (`kubectl get rolebindings -l
console.holos.run/share-target-name=alice@example.com`).

Workers in subsequent phases generate the name by calling a single helper
`rbacname.RoleBindingName(rolePurpose, kind, name)` that lives in the shared
RBAC package. Phase 4 / 5 specify the helper's exact signature; this ADR pins
the algorithm so two phases can independently produce the same name.

`ownerReferences` point at the bound `Role`. When a `Role` is deleted (because
its parent project / deployment was deleted), every `RoleBinding` that referenced
it is garbage-collected. Handlers do not delete `RoleBindings` on share-removal
through GC alone — they delete them directly via the API server's `Delete` verb,
because the share-revoke semantics is "remove this user immediately", not
"remove this user when the parent is deleted".

There is exactly one `RoleBinding` per (Role, principal) pair. The handler that
adds a share is idempotent: it computes the deterministic name and either creates
the object or finds the existing object and is done. The handler that removes a
share deletes by deterministic name and ignores `NotFound`. Two concurrent share
operations therefore cannot create duplicate bindings; the API server guarantees
single-object-per-name.

#### 6.4 — Share-entry mapping

A "share-entry" in the UI maps to exactly one `RoleBinding`:

| UI field | RoleBinding subject |
|---|---|
| Share with user `alice@example.com` (resolved to OIDC `sub` `0e2e46e4...`) | `kind: User, name: oidc:0e2e46e4...` |
| Share with group `platform-admins` | `kind: Group, name: oidc:platform-admins` |

Resolution of email → sub happens at share-add time, against Dex's user store.
The resolution is authoritative: the share UI never stores email-as-principal in
the cluster; the persisted form is always `oidc:<sub>` or `oidc:<group>`. The
display layer maps `oidc:<sub>` back to email/name at render time using the same
Dex lookup.

### Decision 7 — No backwards compatibility; one Secret-only migration path

The product is not in production. The migration story is therefore minimal:

- **Custom `console/rbac` package**: removed in Phase 8
  ([HOL-1036](https://linear.app/holos-run/issue/HOL-1036/chorerbac-remove-consolerbac-package-and-legacy-sharing-surface)).
  No fallback path. Every handler is converted to use the impersonated client
  before the package is removed.
- **`console.holos.run/share-users` and `console.holos.run/share-roles` annotations
  on Namespace and Secret objects**: a one-shot job in Phase 7
  ([HOL-1035](https://linear.app/holos-run/issue/HOL-1035/featmigration-translate-secret-sharing-annotations-to-rolebindings))
  reads existing annotations on Secret objects, materializes equivalent
  `RoleBinding` objects against the per-project `Role` (Decision 6.1), and strips
  the annotations. The job is idempotent and re-runnable.
- **All other existing share annotations** (on Deployments, Folders, Organizations):
  not migrated. Deleted with the `console/rbac` package. The product owner has
  confirmed no production data exists for those scopes.
- **OIDC identity changeover**: existing Dex tokens become invalid on prefix change.
  Dev-environment users sign in again. There is no scripted conversion of session
  state.

### Decision 8 — Kubernetes API server is the only authorizer

A handler MUST NOT make an in-process authorization decision. Every operation
follows this skeleton:

```go
// Resolve the per-request impersonating client (Phase 3).
client, err := impersonating.FromContext(ctx)
if err != nil {
    return nil, connect.NewError(connect.CodeUnauthenticated, err)
}

// Make the API call as the user. The API server returns either the resource
// or a 403; the handler does not have a "policy" branch.
secret, err := client.CoreV1().Secrets(ns).Get(ctx, name, metav1.GetOptions{})
if apierrors.IsForbidden(err) {
    return nil, connect.NewError(connect.CodePermissionDenied, err)
}
```

The only places where an in-cluster client is used instead are documented in
Decision 5 (RBAC reconciliation, controller loops, render-time evaluation). A
handler that mixes both must do so explicitly, with a comment naming this ADR.

This removes the entire `console/rbac` decision tree:

- No `CheckAccessGrants`, `CheckCascadeAccess`, or `bestRoleWithOrg`.
- No `CascadeTable`, `OrgCascadeSecretPerms`, `ProjectCascadeSecretPerms`,
  `TemplateCascadePerms`.
- No `RoleViewer` / `RoleEditor` / `RoleOwner` enum. The roles vanish; what remains
  is the Kubernetes verb set.

The pre-existing roles map onto Kubernetes verbs in subsequent phases:

| Holos role (deprecated) | Equivalent Kubernetes verbs on the bound Role |
|---|---|
| Viewer | `get`, `list`, `watch` |
| Editor | Viewer + `create`, `update`, `patch` |
| Owner | Editor + `delete`; plus the right to bind the `Role` to others |

"The right to bind to others" is itself a Kubernetes RBAC concept: it requires
the binding user to either hold every verb the bound `Role` grants
(`escalate` semantics) or to have explicit `bind` on the `Role`. The console
mediates this: the share UI is gated by an SSAR for `bind` on the target `Role`,
and the actual `RoleBinding` create is performed by the console's
ServiceAccount, which holds `escalate` on `roles` (Decision 5).

## Open Questions Carried into Implementation

These are NOT open questions — they are deliberate deferrals to the named phase.
Phase 1 closes every open question in the parent issue.

- **Specific `--oidc-*` flag values for the in-cluster API server** — pinned in Phase
  2 ([HOL-1030](https://linear.app/holos-run/issue/HOL-1030/configrbac-grant-the-holos-console-service-account-impersonation))
  and in the development cluster's `kind` config.
- **Helper signature for `rbacname.RoleBindingName`** — pinned in Phase 4
  ([HOL-1032](https://linear.app/holos-run/issue/HOL-1032/featsecrets-provision-project-rolerolebindings-and-use-impersonated)).
- **Exact SSAR batching constants** (goroutine pool size, request timeout) —
  pinned in Phase 6
  ([HOL-1034](https://linear.app/holos-run/issue/HOL-1034/refactorconsole-switch-remaining-connectrpc-handlers-to-impersonated)).

## Consequences

### Positive

- **Single source of truth.** The cluster's RBAC graph is the answer to every
  authorization question. `kubectl auth can-i` and `kubectl --as oidc:<sub>`
  produce identical decisions to a console RPC made by the same user.
- **Audit trail is the API server's audit trail.** Every create, update, delete
  in `etcd` carries `user.username = oidc:<sub>` and the impersonator
  (`impersonatedUser`) field captures `holos-console` for traceability of which
  binary made the call. Compliance auditors can reconstruct user activity from
  `kube-apiserver` logs alone.
- **Smaller code surface.** The `console/rbac` package, its cascade tables, its
  permission enum, and the share-annotation parser are all deleted in Phase 8.
- **Consistent semantics across resources.** Adding a new resource kind requires
  defining one `Role` template and using the existing `RoleBinding` machinery —
  no new permission enum, no new cascade table.
- **Predictable group behavior.** Group membership flows from Dex through the
  unmodified `groups` claim; an operator who adds `oidc:platform-admins` as the
  subject of a `RoleBinding` knows exactly which Dex group is granted.

### Negative

- **Latency of `SelfSubjectAccessReview`.** Each list response is followed by
  one or more SSARs. Mitigated by per-request batching and an 8-goroutine fan-out
  (Decision 4). Measured impact will be tracked in Phase 6.
- **`escalate` on `roles` granted to the console's ServiceAccount** is a
  privileged grant. Mitigated by limiting the verb set on the `ClusterRole` to
  exactly the RBAC objects Holos manages, and by audit-logging every
  `RoleBinding` create made through the in-cluster client.
- **Per-resource `Role` for every Deployment** scales linearly with deployment
  count. Each `Role` is small (~1 KB in `etcd`); for the M2 target of ~10⁴
  deployments per cluster, this is ~10 MB — comparable to the storage footprint
  of the deployments themselves. Acceptable.

### Risks

- **API server outage breaks the UI.** Today the console's in-process RBAC works
  even if the API server is degraded; the new model fails closed because every
  authorization is an API call. We accept this — the rest of the console already
  fails closed under API server outage (no resources to render), so the
  authorization layer doing the same is no regression.
- **Drift between Dex's groups claim and the API server's `--oidc-groups-claim`
  config.** If Dex emits `groups: [admins]` and the API server is configured to
  read `--oidc-groups-claim=roles`, every group binding silently denies. The
  Phase 2 RBAC config commits both ends of this contract to source control and
  CI verifies them in `make test-e2e`.

## References

- [HOL-1027](https://linear.app/holos-run/issue/HOL-1027/holos-rbac-migration-to-k8s-rbac) — the voice memo
  that motivated the migration.
- [HOL-1028](https://linear.app/holos-run/issue/HOL-1028/holos-rbac-migration-to-kubernetes-rbac-oidc-impersonation) — the implementation plan this ADR governs.
- [Kubernetes documentation — User impersonation](https://kubernetes.io/docs/reference/access-authn-authz/authentication/#user-impersonation) — the
  `Impersonate-User`, `Impersonate-Group`, `Impersonate-Extra-*` request headers.
- [Kubernetes documentation — RBAC `resourceNames` restrictions on collection
  requests](https://kubernetes.io/docs/reference/access-authn-authz/rbac/#restrictions-on-resource-names-that-take-effect-on-collection-requests) — explains
  the `list`/`watch` requirement in Decision 6.2.
- [Kubernetes documentation — Privilege escalation prevention and bootstrapping](https://kubernetes.io/docs/reference/access-authn-authz/rbac/#privilege-escalation-prevention-and-bootstrapping) — explains
  the `escalate` verb in Decision 5.
- [ADR 007](007-org-grants-no-cascade.md) — superseded.
- [ADR 017](017-config-management-rbac-levels.md) — superseded.
- [ADR 020](020-v1alpha2-folder-hierarchy.md) — namespace naming conventions referenced
  in Decision 6.
- [ADR 031](031-secret-injection-service.md) — secret-injector ServiceAccount and
  no-sensitive-on-CRs invariant referenced in Decision 5.
- [ADR 033](033-render-state-crd.md) — RenderState contains no secret material;
  referenced in Decision 6.2.

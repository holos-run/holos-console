# API Reference: `secrets.holos.run/v1alpha1`

Authored-by-hand reference for the four kinds shipped in milestone M1
([HOL-675](https://linear.app/holos-run/issue/HOL-675/plan-m1-crds-admission-policies-rbac)).
Regenerate the underlying CRDs with `make manifests-secrets`; update this
document in the same change when the Go types move.

- Source of truth: `api/secrets/v1alpha1/*_types.go` (kubebuilder markers
  drive the CRD YAMLs under `config/secret-injector/crd/`).
- ADR: [`docs/adrs/031-secret-injection-service.md`](../adrs/031-secret-injection-service.md).
- Admission policies: `config/secret-injector/admission/` (HOL-703).

## Invariant: no sensitive values on CRs

Every kind in this group is a **control object**, not a vault. A CR leaks
through `kubectl get -o yaml`, etcd snapshots, Velero/Gemini backups, audit
logs, and any principal with `get`/`list` on the CRD. **Forbidden on any
spec or status field:**

- Plaintext credential material (API keys, tokens, passwords, refresh
  tokens).
- Hash output bytes, salt bytes, pepper bytes, or any pepper version
  encoded as an opaque string that hints at rotation generation beyond a
  counting integer.
- Prefix, last-4, fingerprint, or any truncation of the credential that
  reveals non-trivial entropy.
- Upstream credential bytes (the payload swapped in on the hot path).

**Allowed on the CR:** opaque IDs (KSUID-shaped `credentialID`), `{name,
key}` references to a sibling `v1.Secret`, integer `pepperVersion`, phase,
and `[]metav1.Condition`. Sensitive bytes live in referenced `v1.Secret`s
that carry tighter RBAC, encryption-at-rest, and KMS integration.

---

## Kind: `UpstreamSecret`

- **Group/Version:** `secrets.holos.run/v1alpha1`
- **Scope:** `Namespaced` (project namespaces only — admission:
  `upstreamsecret-project-only`).
- **Short names:** `us`. **Categories:** `holos`, `secrets`.
- **Invariant:** must not carry upstream credential bytes, prefix, or
  fingerprint anywhere in `spec`/`status`.

### `spec`

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `secretRef.name` | `string` (min 1) | yes | `metadata.name` of the sibling `v1.Secret` in the same namespace. |
| `secretRef.key` | `string` (min 1) | yes | Key inside the referenced `v1.Secret .data` that holds the upstream credential bytes. |
| `upstream.host` | `string` (min 1) | yes | Upstream hostname matched against `:authority` exactly (no wildcards). |
| `upstream.scheme` | `enum { http, https }` | yes | Transport scheme. |
| `upstream.port` | `int32` (1..65535) | no | Upstream TCP port; defaults by scheme when unset. |
| `upstream.pathPrefix` | `string` | no | Literal URL path prefix the injection applies to. |
| `injection.header` | `string` (RFC 7230 token regex) | yes | HTTP request header the injector writes. |
| `injection.valueTemplate` | `string` | no | Go `text/template` over `{{.Value}}`; admission rejects control chars (`upstreamsecret-valuetemplate-no-control-chars`). Empty means "pass `{{.Value}}` verbatim". |

### `status`

| Field | Type | Description |
| --- | --- | --- |
| `observedGeneration` | `int64` | Most recent `metadata.generation` the reconciler has acted on. |
| `conditions` | `[]metav1.Condition` | Known types: `Accepted`, `ResolvedRefs`, `Ready`. Reasons: `Accepted`, `InvalidSpec`, `ResolvedRefs`, `SecretNotFound`, `SecretKeyMissing`, `Ready`, `NotReady`. |

---

## Kind: `Credential`

- **Group/Version:** `secrets.holos.run/v1alpha1`
- **Scope:** `Namespaced`.
- **Short names:** `cred`. **Categories:** `holos`, `secrets`.
- **Invariant:** full-marshal of any `Credential` (YAML or JSON, spec +
  status) must produce **zero** bytes matching the forbidden patterns
  (`sih_[A-Za-z0-9_-]{20,}`, hash-material regex). Enforced by the
  field-name guard in `credential_invariant_test.go` and by the admission
  policies listed below.

### `spec`

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `authentication.type` | `enum { APIKey, OIDC }` | yes | Authentication scheme. Admission rejects `OIDC` in v1alpha1 (`credential-authn-type-apikey-only`). |
| `authentication.apiKey.headerName` | `string` (min 1) | when `type=APIKey` | HTTP header the injector writes on the hot path. |
| `upstreamSecretRef.namespace` | `string` | no | Target namespace; admission requires `== metadata.namespace` (`credential-upstreamref-same-namespace`). |
| `upstreamSecretRef.name` | `string` (min 1) | yes | `metadata.name` of the sibling `UpstreamSecret`/`v1.Secret`. |
| `upstreamSecretRef.key` | `string` (min 1) | yes | Key inside the referenced `v1.Secret .data`. |
| `expiresAt` | `metav1.Time` (pointer) | no | Wall-clock expiry; reconciler moves `.status.phase` to `Expired` once elapsed. |
| `revoked` | `bool` | no | Administrative revocation request; terminal. |
| `bindToSourcePrincipal` | `*bool` | no | Reserved for M3; v1alpha1 admits but does not act on it. |
| `rotation.graceSeconds` | `int32` (>=0) | no | Seconds a retiring credential remains valid after a successor is issued. |
| `selector.targetRefs[].group` | `string` | no | API group of the target; `""` (core) is the only value accepted in v1alpha1. |
| `selector.targetRefs[].kind` | `string` (min 1) | yes (when target set) | Target kind; admission accepts only `ServiceAccount`. |
| `selector.targetRefs[].name` | `string` (min 1) | yes (when target set) | Target `metadata.name`; same-namespace lookup only. |
| `selector.workloadSelector` | `*metav1.LabelSelector` | no | Pod-label selector OR-combined with `targetRefs`. |

### `status`

| Field | Type | Description |
| --- | --- | --- |
| `observedGeneration` | `int64` | Most recent `metadata.generation` the reconciler has acted on. |
| `phase` | `enum { Active, Rotating, Retired, Revoked, Expired }` | Current lifecycle phase. |
| `credentialID` | `string` (KSUID regex `^[0-9A-Za-z]{27}$`, len 27) | Opaque identifier; **MUST NOT** be or contain the plaintext, a prefix, a last-4, or any substring of the plaintext. |
| `hashSecretRef.name` | `string` (min 1) | `metadata.name` of the sibling `v1.Secret` (same namespace) that stores the argon2id hash + per-credential salt. Owned by the reconciler (M2). |
| `hashSecretRef.key` | `string` (min 1) | Key inside that `v1.Secret .data`. |
| `pepperVersion` | `int32` | Monotonic counter of pepper rotations; **MUST NOT** hint at pepper material. |
| `conditions` | `[]metav1.Condition` | Known types: `Accepted`, `HashMaterialized`, `Ready`, `Expired`. Reasons: `Accepted`, `InvalidSpec`, `OIDCNotSupported`, `HashMaterialized`, `HashSecretMissing`, `Ready`, `NotReady`, `Revoked`, `Expired`. |

---

## Kind: `SecretInjectionPolicy`

- **Group/Version:** `secrets.holos.run/v1alpha1`
- **Scope:** `Namespaced` (organization or folder namespaces only —
  admission: `secretinjectionpolicy-folder-or-org-only`).
- **Short names:** `sip`. **Categories:** `holos`, `secrets`.
- **Invariant:** carries only the match predicate, authentication scheme,
  and the name of the object that holds the sensitive bytes. No field may
  carry plaintext credential bytes, hash material, or any truncation of the
  backing secret.

### `spec`

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `direction` | `enum { Ingress, Egress }` | yes | Traffic direction the policy applies to at the bound target. |
| `match.hosts[]` | `[]string` | no | Exact `:authority` values (no wildcards). |
| `match.pathPrefixes[]` | `[]string` | no | URL path prefixes (literal; invariant-allowlisted exemption — URL-path match, not credential leak). |
| `match.methods[]` | `[]string` (item regex `^[A-Za-z][A-Za-z0-9-]*$`) | no | RFC 7231 method tokens. |
| `callerAuth.type` | `enum { APIKey, OIDC }` | yes | Expected authentication scheme. Admission rejects `OIDC` (`secretinjectionpolicy-authn-type-apikey-only`). |
| `upstreamRef.scope` | `enum { project, folder, organization }` | yes | Resolution scope; admission accepts only `project` in v1alpha1. |
| `upstreamRef.scopeName` | `string` (min 1) | yes | Project/folder/organization name that narrows the resolution. |
| `upstreamRef.name` | `string` (min 1) | yes | `metadata.name` of the `UpstreamSecret` (M1) or `Credential` (M2) swapped in on the hot path. |

### `status`

| Field | Type | Description |
| --- | --- | --- |
| `observedGeneration` | `int64` | Most recent `metadata.generation` the reconciler has acted on. |
| `conditions` | `[]metav1.Condition` | Known types: `Accepted`, `Ready`. Reasons: `Accepted`, `InvalidSpec`, `Ready`, `NotReady`. |

---

## Kind: `SecretInjectionPolicyBinding`

- **Group/Version:** `secrets.holos.run/v1alpha1`
- **Scope:** `Namespaced` (organization or folder namespaces only —
  admission: `secretinjectionpolicybinding-folder-or-org-only`).
- **Short names:** `sipb`. **Categories:** `holos`, `secrets`.
- **Invariant:** names a policy and target set only. No field may carry
  sensitive byte material.

### `spec`

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `policyRef.scope` | `enum { organization, folder }` | yes | Scope of the referenced policy; project-scope refs are rejected (`secretinjectionpolicybinding-policyref-same-namespace-or-ancestor`). |
| `policyRef.namespace` | `string` (min 1) | yes | Namespace of the referenced `SecretInjectionPolicy`; admission verifies the namespace's `console.holos.run/resource-type` label matches `scope`. |
| `policyRef.name` | `string` (min 1) | yes | `metadata.name` of the referenced policy. |
| `targetRefs[].group` | `string` | no | API group of the target; `""` (core) is the only value accepted. |
| `targetRefs[].kind` | `enum { ServiceAccount, Service }` | yes | Bound Kubernetes kind. |
| `targetRefs[].namespace` | `string` (min 1) | yes | Target namespace; admission requires same namespace or a descendant of the binding's scope. |
| `targetRefs[].name` | `string` (min 1) | yes | Target `metadata.name`. |
| `workloadSelector` | `*metav1.LabelSelector` | no | Additional pod-label filter; `nil` means "no filter". |

`targetRefs` has `MinItems=1`; duplicate `(group, kind, namespace, name)`
tuples are rejected by the reconciler.

### `status`

| Field | Type | Description |
| --- | --- | --- |
| `observedGeneration` | `int64` | Most recent `metadata.generation` the reconciler has acted on. |
| `conditions` | `[]metav1.Condition` | Known types: `Accepted`, `ResolvedRefs`, `Programmed`, `Ready`. Reasons: `Accepted`, `InvalidSpec`, `ResolvedRefs`, `PolicyNotFound`, `InvalidTargetKind`, `Programmed`, `AuthorizationPolicyWriteFailed`, `WaypointNotFound`, `Ready`, `NotReady`. |

---

## Shared types

- `SecretKeyReference { name, key }` — same-namespace `v1.Secret` reference.
- `NamespacedSecretKeyReference { namespace?, name, key }` —
  admission-enforced same-namespace reference (`namespace` optional; when
  non-empty must equal the referencing CR's namespace).
- `PhaseType` enum — `Active | Rotating | Retired | Revoked | Expired`.
- `AuthenticationType` enum — `APIKey | OIDC` (admission rejects `OIDC` in
  v1alpha1).
- `Finalizer = "secrets.holos.run/finalizer"` — used by reconcilers when
  non-trivial cleanup (e.g. deleting the `Credential`'s hash `v1.Secret`) is
  required before the API server deletes the managed object.

## Admission policies (enforced by API server)

| Policy | Target kind(s) | Purpose |
| --- | --- | --- |
| `upstreamsecret-project-only` | `UpstreamSecret` | Creation restricted to project namespaces. |
| `upstreamsecret-valuetemplate-no-control-chars` | `UpstreamSecret` | Rejects CRLF, control chars, header separators in `injection.valueTemplate`. |
| `credential-authn-type-apikey-only` | `Credential` | v1alpha1 rejects `spec.authentication.type=OIDC`. |
| `credential-upstreamref-same-namespace` | `Credential` | `spec.upstreamSecretRef.namespace` must equal `metadata.namespace`. |
| `secretinjectionpolicy-folder-or-org-only` | `SecretInjectionPolicy` | Rejects creation in project namespaces. |
| `secretinjectionpolicy-authn-type-apikey-only` | `SecretInjectionPolicy` | v1alpha1 rejects `spec.callerAuth.type=OIDC`. |
| `secretinjectionpolicybinding-folder-or-org-only` | `SecretInjectionPolicyBinding` | Rejects creation in project namespaces. |
| `secretinjectionpolicybinding-policyref-same-namespace-or-ancestor` | `SecretInjectionPolicyBinding` | Binding's `policyRef` must resolve inside the same scope or an ancestor. |
| `namespace-scope-label-immutable` | `Namespace` | The `console.holos.run/resource-type` label is immutable post-creation and owned by the holos platform controller SA. |

Rejection coverage is validated by the envtest negative-path suite at
`api/secrets/v1alpha1/crd_test.go` (HOL-708).

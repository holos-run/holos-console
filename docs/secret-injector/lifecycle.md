# Credential Lifecycle Contract

`internal/secretinjector/controller` — M2 reference for HOL-747.

## The ownerReference atomicity model

Every materialised `Credential` owns exactly one sibling `v1.Secret` that
carries the JSON-encoded `Envelope` (see [kdf.md](kdf.md) for the envelope
shape). The relationship is a Kubernetes controller ownerReference — set by
`ctrl.SetControllerReference` in the Credential reconciler — which means:

- The hash Secret lists the Credential as its **sole controller-type owner**.
- Kubernetes garbage-collects the hash Secret automatically when the
  Credential is deleted (delete cascade, no operator action required).
- A hash Secret that loses its owner (for example, one orphaned during a
  misconfigured restore) is not re-adopted: `isOwnedByCredential` checks
  the owner UID, not the name, so a Credential deleted and recreated with
  the same name but a fresh UID materialises its own envelope from first
  principles rather than inheriting the previous hash.

The deterministic secret name is `<credentialName>-hash` in the Credential's
own namespace. This allows `kubectl get secrets <credentialName>-hash` to
locate the hash material, but `Status.HashSecretRef` is the authoritative
pointer: callers must consult the status field, not the naming convention,
in case the name derivation changes in a future version.

### Why a single ownerReference

Kubernetes rejects a second controller owner on an existing object (only one
entry may have `controller: true`). The reconciler enforces the same rule
explicitly: if a Secret with the derived name already exists but its owner
UID does not match the current Credential, the reconciler returns an error
rather than clobbering the Secret. This prevents a name-squatting attack
where a rogue Credential's delete triggers GC of another Credential's hash
material via a stolen ownerReference.

## Hash Secret: the data["envelope"] contract

The hash Secret carries one key:

| Key | Value |
|-----|-------|
| `envelope` | JSON-encoded `sicrypto.Envelope` (see [kdf.md](kdf.md)) |

The `envelope` value is **immutable** once written. The reconciler does not
update an existing envelope — it creates a new one if the Secret is missing
or unowned, and otherwise trusts the stored envelope. An operator who edits
the `envelope` value manually breaks the Verify path for that Credential
without surfacing any condition change; the reconciler notices only on the
next login attempt. Manual edits to the hash Secret are unsupported.

GoDoc for the write site (`credential_controller.go`):

```
credentialHashEnvelopeKey = "envelope"
```

The JSON contract, its immutability, and its backing struct are documented
in `internal/secretinjector/crypto/kdf.go` and referenced from this constant.

## Credential lifecycle phases

The reconciler drives the Credential through mutually exclusive phases in
this priority order:

| Phase | Trigger | Hash Secret |
|-------|---------|-------------|
| `Revoked` | `spec.revoked = true` | Deleted eagerly; GC follows |
| `Expired` | `spec.expiresAt` elapsed | Retained (read-only, expired) |
| `Rotating` | A successor exists in rotation group (grace window) | Retained |
| `Retired` | Grace window elapsed | Retained |
| `Active` | Accepted + hash materialised | Created or verified |

Revocation is **terminal**: the reconciler deletes the hash Secret eagerly
(not via GC) so a Revoked Credential cannot be verified against a stale
envelope after the phase change. `DeletePropagationBackground` lets the API
server reclaim the object asynchronously; the reconciler does not wait.

## Delete-cascade semantics

To delete a Credential and its hash material:

```
kubectl delete credential <name> -n <namespace>
```

The garbage collector cascades to the hash Secret via the ownerReference
within the API server's default GC period (typically seconds). No additional
`kubectl delete secret` step is required.

**Backup/restore ordering**: always restore the `v1.Secret`
(`holos-secret-injector-pepper`) containing the pepper material **before**
restoring Credential objects. A Credential whose `status.pepperVersion` names
a version absent from the restored pepper Secret cannot be verified until its
hash is re-derived against the restored (or new) pepper material. The
Credential itself can be restored independently of its hash Secret — the
reconciler will re-materialise the hash Secret on the next reconcile if
`Status.HashSecretRef` is missing or the referenced Secret is gone.

## Admission vs. reconciler enforcement

The M2 invariant set is split across two enforcement layers:

### Admission layer (`config/secret-injector/admission/`)

ValidatingAdmissionPolicies fire at create/update time and are the **first
line of defence**. They reject structurally invalid objects so the reconciler
never sees them:

| Policy | Invariant |
|--------|-----------|
| `credential-authn-type-apikey-only.yaml` | `spec.authentication.type` must be `APIKey` (OIDC reserved) |
| `credential-upstreamref-same-namespace.yaml` | Cross-namespace `spec.upstreamSecretRef` rejected |
| `secretinjectionpolicy-authn-type-apikey-only.yaml` | Same type constraint on SecretInjectionPolicy |
| `secretinjectionpolicybinding-folder-or-org-only.yaml` | Binding scope must be folder or organization |
| `secretinjectionpolicybinding-policyref-same-namespace-or-ancestor.yaml` | Policy ref must be same namespace or ancestor |
| `secretinjectionpolicy-folder-or-org-only.yaml` | Policy scope must be folder or organization |
| `upstreamsecret-project-only.yaml` | UpstreamSecret scope must be project |
| `upstreamsecret-valuetemplate-no-control-chars.yaml` | No control characters in value templates |
| `namespace-scope-label-immutable.yaml` | Scope labels are immutable after creation |

Admission runs with CEL expressions inside the API server; no external
webhook is required.

### Reconciler layer (marshal-scan gate)

The Credential reconciler re-checks the same structural invariants as a
belt-and-braces guard against objects that bypass admission (`kubectl apply
--server-side --force`, direct etcd writes). The re-check is cheaper than
admission because it fires only on objects the reconciler was already going
to process. See `credentialAcceptedCondition` in `credential_controller.go`:
an object that fails the re-check gets `Accepted=False` and stays `NotReady`
without entering the materialisation path.

The **marshal-scan gate** (`internal/secretinjector/controller/invariant_test.go`)
is the automated enforcement point for the dominant "no sensitive values on
CRs" invariant. After every reconcile in the envtest suite, the gate GETs
every CR, marshals it to both JSON and YAML, and asserts that the forbidden
byte patterns from `api/secrets/v1alpha1/invariant_patterns.go` produce zero
matches. The gate covers patterns:

| Pattern | What it detects |
|---------|----------------|
| `api-key-prefix` | `sih_[A-Za-z0-9_-]{20,}` — caller-facing API key on a CR |
| `argon2id-envelope` | `$argon2id$` — PHC-string argon2id envelope on a CR |

A match in either the JSON or YAML form fails the test without printing the
matched bytes (doing so would leak the credential material the invariant is
written to prevent).

### Division summary

```
Admission (create/update)       Reconciler (steady-state)
─────────────────────────       ─────────────────────────
Type constraints                Re-check type constraints (belt-and-braces)
Cross-namespace ref rejection   Status conditions reflect rejected spec
Scope label immutability        Marshal-scan gate: no sensitive bytes on CRs
Control-char guards             OwnerReference atomicity enforcement
```

Invariants that admission owns are **gate invariants**: they prevent bad
objects from entering the API server. Invariants the reconciler enforces are
**runtime invariants**: they prevent bad state from propagating even when
a bad object slips through admission.

## No-sensitive-values invariant (agents reading this cold)

**Every CR in `secrets.holos.run/v1alpha1` is a control object, not a vault.**

CRs carry: references, selectors, lifecycle metadata, phase, and conditions.

CRs NEVER carry: plaintext credential material, hash bytes, salt bytes,
pepper bytes, API key prefixes, last-4 digits, or any truncation of a
credential that reveals non-trivial entropy.

This invariant is not optional. It is enforced by:

1. ValidatingAdmissionPolicies at the API server boundary.
2. The Credential reconciler's `Accepted` condition gate.
3. The marshal-scan test in `invariant_test.go` that runs after every
   reconcile in the envtest suite.

Any agent editing a CR field in `secrets.holos.run/v1alpha1` MUST verify
that the field does not carry sensitive bytes before committing. The
marshal-scan test is the automated gate, but it fires only in the envtest
suite — a field added without a test may only surface in production.

See `api/secrets/v1alpha1/doc.go` for the full rationale and the list of
allowed vs. forbidden field categories.

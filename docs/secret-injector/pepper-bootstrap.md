# Pepper Bootstrap Runbook

`internal/secretinjector/crypto` — M2 reference for HOL-747.

## Why pepper exists

A pepper is a cluster-wide secret value concatenated with the plaintext
before hashing. It defends against offline brute-force in the event an
attacker exfiltrates the hash Secret: without the pepper bytes the attacker
cannot reproduce the argon2id input, so the cost of a brute-force attack is
the full argon2id cost _plus_ a 256-bit random search. Even a nation-state
adversary who steals every hash in the cluster cannot run any useful
offline attack without also stealing the pepper Secret.

The pepper bytes NEVER travel through a CR. They live exclusively in a
`v1.Secret` in the controller's own namespace, protected by a namespaced
RBAC `Role` rather than the cluster-wide `ClusterRole` that governs the
CRDs. This is the same principle documented in `api/secrets/v1alpha1/doc.go`
for credential material: tight RBAC, encryption-at-rest, no audit-log
exposure through `kubectl get -o yaml` on a higher-traffic object class.

## Secret shape

The controller reads from and writes to a single, pinned `v1.Secret`:

```
namespace: <controller namespace>   # resolved from POD_NAMESPACE env var
name:      holos-secret-injector-pepper
type:      Opaque
```

`data` entries follow the pattern `pepper-<N>` where `<N>` is a
positive decimal integer (version 1 is the first seal):

```yaml
# EXAMPLE — values are synthetic placeholders, not real pepper bytes.
# Never copy-paste from a doc; the real bytes are random and opaque.
data:
  pepper-1: <base64-encoded 32-byte random seed — PLACEHOLDER_DO_NOT_COPY>
```

Keys that do not match the `pepper-<positive-int>` format are silently
ignored. This allows future extensions (for example, a `salt-seed` key)
to coexist in the same Secret without corrupting version discovery.

## First-boot self-seal

`Bootstrap` in `internal/secretinjector/crypto/pepper_bootstrap.go` is
called exactly once per manager process, from `controller.Manager.Start`,
before the first `Reconcile` runs. On a missing Secret it:

1. Generates 32 random bytes from `crypto/rand` (256-bit seed).
2. Creates the Secret with `data["pepper-1"] = <seed>`.
3. Returns `BootstrapResult{ActiveVersion: 1, Created: true, BytesLength: 32}`.

The manager logs the result so operators can distinguish a first-boot seal
(`Created: true`) from a warm restart (`Created: false`) at a glance.
Bootstrap never logs the pepper bytes, never returns them, and never exposes
them on the `BootstrapResult` struct — only the integer version and the byte
length appear in telemetry.

If `Create` returns `AlreadyExists` (two manager replicas racing at startup),
Bootstrap re-reads the winner's Secret and reports its active version. The
single-replica deployment is the common case; the two-round-trip path exists
precisely to prevent the losing replica from overwriting the winning pepper.

Bootstrap returns an error and the manager refuses to start if:

- The namespace is empty (the `POD_NAMESPACE` downward-API variable was not
  set on the Deployment — see `cmd/secret-injector/` for the binary entrypoint
  and `config/secret-injector/rbac/namespace/` for the namespace-scoped wiring).
- The Secret exists but carries no `pepper-<N>` data rows (an operator
  manually cleared `.data`).
- The highest-numbered row is empty.

**A failure at Bootstrap is fatal**: the reconciler cannot hash without a
pepper, and falling back to an unpeppered hash would be a silent security
regression. The manager must not report readiness on an unusable pepper.

## RBAC envelope

The controller's `ClusterRole` grants `get` on `core/v1 Secret` in the
controller's own namespace — not `list` or `watch`. Enumeration of Secrets
is the class of vulnerability the service is designed to close (see ADR 031).
The `SecretLoader` that reads the pepper Secret uses a non-cached direct
client (`client.New`, not `mgr.GetClient()`) specifically because a
cache-backed client would lazily start a Secret informer on first `Get`,
requiring `list/watch` that real RBAC forbids.

## Versioning contract

Every `Envelope` stores the integer pepper version (`pepperVersion`) that
was active at `Hash` time. The Credential reconciler passes this string
to `KDF.Hash` and the verifier calls `Loader.Get(ctx, pepperVersion)` to
look up the matching bytes on `Verify`. This means:

- **Multiple pepper versions can coexist** in the Secret's `.data` during a
  rotation window. A credential hashed under version 1 is still verifiable
  while version 2 is active for new hashes.
- **The active version is always the highest integer** present in `.data`.
  `Loader.Active` returns the max-version row and its bytes; new `Hash`
  calls stamp that version onto the envelope.
- **Version integers are monotonically assigned**. The first seal writes
  version 1; a rotation appends version 2, then version 3, and so on.
  Gaps in the sequence are safe but confusing; avoid them.

## Rotation lifecycle (Post-MVP)

The rotation controller that appends a new `pepper-<N+1>` row, triggers
re-hash of all active Credentials, and eventually retires the old row is
a Post-MVP deliverable (tracked under HOL-747). The `SecretLoader`'s read
surface is already rotation-ready: every call fetches the current state of
the Secret, so an external rotation takes effect on the next reconcile
without a manager restart.

Until the rotation controller ships:

1. An operator who needs to rotate manually can append a new `pepper-<N+1>`
   row to the Secret. The `SecretLoader` will report the new version on the
   next call to `Active`. Active Credentials will be re-hashed at next login
   or the next periodic reconcile.
2. Retire an old row only after every Credential that referenced it has been
   re-hashed. An operator who deletes a row prematurely will see
   `ErrPepperVersionNotFound` surface on Verify for any credential still
   carrying the retired version.
3. The backup/restore ordering rule: restore the pepper Secret before
   restoring Credentials. A Credential whose `status.pepperVersion` names a
   version absent from the restored pepper Secret cannot be verified until
   its hash is re-derived from the restored or new pepper material.

## Observability

`BootstrapResult` exposes three telemetry-safe fields:

| Field | Type | Meaning |
|-------|------|---------|
| `ActiveVersion` | `int32` | Highest pepper version in .data |
| `Created` | `bool` | True on first-boot seal; false on warm restart |
| `BytesLength` | `int` | Byte length of active row (32 for a healthy seal) |

None of these fields contain or hint at the pepper bytes themselves.

## Testing

- `internal/secretinjector/crypto/pepper_test.go` — `SecretLoader` unit tests and `Bootstrap` unit tests (cold-start, warm-restart, and race-on-create paths)
- The cross-reconciler envtest suite wires a test pepper seed via
  `suite_test.go` and verifies that no pepper bytes appear on any CR via
  the marshal-scan gate.

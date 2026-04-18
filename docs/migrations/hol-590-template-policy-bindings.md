# Migration: TemplatePolicyRule.Target globs -> TemplatePolicyBindings

This runbook covers the one-shot cluster migration added in HOL-599. Run it once per environment before rolling out HOL-600, which removes the legacy `TemplatePolicyRule.Target.{project_pattern, deployment_pattern}` evaluation path.

## Background

Prior to HOL-590, `TemplatePolicy` selected its render targets via two glob fields on every rule:

- `target.project_pattern` — matched against the project slug.
- `target.deployment_pattern` — matched against the deployment name; when empty, the rule matched both project-scope templates and deployments.

HOL-590 replaced that selector with an explicit `TemplatePolicyBinding` resource: a named attachment from a single `TemplatePolicy` to a concrete list of `(kind, project_name, name)` render targets. The resolver (HOL-596) already evaluates bindings alongside the legacy globs; this migration translates any currently-populated glob into an equivalent binding, then clears the glob fields on the policy so HOL-600 can delete the evaluation path safely.

## When to run

Run this migration exactly once per environment, **after** the binding handler, resolver, and UI have landed (HOL-592..598) and **before** HOL-600 is merged.

A cluster where every `TemplatePolicy` has empty `Target` fields does not need this migration. The command is still safe to run against such a cluster — it reports `0 plans, N skipped` and exits clean.

## Command

```
holos-console migrate template-policy-targets [--apply]
```

Defaults:

- `--dry-run` is implied. The command prints the plan it would execute and exits without mutating the cluster. Re-run with `--apply` to mutate.
- The namespace-prefix flags (`--namespace-prefix`, `--organization-prefix`, `--folder-prefix`, `--project-prefix`) are inherited from the root `holos-console` command. If your deployment uses non-default prefixes, pass them on the command line.

Kubernetes credentials are discovered the same way the server discovers them: in-cluster first, then `KUBECONFIG`. The command fails fast with `no kubernetes config available` if neither is configured.

### Dry-run

```
holos-console migrate template-policy-targets
```

Emits one block per policy with non-empty `Target` globs:

```
policy holos-org-acme/audit -> binding holos-org-acme/audit-migrated (scope=organization, scope_name=acme)
  targets:
    - kind=deployment project=lilies name=api
    - kind=deployment project=roses name=api
  status: will create new binding and clear policy Target globs

Summary: 0 would create bindings; would clear targets on 0 policies; 0 skipped (already migrated); 0 conflicts.
Re-run with --apply to mutate the cluster.
```

### Apply

```
holos-console migrate template-policy-targets --apply
```

Re-runs the same enumeration, then:

1. Creates each missing `TemplatePolicyBinding` with the computed target set.
2. Clears the `target.project_pattern` and `target.deployment_pattern` fields on every rule in the originating policy (all other rule fields are preserved verbatim).

The final summary line distinguishes dry-run from apply:

```
Summary: 3 created bindings; cleared targets on 3 policies; 1 skipped (already migrated); 0 conflicts.
```

## What the command does, in detail

1. Lists every namespace carrying the `app.kubernetes.io/managed-by=console.holos.run` label.
2. Classifies each namespace as organization, folder, or project using the same prefix scheme the runtime resolver uses.
3. For each organization and folder namespace, lists `TemplatePolicy` ConfigMaps (`console.holos.run/resource-type=template-policy`).
4. Skips any policy whose rules already have empty `Target` globs — such a policy is either newly authored under HOL-598's UI or has already been migrated on a previous run.
5. For each remaining policy, enumerates the descendant project namespaces (by walking the `console.holos.run/parent` label chain), then for each rule with a non-empty `Target`:
   - Matches `project_pattern` against each descendant project slug.
   - When `deployment_pattern` is empty, selects every project-scope template and every deployment in the matched projects.
   - When `deployment_pattern` is non-empty, selects only deployments whose name matches the pattern in the matched projects.
6. Skips project namespaces carrying a non-nil `metadata.deletionTimestamp` so the migrator's descendant set matches the runtime topology's (HOL-570). Binding targets that live in terminating namespaces would never have activated under the legacy glob evaluation path.
7. Deduplicates the collected `(kind, project_name, name)` triples into a sorted target list.
8. If the target list is empty (the globs currently match no live render targets), skips binding creation — the binding handler rejects an empty `target_refs` list and writing one would leave an uneditable ConfigMap artifact. The policy's `Target` fields are still cleared on `--apply` because the globs matched nothing under the legacy path either, so clearing is semantics-preserving.
9. Looks up any existing binding named `<policy-name>-migrated` in the policy's namespace:
   - If the binding already exists with the same target set, leaves it alone (and only clears the policy's `Target` fields on `--apply`).
   - If the binding exists with a different target set, records a `CONFLICT` in the plan and leaves the policy untouched.
   - If no binding exists (and the target list is non-empty), creates one on `--apply`.
10. On `--apply`, creates the binding first (when needed), then clears the policy's `Target` fields.

## Idempotency

Re-running the command is always safe:

- A policy whose `Target` fields are already empty is skipped entirely. The command reports it under `N skipped (already migrated)`.
- A binding with the deterministic name `<policy-name>-migrated` that already carries the expected target set is re-used. The command does not attempt to recreate it and does not attempt to mutate it.
- A partial run that created the binding but failed before clearing the policy retries cleanly: the next invocation finds the binding, verifies the target set matches, and proceeds directly to clearing the policy.
- A policy whose globs match no live render targets is cleared without producing a binding. A subsequent run sees the cleared Target fields and classifies the policy as already migrated.

## Operator actions after a conflict

A `CONFLICT` row means an existing binding has the expected name but different target refs. The command refuses to overwrite the binding so an operator can inspect it and decide how to proceed:

- If the existing binding is correct, rename or delete the policy's `Target` fields by hand and re-run this migration — the policy will then be classified as "already migrated" and skipped.
- If the existing binding is wrong, delete it and re-run with `--apply` to have the migrator recreate it.

A conflict row can also indicate a **name collision with a non-binding ConfigMap** (for example a leftover Template named `<policy>-migrated`). The plan note identifies this case explicitly and names the colliding `resource-type`. Delete or rename the offending ConfigMap and re-run the migration; the migrator does not overwrite objects it did not create.

## Operator actions after an ancestry error

The command fails fast when a managed project namespace's parent chain is broken — missing `console.holos.run/parent` label, a parent that is not in the managed namespace index, or a cycle. Fixing the error before the migration runs is required because silently dropping the project from the descendant set would allow the migrator to clear policy `Target` globs while skipping legitimate bindings, which would permanently remove policy coverage once HOL-600 lands.

Typical recovery steps:

- Inspect the failing namespace reported in the error message (`walking ancestors of "holos-prj-<slug>"`).
- Restore the missing or cyclic `console.holos.run/parent` label so the chain terminates at the owning organization namespace.
- Re-run the migration — the migrator re-walks the chain and proceeds once the ancestry is consistent.

## Semantic caveat

Pre-HOL-590 rule-level `Target` globs could select a narrower set per rule than the policy as a whole. The binding model is per-policy: a single binding covers all of its bound policy's rules for every target it names. If a policy has multiple rules with disjoint `Target` globs, the migration produces one binding with the union of all matched targets. Post-migration every rule applies to every target named on that union — which may broaden some rules relative to their pre-migration glob. In practice platform engineers author policy rules to cover the same target set, so this broadening is uncommon; if your cluster has exotic per-rule targets, review the printed plan before running `--apply` and split the policy into multiple narrower policies beforehand.

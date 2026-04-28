# Default Folder Annotation Cleanup (2026-04)

## Summary

The `console.holos.run/default-folder` organization annotation was removed from
the console contract when project namespaces became direct children of their
organization namespace. Existing clusters may still have the stale annotation on
organization namespaces created by older console versions.

The cleanup tool ships as `cmd/holos-console-migrate-default-folder`. It is
operator-run and strips only `console.holos.run/default-folder` from
console-managed organization namespaces.

## Preconditions

1. Run this after deploying the console image that no longer reads or writes the
   default-folder annotation.
2. Capture the current organization namespace annotations before applying:

   ```bash
   for ns in $(kubectl get ns \
     -l app.kubernetes.io/managed-by=console.holos.run \
     -l console.holos.run/resource-type=organization -o name); do
     name="${ns#namespace/}"
     kubectl get namespace "$name" \
       -o jsonpath='{.metadata.annotations}' \
       > "/tmp/backup-${name}-annotations.json"
   done
   ```

## Run Order

1. Dry-run the cleanup:

   ```bash
   holos-console-migrate-default-folder
   ```

   The tool prints one row per console-managed organization namespace:

   | NAMESPACE | DEFAULT-FOLDER-FOUND | DEFAULT-FOLDER-STRIPPED |
   | --------- | -------------------- | ----------------------- |
   | holos-org-acme | true | true |

   In dry-run mode, `DEFAULT-FOLDER-STRIPPED=true` means the tool would remove
   the annotation.

2. Apply the cleanup:

   ```bash
   holos-console-migrate-default-folder --apply
   ```

3. Validate manually:

   ```bash
   kubectl get ns \
     -l app.kubernetes.io/managed-by=console.holos.run \
     -l console.holos.run/resource-type=organization \
     -o json \
     | jq -r '.items[]
       | select(.metadata.annotations["console.holos.run/default-folder"] != null)
       | .metadata.name'
   ```

   The command must print nothing.

## Completion Assertion

When `--apply` is set, the tool re-lists every console-managed organization
namespace before exiting and fails if any still carries
`console.holos.run/default-folder`. A successful apply therefore means the
cluster has no remaining organization namespace tombstones for this annotation.

## Rollback

The migration removes one annotation key and does not create replacement
objects. To roll back, restore the backed-up annotations for affected
organization namespaces:

```bash
for f in /tmp/backup-*-annotations.json; do
  name="$(basename "$f" -annotations.json | sed 's/^backup-//')"
  kubectl annotate --overwrite namespace "$name" \
    "$(jq -r 'to_entries | map("\(.key)=\(.value)") | .[]' "$f")"
done
```

After rollback, investigate the failed run and re-run the cleanup once the
cluster is healthy.

## Related

- Parent: [HOL-1091](https://linear.app/holos-run/issue/HOL-1091) — remove
  default_folder functionality entirely.
- This phase: [HOL-1096](https://linear.app/holos-run/issue/HOL-1096) —
  drop the annotation constant, admission policy references, and migrate live
  cluster state.

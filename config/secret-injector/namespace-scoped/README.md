# holos-secret-injector namespace-scoped overlay

This overlay installs the namespace-local resources for the
holos-secret-injector controller in `holos-system`:

- `ServiceAccount` the controller runs as.
- `Role` + `RoleBinding` granting CUD on `core/v1 Secret` within this
  namespace (the hash-material Secret envelope the M2 reconciler owns).

**M1 deferral.** The controller `Deployment` and any `Service` exposing
it are M2 scope per parent plan HOL-675. They will be added here in
the HOL-670 successor tickets. Cluster-wide resources (CRDs, VAPs,
ClusterRole, ClusterRoleBinding) live in `../cluster-scoped/`.

## Apply order

```sh
kubectl apply -k config/secret-injector/namespace-scoped/
kubectl apply -k config/secret-injector/cluster-scoped/
```

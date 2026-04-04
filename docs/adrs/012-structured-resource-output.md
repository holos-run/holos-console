# ADR 012: Structured Resource Output for CUE Templates

## Status

Accepted

## Context

CUE deployment templates currently produce a flat list of Kubernetes resource
manifests via the `resources` field:

```cue
resources: [
    { kind: "ServiceAccount", ... },
    { kind: "Deployment", ... },
    { kind: "Service", ... },
]
```

The console iterates this list, validates each element, and applies them to the
cluster. This flat structure works for the current set of namespaced resources,
but has limitations:

1. **No distinction between namespaced and cluster-scoped resources.** The
   current validation requires every resource to have a `metadata.namespace`
   matching the project namespace. This makes it impossible for templates to
   produce cluster-scoped resources like `Namespace`, `ClusterRole`,
   `ClusterRoleBinding`, or `PriorityClass` that platform teams need.

2. **No structural guarantees about uniqueness.** A flat list can contain
   duplicate Kind/name combinations that would conflict at apply time. The
   error surfaces only during the Kubernetes API call, not during CUE
   evaluation.

3. **Planned platform input.** A second input (`platform: #PlatformInput`) is
   planned for platform-mandated configuration. Platform policy often requires
   cluster-scoped resources (e.g., `ClusterRoleBinding` for pod security,
   `ResourceQuota` at the namespace level). The output structure must
   accommodate both user-scoped and platform-scoped resources cleanly.

## Decisions

### 1. Output resources are organized into two categories: namespaced and cluster.

The template output is refactored from a flat `resources` list to two
structured fields:

```cue
// namespaced organizes resources that live within a Kubernetes namespace.
// Structure: namespaced.<namespace>.<Kind>.<name>
namespaced: [Namespace=string]: [Kind=string]: [Name=string]: {
    apiVersion: string
    kind:       Kind
    metadata: {
        name:      Name
        namespace: Namespace
        ...
    }
    ...
}

// cluster organizes resources that are cluster-scoped (no namespace).
// Structure: cluster.<Kind>.<name>
cluster: [Kind=string]: [Name=string]: {
    apiVersion: string
    kind:       Kind
    metadata: {
        name: Name
        ...
    }
    ...
}
```

### 2. Namespaced resources use a three-level nested struct.

Namespaced resources are organized as:

```
namespaced.<namespace>.<Kind>.<name>
```

For example, `namespaced.example.Deployment.myapp` represents the `myapp`
Deployment in the `example` namespace. This structure:

- Enforces uniqueness per Kind/name within a namespace at the CUE level.
- Groups related resources by namespace, making multi-namespace templates
  possible in the future.
- Allows CUE constraints to enforce that the namespace key matches
  `metadata.namespace`.

Example:

```cue
namespaced: (input.namespace): {
    ServiceAccount: (input.name): {
        apiVersion: "v1"
        kind:       "ServiceAccount"
        metadata: {
            name:      input.name
            namespace: input.namespace
            labels:    _labels
        }
    }
    Deployment: (input.name): {
        apiVersion: "apps/v1"
        kind:       "Deployment"
        metadata: {
            name:      input.name
            namespace: input.namespace
            labels:    _labels
        }
        spec: { ... }
    }
    Service: (input.name): {
        apiVersion: "v1"
        kind:       "Service"
        metadata: {
            name:      input.name
            namespace: input.namespace
            labels:    _labels
        }
        spec: { ... }
    }
}
```

### 3. Cluster-scoped resources use a two-level nested struct.

Cluster resources are organized as:

```
cluster.<Kind>.<name>
```

For example, `cluster.Namespace.example` represents the `example` Namespace.
Cluster resources have no namespace key since they are not scoped to one.

Example:

```cue
cluster: {
    Namespace: (input.namespace): {
        apiVersion: "v1"
        kind:       "Namespace"
        metadata: {
            name:   input.namespace
            labels: _labels
        }
    }
    ClusterRoleBinding: "\(input.name)-psp": {
        apiVersion: "rbac.authorization.k8s.io/v1"
        kind:       "ClusterRoleBinding"
        metadata: {
            name:   "\(input.name)-psp"
            labels: _labels
        }
        // ...
    }
}
```

### 4. The Go renderer walks both output fields.

The `CueRenderer` is updated to:

1. Look up `namespaced` — iterate namespace keys, then Kind keys, then name
   keys, collecting each leaf as an `unstructured.Unstructured`.
2. Look up `cluster` — iterate Kind keys, then name keys, collecting each leaf.
3. Apply separate validation rules:
   - **Namespaced resources**: must have `metadata.namespace` matching the
     struct key and the project namespace (unless multi-namespace is enabled in
     a future extension). Kind must be in the namespaced allowlist.
   - **Cluster resources**: must NOT have `metadata.namespace`. Kind must be in
     the cluster allowlist (initially empty; extended as cluster resource
     support is added).
4. Return both sets of resources for the `Applier` to handle.

### 5. The flat `resources` list is removed.

The `resources` field is removed in the new interface. Since the code is not
yet released, there is no backwards-compatibility requirement. All templates
(default and user-created) must be migrated to the structured format.

### 6. CUE struct keys enforce consistency with metadata.

CUE constraints ensure the struct keys match the resource metadata:

```cue
namespaced: [Namespace=string]: [Kind=string]: [Name=string]: {
    kind: Kind
    metadata: {
        name:      Name
        namespace: Namespace
    }
}

cluster: [Kind=string]: [Name=string]: {
    kind: Kind
    metadata: name: Name
}
```

This means a mismatch between the struct path and the resource metadata is a
CUE evaluation error, caught before any Kubernetes API call.

## Consequences

### Positive

- **Uniqueness enforced structurally.** Duplicate Kind/name combinations within
  a namespace are impossible — CUE structs merge or conflict at evaluation time.
- **Clear separation of concerns.** Namespaced vs. cluster resources have
  distinct validation rules and apply strategies.
- **Foundation for platform input.** Platform-mandated cluster resources
  (ClusterRole, ResourceQuota, etc.) have a natural home in the `cluster`
  field.
- **Self-documenting paths.** `namespaced.holos-prj-api.Deployment.myapp` is
  immediately readable — you know the namespace, kind, and name from the path.

### Negative

- **More verbose template syntax.** The nested struct is more typing than a
  flat list. Mitigated by CUE's structural typing — the constraints catch
  errors that the flat list would only surface at apply time.
- **Migration required.** All existing templates must be updated. Since the
  code is unreleased, this is a one-time cost with no user impact.

### Risks

- **Multi-namespace templates.** The `namespaced` struct key allows resources
  in multiple namespaces, but the current validation restricts all namespaced
  resources to the project namespace. If this restriction is relaxed in the
  future, careful RBAC checks per namespace will be needed.
- **Cluster resource RBAC.** Allowing cluster-scoped resources requires a new
  authorization model — the current project-scoped RBAC does not cover
  cluster-level operations. The initial implementation should keep the cluster
  allowlist empty and extend it incrementally.

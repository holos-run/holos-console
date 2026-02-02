# ADR 006: Namespace Prefix Resolution Is Internal Service Behavior

## Status

Accepted

## Context

The console server manages Kubernetes Namespace objects on behalf of users. Each namespace name is constructed from configurable prefixes (`--namespace-prefix`, `--organization-prefix`, `--project-prefix`) combined with the user-facing resource name. For example, with defaults `holos-`, `org-`, and `prj-`, an organization named `acme` maps to the namespace `holos-org-acme`.

The current implementation exposes prefix internals to callers in two ways: (1) `OrgFromNamespace`/`ProjectFromNamespace` use `strings.TrimPrefix` which silently returns garbage when a namespace name does not match the expected prefix, and (2) `buildOrganization` always parses namespace names instead of reading from labels, leaking the prefix scheme into API responses.

## Decisions

1. **Namespace prefix configuration is internal service behavior, not exposed to callers.** API consumers operate exclusively with logical resource names (e.g. `acme`, `my-project`). The mapping between logical names and Kubernetes namespace names is an implementation detail of the server. No prefix value or namespace name is ever returned in API responses.

2. **The resolver uses configured prefix values when constructing and parsing namespace names.** `OrgNamespace`/`ProjectNamespace` construct names by concatenating prefixes. `OrgFromNamespace`/`ProjectFromNamespace` parse names by validating and stripping the expected prefix. Labels (`console.holos.run/organization`, `console.holos.run/project`) remain the authoritative source for resource names and must be preferred by callers when available.

3. **Namespaces that do not match the configured prefix are silently filtered from list operations.** When listing organizations or projects, any namespace whose name does not begin with the expected full prefix (`{NamespacePrefix}{OrganizationPrefix}` or `{NamespacePrefix}{ProjectPrefix}`) must be excluded from results. A `slog.Debug` message must be logged recording the namespace name and the reason it was filtered (prefix mismatch). This ensures that namespaces from other console instances sharing the same cluster (with different prefix configurations) or manually-created namespaces do not leak into API responses.

4. **Resolving a namespace name that does not conform to the relevant prefix is a typed error.** `OrgFromNamespace` and `ProjectFromNamespace` must return an error when the namespace name does not have the expected prefix. The error type is `*PrefixMismatchError`, exported from the `resolver` package, with the following shape:

   ```go
   // PrefixMismatchError is returned when a namespace name does not begin with
   // the expected prefix for the resource type being resolved.
   type PrefixMismatchError struct {
       Namespace string // the namespace name that was checked
       Prefix    string // the expected prefix that was not found
   }

   func (e *PrefixMismatchError) Error() string {
       return fmt.Sprintf("namespace %q does not match expected prefix %q", e.Namespace, e.Prefix)
   }
   ```

   Callers use `errors.As` to detect this error and decide whether to filter, skip, or propagate it.

## Consequences

- API responses never contain prefix artifacts; logical names come from labels or validated prefix stripping.
- Multi-instance clusters (different `--namespace-prefix` values) are safe: each instance only sees its own namespaces.
- Callers of `OrgFromNamespace`/`ProjectFromNamespace` must handle the new error return. This is a breaking change to the resolver API but is confined to internal packages.
- List operations become slightly more defensive, filtering at the resolver layer rather than relying solely on label selectors.

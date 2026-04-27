// Package legacy provides shared parsers for the pre-RBAC sharing
// annotations stored on v1.Secret and v1.Namespace objects.
//
// Deprecated: This package exists solely for the one-time migration tool in
// `cmd/holos-console-migrate-rbac`. New code must not import it. Once all
// clusters have been migrated (annotations stripped and RoleBindings
// reconciled), this package and the migration tool will be deleted.
//
// Before ADR 036 moved authorization to native Kubernetes RBAC with OIDC
// impersonation, holos-console encoded sharing grants as JSON arrays in the
// `console.holos.run/share-users`, `console.holos.run/share-roles`,
// `console.holos.run/default-share-users`, and
// `console.holos.run/default-share-roles` annotations. The
// per-resource handler packages (organizations, folders, projects,
// secrets) each open-coded an identical parser. This package centralises
// the unmarshalling so the migration tool in
// `cmd/holos-console-migrate-rbac` can reuse the exact same decoder
// without dragging in a handler package.
//
// The package is intentionally tiny and dependency-free: it imports only
// the API constants from `api/v1alpha2` and the `secrets.AnnotationGrant`
// shape so the on-disk representation stays in sync with the existing
// reconciler.
package legacy

import (
	"encoding/json"
	"fmt"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/secrets"
)

// AnnotationKeys returns the four legacy sharing annotation keys handled
// by this package. The order is stable (share-users, share-roles,
// default-share-users, default-share-roles) so callers iterating the
// result slice produce deterministic output.
func AnnotationKeys() []string {
	return []string{
		v1alpha2.AnnotationShareUsers,
		v1alpha2.AnnotationShareRoles,
		v1alpha2.AnnotationDefaultShareUsers,
		v1alpha2.AnnotationDefaultShareRoles,
	}
}

// ParseGrants unmarshals the JSON array stored in annotations[key].
//
// Returns:
//   - nil, nil when annotations is nil or the key is absent;
//   - the decoded slice when the value is a valid JSON array;
//   - a wrapped error including the annotation key on malformed JSON.
//
// The function never silently skips invalid JSON: a malformed annotation
// must be visible to the operator running the migration so that they can
// hand-correct the data before the RoleBindings are written.
func ParseGrants(annotations map[string]string, key string) ([]secrets.AnnotationGrant, error) {
	if annotations == nil {
		return nil, nil
	}
	value, ok := annotations[key]
	if !ok {
		return nil, nil
	}
	var grants []secrets.AnnotationGrant
	if err := json.Unmarshal([]byte(value), &grants); err != nil {
		return nil, fmt.Errorf("invalid %s annotation: %w", key, err)
	}
	return grants, nil
}

// ShareUsers returns the parsed share-users grants for the given
// annotations map (typically taken from a v1.Secret or v1.Namespace).
func ShareUsers(annotations map[string]string) ([]secrets.AnnotationGrant, error) {
	return ParseGrants(annotations, v1alpha2.AnnotationShareUsers)
}

// ShareRoles returns the parsed share-roles grants for the given
// annotations map.
func ShareRoles(annotations map[string]string) ([]secrets.AnnotationGrant, error) {
	return ParseGrants(annotations, v1alpha2.AnnotationShareRoles)
}

// DefaultShareUsers returns the parsed default-share-users grants for the
// given annotations map. These appear on org, folder, and project
// namespaces and seed the cascade chain applied to new Secrets.
func DefaultShareUsers(annotations map[string]string) ([]secrets.AnnotationGrant, error) {
	return ParseGrants(annotations, v1alpha2.AnnotationDefaultShareUsers)
}

// DefaultShareRoles returns the parsed default-share-roles grants for the
// given annotations map.
func DefaultShareRoles(annotations map[string]string) ([]secrets.AnnotationGrant, error) {
	return ParseGrants(annotations, v1alpha2.AnnotationDefaultShareRoles)
}

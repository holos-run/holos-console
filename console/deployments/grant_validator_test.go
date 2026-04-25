package deployments_test

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	"github.com/holos-run/holos-console/console/deployments"
)

// makeCache returns a TemplateGrantCache pre-loaded with the provided grants
// and namespaces.
func makeCache(grants []v1alpha1.TemplateGrant, namespaces []corev1.Namespace) *deployments.TemplateGrantCache {
	c := deployments.NewTemplateGrantCache()
	c.SetGrants(grants)
	c.SetNamespaces(namespaces)
	return c
}

// makeNS returns a Namespace with the given name and labels.
func makeNS(name string, lbls map[string]string) corev1.Namespace {
	return corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: lbls,
		},
	}
}

// makeGrant returns a TemplateGrant in the given namespace with a single From
// entry.
func makeGrant(ns, name string, from []v1alpha1.TemplateGrantFromRef, to []v1alpha1.LinkedTemplateRef) v1alpha1.TemplateGrant {
	return v1alpha1.TemplateGrant{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      name,
		},
		Spec: v1alpha1.TemplateGrantSpec{
			From: from,
			To:   to,
		},
	}
}

// ref returns a LinkedTemplateRef for the given namespace and name.
func ref(ns, name string) v1alpha1.LinkedTemplateRef {
	return v1alpha1.LinkedTemplateRef{Namespace: ns, Name: name}
}

// TestGrantValidator_SameNamespace asserts that same-namespace references are
// always allowed without consulting the cache.
func TestGrantValidator_SameNamespace(t *testing.T) {
	// Even a nil cache must allow same-namespace references.
	var c *deployments.TemplateGrantCache
	dependent := ref("org-acme", "my-deployment")
	requires := ref("org-acme", "some-template")
	if err := c.ValidateGrant(context.Background(), dependent, requires); err != nil {
		t.Fatalf("same-namespace reference should always be allowed; got %v", err)
	}
}

// TestGrantValidator_LiteralMatch asserts that a TemplateGrant with a literal
// From.Namespace permits the named dependent namespace.
func TestGrantValidator_LiteralMatch(t *testing.T) {
	grant := makeGrant("org-acme", "grant-1",
		[]v1alpha1.TemplateGrantFromRef{{Namespace: "prj-alpha"}},
		nil, // all templates reachable
	)
	c := makeCache([]v1alpha1.TemplateGrant{grant}, nil)

	dependent := ref("prj-alpha", "dep")
	requires := ref("org-acme", "base-template")

	if err := c.ValidateGrant(context.Background(), dependent, requires); err != nil {
		t.Fatalf("literal match should be allowed; got %v", err)
	}
}

// TestGrantValidator_WildcardMatch asserts that a TemplateGrant with
// From.Namespace == "*" permits any dependent namespace (HOL-767).
func TestGrantValidator_WildcardMatch(t *testing.T) {
	grant := makeGrant("org-acme", "grant-wildcard",
		[]v1alpha1.TemplateGrantFromRef{{Namespace: "*"}},
		nil,
	)
	c := makeCache([]v1alpha1.TemplateGrant{grant}, nil)

	for _, depNS := range []string{"prj-alpha", "prj-beta", "fld-shared", "some-random-ns"} {
		dependent := ref(depNS, "dep")
		requires := ref("org-acme", "base-template")
		if err := c.ValidateGrant(context.Background(), dependent, requires); err != nil {
			t.Fatalf("wildcard should permit namespace %q; got %v", depNS, err)
		}
	}
}

// TestGrantValidator_LabelSelectorMatch asserts that a TemplateGrant with a
// NamespaceSelector permits dependent namespaces whose labels match the selector.
func TestGrantValidator_LabelSelectorMatch(t *testing.T) {
	grant := makeGrant("org-acme", "grant-selector",
		[]v1alpha1.TemplateGrantFromRef{
			{
				Namespace: "*",
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"console.holos.run/resource-type": "project",
					},
				},
			},
		},
		nil,
	)
	ns := makeNS("prj-alpha", map[string]string{
		"console.holos.run/resource-type": "project",
	})
	c := makeCache([]v1alpha1.TemplateGrant{grant}, []corev1.Namespace{ns})

	dependent := ref("prj-alpha", "dep")
	requires := ref("org-acme", "base-template")

	if err := c.ValidateGrant(context.Background(), dependent, requires); err != nil {
		t.Fatalf("label selector match should be allowed; got %v", err)
	}
}

// TestGrantValidator_LabelSelectorMismatch asserts that a TemplateGrant with a
// NamespaceSelector rejects a namespace whose labels do not match.
func TestGrantValidator_LabelSelectorMismatch(t *testing.T) {
	grant := makeGrant("org-acme", "grant-selector",
		[]v1alpha1.TemplateGrantFromRef{
			{
				Namespace: "*",
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"console.holos.run/resource-type": "project",
					},
				},
			},
		},
		nil,
	)
	// Folder namespace — label does not match the selector.
	ns := makeNS("fld-alpha", map[string]string{
		"console.holos.run/resource-type": "folder",
	})
	c := makeCache([]v1alpha1.TemplateGrant{grant}, []corev1.Namespace{ns})

	dependent := ref("fld-alpha", "dep")
	requires := ref("org-acme", "base-template")

	err := c.ValidateGrant(context.Background(), dependent, requires)
	if err == nil {
		t.Fatal("label selector mismatch should be rejected; got nil error")
	}
	var notFound *deployments.GrantNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected *GrantNotFoundError; got %T: %v", err, err)
	}
}

// TestGrantValidator_MissingGrant asserts that a cross-namespace reference with
// no matching TemplateGrant is rejected with *GrantNotFoundError.
func TestGrantValidator_MissingGrant(t *testing.T) {
	c := makeCache(nil, nil) // empty cache

	dependent := ref("prj-alpha", "dep")
	requires := ref("org-acme", "base-template")

	err := c.ValidateGrant(context.Background(), dependent, requires)
	if err == nil {
		t.Fatal("missing grant should be rejected; got nil error")
	}
	var notFound *deployments.GrantNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected *GrantNotFoundError; got %T: %v", err, err)
	}
	if notFound.DependentNamespace != "prj-alpha" {
		t.Errorf("DependentNamespace=%q want %q", notFound.DependentNamespace, "prj-alpha")
	}
	if notFound.RequiresNamespace != "org-acme" {
		t.Errorf("RequiresNamespace=%q want %q", notFound.RequiresNamespace, "org-acme")
	}
	if notFound.RequiresName != "base-template" {
		t.Errorf("RequiresName=%q want %q", notFound.RequiresName, "base-template")
	}
}

// TestGrantValidator_DeletedGrantRejectedForNewMaterialisations verifies that
// once a grant is removed from the cache (simulating a delete event processed
// by the controller), new cross-namespace references through it are rejected.
func TestGrantValidator_DeletedGrantRejectedForNewMaterialisations(t *testing.T) {
	grant := makeGrant("org-acme", "grant-1",
		[]v1alpha1.TemplateGrantFromRef{{Namespace: "prj-alpha"}},
		nil,
	)
	c := makeCache([]v1alpha1.TemplateGrant{grant}, nil)

	dependent := ref("prj-alpha", "dep")
	requires := ref("org-acme", "base-template")

	// Before delete: allowed.
	if err := c.ValidateGrant(context.Background(), dependent, requires); err != nil {
		t.Fatalf("pre-delete: expected allowed; got %v", err)
	}

	// Simulate TemplateGrant deletion by replacing the grants list with an
	// empty snapshot (what the controller does on delete event).
	c.SetGrants(nil)

	// After delete: new materialisation through the deleted grant is rejected.
	err := c.ValidateGrant(context.Background(), dependent, requires)
	if err == nil {
		t.Fatal("post-delete: new materialisation should be rejected; got nil error")
	}
	var notFound *deployments.GrantNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected *GrantNotFoundError; got %T: %v", err, err)
	}
}

// TestGrantValidator_DeletedGrantPreservesExistingMaterialisations documents
// the hard-revoke contract: the validator does not cascade deletes. "Existing
// materialised dependency Deployments are preserved" means ValidateGrant does
// not touch or enumerate existing Deployments — this test asserts that the
// validator's only job is to allow/deny new cross-namespace references and does
// not have any side effects on existing Deployments. The contract is enforced
// at the documentation level here; the controller and the reconciler that calls
// ValidateGrant are responsible for not cascading.
func TestGrantValidator_DeletedGrantPreservesExistingMaterialisations(t *testing.T) {
	// This test is a documentation/coverage test. It verifies that
	// ValidateGrant's reject path returns only an error and has no other
	// observable side effect. Callers observe the error and decide not to
	// create a new Deployment; they do NOT delete existing ones.
	c := makeCache(nil, nil)

	dependent := ref("prj-alpha", "dep")
	requires := ref("org-acme", "base-template")

	err := c.ValidateGrant(context.Background(), dependent, requires)
	if err == nil {
		t.Fatal("expected rejection; got nil")
	}
	// The only observable output is the error — no Deployments were touched.
	var notFound *deployments.GrantNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected *GrantNotFoundError; got %T", err)
	}
}

// TestGrantValidator_ToListNarrowsScope verifies that a grant with a non-empty
// `to` list only permits references to the listed templates.
func TestGrantValidator_ToListNarrowsScope(t *testing.T) {
	grant := makeGrant("org-acme", "grant-narrow",
		[]v1alpha1.TemplateGrantFromRef{{Namespace: "prj-alpha"}},
		[]v1alpha1.LinkedTemplateRef{
			{Namespace: "org-acme", Name: "allowed-template"},
		},
	)
	c := makeCache([]v1alpha1.TemplateGrant{grant}, nil)

	dependent := ref("prj-alpha", "dep")

	// Allowed template.
	allowed := ref("org-acme", "allowed-template")
	if err := c.ValidateGrant(context.Background(), dependent, allowed); err != nil {
		t.Fatalf("to-list: allowed template should be permitted; got %v", err)
	}

	// Not-listed template — should be rejected.
	denied := ref("org-acme", "other-template")
	if err := c.ValidateGrant(context.Background(), dependent, denied); err == nil {
		t.Fatal("to-list: non-listed template should be rejected; got nil error")
	}
}

// TestGrantValidator_NilCacheDefaultDeny asserts that a nil *TemplateGrantCache
// safely default-denies cross-namespace references.
func TestGrantValidator_NilCacheDefaultDeny(t *testing.T) {
	var c *deployments.TemplateGrantCache
	dependent := ref("prj-alpha", "dep")
	requires := ref("org-acme", "base-template")

	err := c.ValidateGrant(context.Background(), dependent, requires)
	if err == nil {
		t.Fatal("nil cache: cross-namespace reference should be denied; got nil error")
	}
	var notFound *deployments.GrantNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected *GrantNotFoundError; got %T", err)
	}
}

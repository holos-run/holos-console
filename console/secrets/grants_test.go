package secrets

import (
	"testing"
)

func TestDeduplicateGrants_MergesDuplicates(t *testing.T) {
	grants := []AnnotationGrant{
		{Principal: "alice@example.com", Role: "editor"},
		{Principal: "alice@example.com", Role: "viewer"},
	}
	result := DeduplicateGrants(grants)
	if len(result) != 1 {
		t.Fatalf("expected 1 grant, got %d", len(result))
	}
	if result[0].Principal != "alice@example.com" {
		t.Errorf("expected principal alice@example.com, got %s", result[0].Principal)
	}
	if result[0].Role != "editor" {
		t.Errorf("expected role editor, got %s", result[0].Role)
	}
}

func TestDeduplicateGrants_KeepsHighestRole(t *testing.T) {
	grants := []AnnotationGrant{
		{Principal: "alice@example.com", Role: "viewer"},
		{Principal: "alice@example.com", Role: "owner"},
	}
	result := DeduplicateGrants(grants)
	if len(result) != 1 {
		t.Fatalf("expected 1 grant, got %d", len(result))
	}
	if result[0].Role != "owner" {
		t.Errorf("expected role owner, got %s", result[0].Role)
	}
}

func TestDeduplicateGrants_PreservesTimeFields(t *testing.T) {
	nbf := int64(1000)
	exp := int64(2000)
	grants := []AnnotationGrant{
		{Principal: "alice@example.com", Role: "viewer", Nbf: nil, Exp: nil},
		{Principal: "alice@example.com", Role: "owner", Nbf: &nbf, Exp: &exp},
	}
	result := DeduplicateGrants(grants)
	if len(result) != 1 {
		t.Fatalf("expected 1 grant, got %d", len(result))
	}
	if result[0].Role != "owner" {
		t.Errorf("expected role owner, got %s", result[0].Role)
	}
	if result[0].Nbf == nil || *result[0].Nbf != 1000 {
		t.Errorf("expected Nbf 1000, got %v", result[0].Nbf)
	}
	if result[0].Exp == nil || *result[0].Exp != 2000 {
		t.Errorf("expected Exp 2000, got %v", result[0].Exp)
	}
}

func TestDeduplicateGrants_NoDuplicates(t *testing.T) {
	grants := []AnnotationGrant{
		{Principal: "alice@example.com", Role: "editor"},
		{Principal: "bob@example.com", Role: "viewer"},
	}
	result := DeduplicateGrants(grants)
	if len(result) != 2 {
		t.Fatalf("expected 2 grants, got %d", len(result))
	}
	if result[0].Principal != "alice@example.com" || result[0].Role != "editor" {
		t.Errorf("expected alice/editor, got %s/%s", result[0].Principal, result[0].Role)
	}
	if result[1].Principal != "bob@example.com" || result[1].Role != "viewer" {
		t.Errorf("expected bob/viewer, got %s/%s", result[1].Principal, result[1].Role)
	}
}

func TestDeduplicateGrants_EmptyPrincipal(t *testing.T) {
	grants := []AnnotationGrant{
		{Principal: "", Role: "editor"},
		{Principal: "alice@example.com", Role: "viewer"},
		{Principal: "", Role: "owner"},
	}
	result := DeduplicateGrants(grants)
	if len(result) != 1 {
		t.Fatalf("expected 1 grant, got %d", len(result))
	}
	if result[0].Principal != "alice@example.com" {
		t.Errorf("expected principal alice@example.com, got %s", result[0].Principal)
	}
}

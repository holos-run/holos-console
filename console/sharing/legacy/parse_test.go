package legacy

import (
	"strings"
	"testing"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
)

func TestParseGrants_NilAnnotations(t *testing.T) {
	got, err := ParseGrants(nil, v1alpha2.AnnotationShareUsers)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got != nil {
		t.Errorf("expected nil grants, got %#v", got)
	}
}

func TestParseGrants_KeyAbsent(t *testing.T) {
	got, err := ParseGrants(map[string]string{"unrelated": "x"}, v1alpha2.AnnotationShareUsers)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got != nil {
		t.Errorf("expected nil grants, got %#v", got)
	}
}

func TestParseGrants_ValidJSON(t *testing.T) {
	annotations := map[string]string{
		v1alpha2.AnnotationShareUsers: `[{"principal":"alice@example.com","role":"viewer"},{"principal":"bob@example.com","role":"editor"}]`,
	}
	got, err := ParseGrants(annotations, v1alpha2.AnnotationShareUsers)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 grants, got %d", len(got))
	}
	if got[0].Principal != "alice@example.com" || got[0].Role != "viewer" {
		t.Errorf("unexpected first grant: %#v", got[0])
	}
	if got[1].Principal != "bob@example.com" || got[1].Role != "editor" {
		t.Errorf("unexpected second grant: %#v", got[1])
	}
}

func TestParseGrants_TimeBoundedFields(t *testing.T) {
	annotations := map[string]string{
		v1alpha2.AnnotationShareUsers: `[{"principal":"x","role":"viewer","nbf":100,"exp":200}]`,
	}
	got, err := ParseGrants(annotations, v1alpha2.AnnotationShareUsers)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 grant, got %d", len(got))
	}
	if got[0].Nbf == nil || *got[0].Nbf != 100 {
		t.Errorf("expected nbf=100, got %#v", got[0].Nbf)
	}
	if got[0].Exp == nil || *got[0].Exp != 200 {
		t.Errorf("expected exp=200, got %#v", got[0].Exp)
	}
}

func TestParseGrants_MalformedJSON_ReturnsErrorWithKey(t *testing.T) {
	annotations := map[string]string{
		v1alpha2.AnnotationShareUsers: "not json",
	}
	_, err := ParseGrants(annotations, v1alpha2.AnnotationShareUsers)
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
	if !strings.Contains(err.Error(), v1alpha2.AnnotationShareUsers) {
		t.Errorf("expected error to include annotation key %q, got %q", v1alpha2.AnnotationShareUsers, err.Error())
	}
}

func TestShareUsers_Helper(t *testing.T) {
	annotations := map[string]string{
		v1alpha2.AnnotationShareUsers: `[{"principal":"a","role":"viewer"}]`,
	}
	got, err := ShareUsers(annotations)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Principal != "a" {
		t.Errorf("unexpected grants: %#v", got)
	}
}

func TestShareRoles_Helper(t *testing.T) {
	annotations := map[string]string{
		v1alpha2.AnnotationShareRoles: `[{"principal":"team-a","role":"editor"}]`,
	}
	got, err := ShareRoles(annotations)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Role != "editor" {
		t.Errorf("unexpected grants: %#v", got)
	}
}

func TestDefaultShareUsers_Helper(t *testing.T) {
	annotations := map[string]string{
		v1alpha2.AnnotationDefaultShareUsers: `[{"principal":"a","role":"viewer"}]`,
	}
	got, err := DefaultShareUsers(annotations)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 grant, got %d", len(got))
	}
}

func TestDefaultShareRoles_Helper(t *testing.T) {
	annotations := map[string]string{
		v1alpha2.AnnotationDefaultShareRoles: `[{"principal":"team-a","role":"editor"}]`,
	}
	got, err := DefaultShareRoles(annotations)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 grant, got %d", len(got))
	}
}

func TestAnnotationKeys_StableOrder(t *testing.T) {
	keys := AnnotationKeys()
	want := []string{
		v1alpha2.AnnotationShareUsers,
		v1alpha2.AnnotationShareRoles,
		v1alpha2.AnnotationDefaultShareUsers,
		v1alpha2.AnnotationDefaultShareRoles,
	}
	if len(keys) != len(want) {
		t.Fatalf("expected %d keys, got %d", len(want), len(keys))
	}
	for i := range keys {
		if keys[i] != want[i] {
			t.Errorf("AnnotationKeys()[%d]=%q, want %q", i, keys[i], want[i])
		}
	}
}

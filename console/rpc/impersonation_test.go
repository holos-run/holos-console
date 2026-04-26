package rpc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
)

func TestNewImpersonatedClientsAddsOIDCImpersonationHeaders(t *testing.T) {
	headers := make(chan http.Header, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers <- r.Header.Clone()
		writeNamespaceList(t, w)
	}))
	defer server.Close()

	clients, err := NewImpersonatedClients(&Claims{
		Sub:   "user-123",
		Email: "user@example.com",
		Roles: []string{"platform-admins", "project-editors", "system:authenticated"},
	}, testRESTConfig(server.URL), nil)
	if err != nil {
		t.Fatalf("NewImpersonatedClients: %v", err)
	}

	if _, err := clients.Clientset.CoreV1().Namespaces().List(context.Background(), metav1.ListOptions{}); err != nil {
		t.Fatalf("list namespaces: %v", err)
	}

	got := <-headers
	if got.Get("Impersonate-User") != "oidc:user-123" {
		t.Fatalf("Impersonate-User = %q, want oidc:user-123", got.Get("Impersonate-User"))
	}
	wantGroups := []string{"oidc:platform-admins", "oidc:project-editors"}
	if !reflect.DeepEqual(got.Values("Impersonate-Group"), wantGroups) {
		t.Fatalf("Impersonate-Group = %#v, want %#v", got.Values("Impersonate-Group"), wantGroups)
	}
	if got.Get("Impersonate-Extra-Email") != "" {
		t.Fatalf("Impersonate-Extra-Email = %q, want empty", got.Get("Impersonate-Extra-Email"))
	}
}

func TestImpersonatedClientsFromContextCachesWithinRequestOnly(t *testing.T) {
	users := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		users <- r.Header.Get("Impersonate-User")
		writeNamespaceList(t, w)
	}))
	defer server.Close()

	ctxA := contextWithTestImpersonatedClients(t, "alice", server.URL)
	ctxB := contextWithTestImpersonatedClients(t, "bob", server.URL)

	clientA := ImpersonatedClientsetFromContext(ctxA)
	if got := ImpersonatedClientsetFromContext(ctxA); got != clientA {
		t.Fatal("same request context returned a different impersonated clientset")
	}
	clientB := ImpersonatedClientsetFromContext(ctxB)
	if clientB == clientA {
		t.Fatal("different request contexts shared the same impersonated clientset")
	}

	if _, err := clientA.CoreV1().Namespaces().List(context.Background(), metav1.ListOptions{}); err != nil {
		t.Fatalf("list namespaces as alice: %v", err)
	}
	if _, err := clientB.CoreV1().Namespaces().List(context.Background(), metav1.ListOptions{}); err != nil {
		t.Fatalf("list namespaces as bob: %v", err)
	}

	if got := <-users; got != "oidc:alice" {
		t.Fatalf("first request impersonated %q, want oidc:alice", got)
	}
	if got := <-users; got != "oidc:bob" {
		t.Fatalf("second request impersonated %q, want oidc:bob", got)
	}
}

func TestImpersonatedClientsFromContextWithoutAuthReturnsUnauthorizedClient(t *testing.T) {
	_, err := ImpersonatedClientsetFromContext(context.Background()).
		CoreV1().
		Namespaces().
		List(context.Background(), metav1.ListOptions{})
	if !apierrors.IsUnauthorized(err) {
		t.Fatalf("error = %v, want kubernetes unauthorized error", err)
	}
}

func TestNewImpersonatedClientsRejectsEmptySubject(t *testing.T) {
	_, err := NewImpersonatedClients(&Claims{Roles: []string{"platform-admins"}}, testRESTConfig("https://example.test"), nil)
	if err == nil {
		t.Fatal("expected error for empty subject, got nil")
	}
	if err != ErrUnauthenticatedImpersonation {
		t.Fatalf("error = %v, want ErrUnauthenticatedImpersonation", err)
	}
}

func contextWithTestImpersonatedClients(t *testing.T, sub, host string) context.Context {
	t.Helper()
	clients, err := NewImpersonatedClients(&Claims{Sub: sub}, testRESTConfig(host), nil)
	if err != nil {
		t.Fatalf("NewImpersonatedClients(%q): %v", sub, err)
	}
	return ContextWithImpersonatedClients(context.Background(), clients)
}

func testRESTConfig(host string) *rest.Config {
	return &rest.Config{
		Host:    host,
		APIPath: "/api",
		ContentConfig: rest.ContentConfig{
			GroupVersion:         &schema.GroupVersion{Version: "v1"},
			NegotiatedSerializer: clientgoscheme.Codecs.WithoutConversion(),
		},
	}
}

func writeNamespaceList(t *testing.T, w http.ResponseWriter) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"kind":       "NamespaceList",
		"apiVersion": "v1",
		"metadata":   map[string]interface{}{},
		"items":      []interface{}{},
	}); err != nil {
		t.Fatalf("write response: %v", err)
	}
}

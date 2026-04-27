package rpc

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"

	"connectrpc.com/connect"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const oidcImpersonationPrefix = "oidc:"

// ErrUnauthenticatedImpersonation is returned when an impersonating client
// cannot be built because the request has no authenticated OIDC principal.
var ErrUnauthenticatedImpersonation = errors.New("authenticated OIDC subject is required for kubernetes impersonation")

// ImpersonatedClients is the per-request Kubernetes client bundle used by RPC
// handlers as they migrate from the startup-scoped service-account clients.
type ImpersonatedClients struct {
	Clientset kubernetes.Interface
	Dynamic   dynamic.Interface
	Client    ctrlclient.Client
}

// NewImpersonatedClients creates Kubernetes clients that impersonate the OIDC
// subject and groups from claims. The returned clients must be scoped to the
// current request context; callers must not store them globally.
func NewImpersonatedClients(claims *Claims, base *rest.Config, scheme *runtime.Scheme) (*ImpersonatedClients, error) {
	if claims == nil || strings.TrimSpace(claims.Sub) == "" {
		return nil, ErrUnauthenticatedImpersonation
	}
	if base == nil {
		return newUnauthenticatedClients()
	}

	config := rest.CopyConfig(base)
	config.Impersonate = rest.ImpersonationConfig{
		UserName: oidcImpersonationPrefix + claims.Sub,
		Groups:   PrefixedOIDCGroups(claims.Roles),
	}
	return newClientsForConfig(config, scheme)
}

// ImpersonationInterceptor builds per-request Kubernetes clients from the
// authenticated OIDC claims already stored on the request context. It is kept
// separate from auth so handlers can migrate to Impersonated*FromContext
// without depending on a particular token verifier implementation.
func ImpersonationInterceptor(base *rest.Config, scheme *runtime.Scheme) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			clients, err := NewImpersonatedClients(ClaimsFromContext(ctx), base, scheme)
			if err != nil {
				if errors.Is(err, ErrUnauthenticatedImpersonation) {
					return nil, connect.NewError(connect.CodeUnauthenticated, err)
				}
				return nil, connect.NewError(connect.CodeInternal, err)
			}
			ctx = ContextWithImpersonatedClients(ctx, clients)
			return next(ctx, req)
		}
	}
}

// PrefixedOIDCGroups maps OIDC group claims into the Kubernetes principal
// namespace defined by ADR 036.
func PrefixedOIDCGroups(groups []string) []string {
	if len(groups) == 0 {
		return nil
	}
	prefixed := make([]string, 0, len(groups))
	for _, group := range groups {
		group = strings.TrimSpace(group)
		if group == "" || strings.HasPrefix(group, "system:") {
			continue
		}
		prefixed = append(prefixed, oidcImpersonationPrefix+group)
	}
	return prefixed
}

type impersonatedClientsKey struct{}

// ContextWithImpersonatedClients stores the per-request impersonating clients.
func ContextWithImpersonatedClients(ctx context.Context, clients *ImpersonatedClients) context.Context {
	return context.WithValue(ctx, impersonatedClientsKey{}, clients)
}

// ImpersonatedClientsFromContext returns the per-request impersonating clients,
// or a no-op unauthenticated bundle when the context has no authenticated user.
func ImpersonatedClientsFromContext(ctx context.Context) *ImpersonatedClients {
	if clients, _ := ctx.Value(impersonatedClientsKey{}).(*ImpersonatedClients); clients != nil {
		return clients
	}
	return unauthenticatedClients()
}

// HasImpersonatedClients reports whether the auth interceptor stored a
// per-request impersonating client bundle on this context.
func HasImpersonatedClients(ctx context.Context) bool {
	clients, _ := ctx.Value(impersonatedClientsKey{}).(*ImpersonatedClients)
	return clients != nil
}

// ImpersonatedClientsetFromContext returns the per-request client-go clientset.
func ImpersonatedClientsetFromContext(ctx context.Context) kubernetes.Interface {
	return ImpersonatedClientsFromContext(ctx).Clientset
}

// ImpersonatedDynamicClientFromContext returns the per-request dynamic client.
func ImpersonatedDynamicClientFromContext(ctx context.Context) dynamic.Interface {
	return ImpersonatedClientsFromContext(ctx).Dynamic
}

// ImpersonatedCtrlClientFromContext returns the per-request controller-runtime client.
func ImpersonatedCtrlClientFromContext(ctx context.Context) ctrlclient.Client {
	return ImpersonatedClientsFromContext(ctx).Client
}

func newClientsForConfig(config *rest.Config, scheme *runtime.Scheme) (*ImpersonatedClients, error) {
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	if scheme == nil {
		scheme = clientgoscheme.Scheme
	}
	ctrlClient, err := ctrlclient.New(config, ctrlclient.Options{Scheme: scheme})
	if err != nil {
		return nil, err
	}
	return &ImpersonatedClients{
		Clientset: clientset,
		Dynamic:   dynamicClient,
		Client:    ctrlClient,
	}, nil
}

var (
	unauthenticatedOnce sync.Once
	unauthenticated     *ImpersonatedClients
)

func unauthenticatedClients() *ImpersonatedClients {
	unauthenticatedOnce.Do(func() {
		var err error
		unauthenticated, err = newUnauthenticatedClients()
		if err != nil {
			panic(err)
		}
	})
	return unauthenticated
}

func newUnauthenticatedClients() (*ImpersonatedClients, error) {
	return newClientsForConfig(&rest.Config{
		Host: "https://unauthenticated.invalid",
		Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			status := apierrors.NewUnauthorized("authentication required").Status()
			body, err := runtime.Encode(clientgoscheme.Codecs.LegacyCodec(metav1.SchemeGroupVersion), &status)
			if err != nil {
				return nil, err
			}
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Status:     http.StatusText(http.StatusUnauthorized),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(bytes.NewReader(body)),
			}, nil
		}),
	}, clientgoscheme.Scheme)
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

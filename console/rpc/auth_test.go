package rpc

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// fakeOIDCServer creates an httptest server that serves OIDC discovery and JWKS
// endpoints. The shouldFail atomic controls whether discovery returns errors.
type fakeOIDCServer struct {
	Server     *httptest.Server
	ShouldFail *atomic.Bool
	PrivateKey *rsa.PrivateKey
	KeyID      string
}

func newFakeOIDCServer(t *testing.T) *fakeOIDCServer {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating RSA key: %v", err)
	}

	shouldFail := &atomic.Bool{}
	keyID := "test-key-1"

	mux := http.NewServeMux()
	var serverURL string

	f := &fakeOIDCServer{
		ShouldFail: shouldFail,
		PrivateKey: privateKey,
		KeyID:      keyID,
	}

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		if shouldFail.Load() {
			http.Error(w, "service unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"issuer":                 serverURL,
			"jwks_uri":              serverURL + "/keys",
			"authorization_endpoint": serverURL + "/auth",
			"token_endpoint":         serverURL + "/token",
		})
	})

	mux.HandleFunc("/keys", func(w http.ResponseWriter, r *http.Request) {
		jwk := jose.JSONWebKey{
			Key:       &privateKey.PublicKey,
			KeyID:     keyID,
			Algorithm: string(jose.RS256),
			Use:       "sig",
		}
		jwks := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{jwk}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jwks)
	})

	srv := httptest.NewServer(mux)
	serverURL = srv.URL
	f.Server = srv
	return f
}

// signToken creates a signed JWT with the given subject and audience.
func (f *fakeOIDCServer) signToken(t *testing.T, subject, audience string) string {
	t.Helper()

	signerOpts := jose.SignerOptions{}
	signerOpts.WithHeader(jose.HeaderKey("kid"), f.KeyID)

	signer, err := jose.NewSigner(jose.SigningKey{
		Algorithm: jose.RS256,
		Key:       f.PrivateKey,
	}, &signerOpts)
	if err != nil {
		t.Fatalf("creating signer: %v", err)
	}

	now := time.Now()
	claims := jwt.Claims{
		Issuer:    f.Server.URL,
		Subject:   subject,
		Audience:  jwt.Audience{audience},
		IssuedAt:  jwt.NewNumericDate(now),
		Expiry:    jwt.NewNumericDate(now.Add(time.Hour)),
		NotBefore: jwt.NewNumericDate(now),
	}

	token, err := jwt.Signed(signer).Claims(claims).Serialize()
	if err != nil {
		t.Fatalf("signing token: %v", err)
	}
	return token
}

// noopHandler is a ConnectRPC handler that returns a nil response.
func noopHandler(_ context.Context, _ connect.AnyRequest) (connect.AnyResponse, error) {
	return nil, nil
}

// newTestRequest creates a minimal ConnectRPC request with an Authorization header.
func newTestRequest(token string) *connect.Request[any] {
	req := connect.NewRequest[any](nil)
	if token != "" {
		req.Header().Set("Authorization", fmt.Sprintf("Bearer %s", token))
	}
	return req
}

func TestLazyAuthInterceptor_RetryAfterInitFailure(t *testing.T) {
	fake := newFakeOIDCServer(t)
	defer fake.Server.Close()

	clientID := "test-client"
	interceptor := LazyAuthInterceptor(fake.Server.URL, clientID, "groups", fake.Server.Client())

	handler := interceptor(noopHandler)

	// First request: OIDC discovery should fail
	fake.ShouldFail.Store(true)
	token := fake.signToken(t, "user-1", clientID)
	_, err := handler(context.Background(), newTestRequest(token))
	if err == nil {
		t.Fatal("expected error when OIDC discovery fails, got nil")
	}
	if connect.CodeOf(err) != connect.CodeUnavailable {
		t.Fatalf("expected CodeUnavailable, got %v", connect.CodeOf(err))
	}

	// Second request: OIDC discovery should succeed (retry must work)
	fake.ShouldFail.Store(false)
	_, err = handler(context.Background(), newTestRequest(token))
	if err != nil {
		t.Fatalf("expected success after OIDC recovery, got error: %v", err)
	}
}

func TestLazyAuthInterceptor_CachesAfterSuccess(t *testing.T) {
	fake := newFakeOIDCServer(t)
	defer fake.Server.Close()

	clientID := "test-client"
	discoveryCount := &atomic.Int32{}

	// Wrap the server to count discovery requests
	countingMux := http.NewServeMux()
	countingMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/openid-configuration" {
			discoveryCount.Add(1)
		}
		fake.Server.Config.Handler.ServeHTTP(w, r)
	})
	countingSrv := httptest.NewServer(countingMux)
	defer countingSrv.Close()

	// Create a new fake that points to the counting server for discovery
	// but uses the same keys
	countingFake := newFakeOIDCServer(t)
	defer countingFake.Server.Close()

	interceptor := LazyAuthInterceptor(countingFake.Server.URL, clientID, "groups", countingFake.Server.Client())
	handler := interceptor(noopHandler)

	token := countingFake.signToken(t, "user-1", clientID)

	// First request initializes the verifier
	_, err := handler(context.Background(), newTestRequest(token))
	if err != nil {
		t.Fatalf("first request failed: %v", err)
	}

	// Second request should reuse cached verifier
	_, err = handler(context.Background(), newTestRequest(token))
	if err != nil {
		t.Fatalf("second request failed: %v", err)
	}

	// Third request should also reuse cached verifier
	_, err = handler(context.Background(), newTestRequest(token))
	if err != nil {
		t.Fatalf("third request failed: %v", err)
	}
}

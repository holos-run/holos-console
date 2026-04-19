package oidc_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/holos-run/holos-console/console/oidc"
)

// newDexState creates a real Dex instance and returns its DexState for testing.
func newDexState(t *testing.T) *oidc.DexState {
	t.Helper()
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	_, state, err := oidc.NewHandler(ctx, oidc.Config{
		Issuer:       "https://test.example.com/dex",
		ClientID:     "test-client",
		RedirectURIs: []string{"https://test.example.com/callback"},
		Logger:       logger,
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	return state
}

// postTokenExchange invokes the handler with a JSON-encoded body and returns
// the recorder so the test can assert on status, headers, and body.
func postTokenExchange(t *testing.T, state *oidc.DexState, body string) *httptest.ResponseRecorder {
	t.Helper()
	handler := oidc.HandleTokenExchange(state)
	req := httptest.NewRequest(http.MethodPost, "/api/dev/token", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler(rr, req)
	return rr
}

// decodeTokenResponse decodes a successful token-exchange response body.
func decodeTokenResponse(t *testing.T, rr *httptest.ResponseRecorder) oidc.TokenExchangeResponse {
	t.Helper()
	var resp oidc.TokenExchangeResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	return resp
}

// verifiedClaims parses an id_token JWS against the Dex signing public key
// and returns the decoded claims. Fails the test if the signature does not
// verify.
func verifiedClaims(t *testing.T, state *oidc.DexState, idToken string) map[string]any {
	t.Helper()

	keys, err := state.Storage.GetKeys(context.Background())
	if err != nil {
		t.Fatalf("state.Storage.GetKeys: %v", err)
	}
	if keys.SigningKeyPub == nil {
		t.Fatal("no signing public key available in Dex storage")
	}

	// Accept whatever algorithm Dex is configured with; in practice the
	// embedded Dex uses RS256 but we don't want to hard-code it here.
	alg := jose.SignatureAlgorithm(keys.SigningKeyPub.Algorithm)
	if alg == "" {
		alg = jose.RS256
	}
	parsed, err := jose.ParseSigned(idToken, []jose.SignatureAlgorithm{alg})
	if err != nil {
		t.Fatalf("jose.ParseSigned: %v", err)
	}

	payload, err := parsed.Verify(keys.SigningKeyPub)
	if err != nil {
		t.Fatalf("signature verification failed: %v", err)
	}

	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatalf("unmarshal claims: %v", err)
	}
	return claims
}

func TestHandleTokenExchange_Success(t *testing.T) {
	state := newDexState(t)

	rr := postTokenExchange(t, state, `{"email":"platform@localhost"}`)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	resp := decodeTokenResponse(t, rr)

	if resp.IDToken == "" {
		t.Error("id_token is empty")
	}
	if resp.Email != "platform@localhost" {
		t.Errorf("email = %q, want %q", resp.Email, "platform@localhost")
	}
	if len(resp.Groups) != 1 || resp.Groups[0] != "owner" {
		t.Errorf("groups = %v, want [owner]", resp.Groups)
	}
	if resp.ExpiresIn <= 0 {
		t.Errorf("expires_in = %d, want > 0", resp.ExpiresIn)
	}
}

// TestHandleTokenExchange_Personas is the table-driven Go-test replacement
// for the "Dev Token Endpoint > should return a valid token for the
// <persona>" Playwright cases in frontend/e2e/multi-persona.spec.ts. Each
// row asserts the same response-shape invariants the browser tests asserted
// (id_token present, email echoed back, expected group membership, non-zero
// expiry) without paying the browser-start cost.
func TestHandleTokenExchange_Personas(t *testing.T) {
	state := newDexState(t)

	cases := []struct {
		name       string
		email      string
		wantGroups []string
	}{
		{
			name:       "platform engineer persona returns owner group",
			email:      oidc.EmailPlatform,
			wantGroups: []string{"owner"},
		},
		{
			name:       "product engineer persona returns editor group",
			email:      oidc.EmailProduct,
			wantGroups: []string{"editor"},
		},
		{
			name:       "SRE persona returns viewer group",
			email:      oidc.EmailSRE,
			wantGroups: []string{"viewer"},
		},
		{
			name:       "admin persona returns owner group",
			email:      oidc.EmailAdmin,
			wantGroups: []string{"owner"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, err := json.Marshal(oidc.TokenExchangeRequest{Email: tc.email})
			if err != nil {
				t.Fatalf("marshal request: %v", err)
			}

			rr := postTokenExchange(t, state, string(body))

			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body = %s", rr.Code, http.StatusOK, rr.Body.String())
			}

			resp := decodeTokenResponse(t, rr)

			if resp.IDToken == "" {
				t.Error("id_token is empty")
			}
			if resp.Email != tc.email {
				t.Errorf("email = %q, want %q", resp.Email, tc.email)
			}
			if resp.ExpiresIn <= 0 {
				t.Errorf("expires_in = %d, want > 0", resp.ExpiresIn)
			}
			if len(resp.Groups) != len(tc.wantGroups) {
				t.Fatalf("groups = %v, want %v", resp.Groups, tc.wantGroups)
			}
			for i, g := range tc.wantGroups {
				if resp.Groups[i] != g {
					t.Errorf("groups[%d] = %q, want %q", i, resp.Groups[i], g)
				}
			}
		})
	}
}

// TestHandleTokenExchange_SignatureVerification exercises the signature-
// verification invariant that the browser tests implicitly relied on when
// they injected the returned id_token into sessionStorage and expected
// subsequent RPCs to authenticate. It parses the JWS with the algorithm
// declared by Dex's signing key and verifies it against the public key
// exposed via state.Storage.GetKeys.
func TestHandleTokenExchange_SignatureVerification(t *testing.T) {
	state := newDexState(t)

	for _, email := range []string{
		oidc.EmailAdmin,
		oidc.EmailPlatform,
		oidc.EmailProduct,
		oidc.EmailSRE,
	} {
		t.Run(email, func(t *testing.T) {
			body, err := json.Marshal(oidc.TokenExchangeRequest{Email: email})
			if err != nil {
				t.Fatalf("marshal request: %v", err)
			}

			rr := postTokenExchange(t, state, string(body))
			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body = %s", rr.Code, http.StatusOK, rr.Body.String())
			}
			resp := decodeTokenResponse(t, rr)

			// Verify the JWS signature using the Dex signing public key.
			claims := verifiedClaims(t, state, resp.IDToken)

			// Spot-check that the verified claims carry the requester's email.
			gotEmail, _ := claims["email"].(string)
			if gotEmail != email {
				t.Errorf("verified claims email = %q, want %q", gotEmail, email)
			}
		})
	}
}

// TestHandleTokenExchange_ClaimContents asserts the OIDC claim contents each
// persona's id_token carries. This is the signed-token analogue of the
// persona-email assertions the browser tests performed after persona
// switch (the profile page's rendered email came from the email claim),
// plus the issuer/audience/subject/email_verified claims that any
// downstream JWT verifier cares about.
func TestHandleTokenExchange_ClaimContents(t *testing.T) {
	state := newDexState(t)

	cases := []struct {
		name         string
		email        string
		wantGroup    string
		wantAudience string
		wantIssuer   string
	}{
		{
			name:         "platform engineer claims",
			email:        oidc.EmailPlatform,
			wantGroup:    "owner",
			wantAudience: "test-client",
			wantIssuer:   "https://test.example.com/dex",
		},
		{
			name:         "product engineer claims",
			email:        oidc.EmailProduct,
			wantGroup:    "editor",
			wantAudience: "test-client",
			wantIssuer:   "https://test.example.com/dex",
		},
		{
			name:         "SRE claims",
			email:        oidc.EmailSRE,
			wantGroup:    "viewer",
			wantAudience: "test-client",
			wantIssuer:   "https://test.example.com/dex",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, err := json.Marshal(oidc.TokenExchangeRequest{Email: tc.email})
			if err != nil {
				t.Fatalf("marshal request: %v", err)
			}
			rr := postTokenExchange(t, state, string(body))
			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body = %s", rr.Code, http.StatusOK, rr.Body.String())
			}
			resp := decodeTokenResponse(t, rr)

			claims := verifiedClaims(t, state, resp.IDToken)

			if got, _ := claims["iss"].(string); got != tc.wantIssuer {
				t.Errorf("iss = %q, want %q", got, tc.wantIssuer)
			}
			// Dex encodes a single-element audience as a string, not an
			// array (see audience.MarshalJSON in token_exchange.go).
			if got, _ := claims["aud"].(string); got != tc.wantAudience {
				t.Errorf("aud = %q, want %q", got, tc.wantAudience)
			}
			if got, _ := claims["email"].(string); got != tc.email {
				t.Errorf("email = %q, want %q", got, tc.email)
			}
			if got, _ := claims["email_verified"].(bool); !got {
				t.Errorf("email_verified = %v, want true", claims["email_verified"])
			}

			// groups is a JSON array; the JWT-decoded value is []any.
			groupsAny, ok := claims["groups"].([]any)
			if !ok {
				t.Fatalf("groups claim missing or wrong type: %T", claims["groups"])
			}
			if len(groupsAny) != 1 {
				t.Fatalf("groups = %v, want single-element [%q]", groupsAny, tc.wantGroup)
			}
			if got, _ := groupsAny[0].(string); got != tc.wantGroup {
				t.Errorf("groups[0] = %q, want %q", got, tc.wantGroup)
			}

			// sub must be a non-empty base64-url-encoded protobuf subject.
			if got, _ := claims["sub"].(string); got == "" {
				t.Error("sub claim is empty")
			}

			// iat and exp are floats in generic JSON decoding.
			iat, _ := claims["iat"].(float64)
			exp, _ := claims["exp"].(float64)
			if iat == 0 {
				t.Error("iat claim missing or zero")
			}
			if exp == 0 {
				t.Error("exp claim missing or zero")
			}
			if exp <= iat {
				t.Errorf("exp (%v) must be after iat (%v)", exp, iat)
			}

			// The claim window must match the handler's declared
			// expires_in of 1 hour (see mintIDToken in
			// token_exchange.go). Allow a small clock skew.
			window := time.Duration(exp-iat) * time.Second
			wantWindow := time.Duration(resp.ExpiresIn) * time.Second
			if delta := window - wantWindow; delta < -5*time.Second || delta > 5*time.Second {
				t.Errorf("claim window = %v, want ~%v", window, wantWindow)
			}
		})
	}
}

func TestHandleTokenExchange_AllUsers(t *testing.T) {
	state := newDexState(t)
	handler := oidc.HandleTokenExchange(state)

	for _, user := range oidc.TestUsers {
		t.Run(user.ID, func(t *testing.T) {
			body, _ := json.Marshal(map[string]string{"email": user.Email})
			req := httptest.NewRequest(http.MethodPost, "/api/dev/token", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()

			handler(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body = %s", rr.Code, http.StatusOK, rr.Body.String())
			}

			var resp oidc.TokenExchangeResponse
			if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			if resp.Email != user.Email {
				t.Errorf("email = %q, want %q", resp.Email, user.Email)
			}
			if len(resp.Groups) != len(user.Groups) {
				t.Fatalf("groups length = %d, want %d", len(resp.Groups), len(user.Groups))
			}
			for i, g := range resp.Groups {
				if g != user.Groups[i] {
					t.Errorf("groups[%d] = %q, want %q", i, g, user.Groups[i])
				}
			}
		})
	}
}

// TestHandleTokenExchange_UnknownEmail is the Go-test replacement for the
// "Dev Token Endpoint > should reject unknown email addresses" Playwright
// case. It asserts the 400 status and the "unknown test user email" body
// fragment the E2E test asserted on.
func TestHandleTokenExchange_UnknownEmail(t *testing.T) {
	state := newDexState(t)

	cases := []struct {
		name  string
		email string
	}{
		{name: "simple unknown", email: "nobody@localhost"},
		{name: "multi-persona spec literal", email: "unknown@example.com"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, err := json.Marshal(oidc.TokenExchangeRequest{Email: tc.email})
			if err != nil {
				t.Fatalf("marshal request: %v", err)
			}

			rr := postTokenExchange(t, state, string(body))

			if rr.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body = %s", rr.Code, http.StatusBadRequest, rr.Body.String())
			}
			if !strings.Contains(rr.Body.String(), "unknown test user email") {
				t.Errorf("body = %q, want to contain %q", rr.Body.String(), "unknown test user email")
			}
		})
	}
}

func TestHandleTokenExchange_EmptyEmail(t *testing.T) {
	state := newDexState(t)
	handler := oidc.HandleTokenExchange(state)

	body := `{"email":""}`
	req := httptest.NewRequest(http.MethodPost, "/api/dev/token", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", rr.Code, http.StatusBadRequest, rr.Body.String())
	}
}

func TestHandleTokenExchange_InvalidJSON(t *testing.T) {
	state := newDexState(t)
	handler := oidc.HandleTokenExchange(state)

	body := `not json`
	req := httptest.NewRequest(http.MethodPost, "/api/dev/token", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", rr.Code, http.StatusBadRequest, rr.Body.String())
	}
}

func TestHandleTokenExchange_WrongMethod(t *testing.T) {
	state := newDexState(t)
	handler := oidc.HandleTokenExchange(state)

	req := httptest.NewRequest(http.MethodGet, "/api/dev/token", nil)
	rr := httptest.NewRecorder()

	handler(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleTokenExchange_NilState(t *testing.T) {
	// When Dex is disabled, the handler receives nil state.
	handler := oidc.HandleTokenExchange(nil)

	body := `{"email":"admin@localhost"}`
	req := httptest.NewRequest(http.MethodPost, "/api/dev/token", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusNotFound)
	}
}

func TestHandleTokenExchange_ResponseContentType(t *testing.T) {
	state := newDexState(t)
	handler := oidc.HandleTokenExchange(state)

	body := `{"email":"admin@localhost"}`
	req := httptest.NewRequest(http.MethodPost, "/api/dev/token", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler(rr, req)

	ct := rr.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
}

func TestHandleTokenExchange_BodyTooLarge(t *testing.T) {
	state := newDexState(t)
	handler := oidc.HandleTokenExchange(state)

	// Send a body larger than the limit
	largeBody := make([]byte, 2*1024*1024) // 2MB
	req := httptest.NewRequest(http.MethodPost, "/api/dev/token", bytes.NewBuffer(largeBody))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler(rr, req)

	// Should fail with bad request (invalid JSON after truncation)
	if rr.Code == http.StatusOK {
		t.Fatal("expected non-200 status for oversized body")
	}
}

func TestHandleTokenExchange_TokenFormat(t *testing.T) {
	state := newDexState(t)
	handler := oidc.HandleTokenExchange(state)

	body := `{"email":"admin@localhost"}`
	req := httptest.NewRequest(http.MethodPost, "/api/dev/token", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var resp oidc.TokenExchangeResponse
	data, _ := io.ReadAll(rr.Body)
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// ID token should be a JWT (3 dot-separated parts)
	parts := bytes.Split([]byte(resp.IDToken), []byte("."))
	if len(parts) != 3 {
		t.Errorf("id_token has %d parts, want 3 (JWT format)", len(parts))
	}
}

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
	"testing"

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

func TestHandleTokenExchange_Success(t *testing.T) {
	state := newDexState(t)
	handler := oidc.HandleTokenExchange(state)

	body := `{"email":"platform@localhost"}`
	req := httptest.NewRequest(http.MethodPost, "/api/dev/token", bytes.NewBufferString(body))
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

func TestHandleTokenExchange_UnknownEmail(t *testing.T) {
	state := newDexState(t)
	handler := oidc.HandleTokenExchange(state)

	body := `{"email":"nobody@localhost"}`
	req := httptest.NewRequest(http.MethodPost, "/api/dev/token", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", rr.Code, http.StatusBadRequest, rr.Body.String())
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

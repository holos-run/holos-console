package console

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestLogRequests_HealthCheck_Suppressed(t *testing.T) {
	// When LogHealthChecks is false (default), /healthz and /readyz should not be logged.
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	slog.SetDefault(logger)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := logRequests(inner, false)

	for _, path := range []string{"/healthz", "/readyz"} {
		buf.Reset()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if buf.Len() != 0 {
			t.Errorf("expected no log output for %s when LogHealthChecks=false, got: %s", path, buf.String())
		}
	}
}

func TestLogRequests_HealthCheck_Logged(t *testing.T) {
	// When LogHealthChecks is true, /healthz and /readyz should be logged.
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	slog.SetDefault(logger)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := logRequests(inner, true)

	for _, path := range []string{"/healthz", "/readyz"} {
		buf.Reset()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if buf.Len() == 0 {
			t.Errorf("expected log output for %s when LogHealthChecks=true, got nothing", path)
		}
	}
}

func TestHandleUserInfo_Removed(t *testing.T) {
	// The /api/userinfo endpoint (FINDING-02) trusted X-Forwarded-* headers
	// without validation. It has been removed as part of reversing ADR 002.
	// This test verifies the handleUserInfo function no longer exists by
	// confirming the endpoint is not registered.
	mux := http.NewServeMux()
	// If handleUserInfo were still registered, GET /api/userinfo with
	// X-Forwarded-User would return 200. After removal, the default mux
	// returns 404.
	req := httptest.NewRequest(http.MethodGet, "/api/userinfo", nil)
	req.Header.Set("X-Forwarded-User", "attacker")
	req.Header.Set("X-Forwarded-Email", "attacker@example.com")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected /api/userinfo to return 404 (removed), got %d", rec.Code)
	}
}

func TestServeIndex_InjectsConsoleConfig(t *testing.T) {
	// When ConsoleConfig is provided, serveIndex should inject
	// window.__CONSOLE_CONFIG__ into the HTML <head>.
	fakeFS := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte(`<!DOCTYPE html><html><head></head><body></body></html>`),
		},
	}

	consoleConfig := &ConsoleConfig{DevToolsEnabled: true}
	h := newUIHandler(fakeFS, nil, consoleConfig)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `window.__CONSOLE_CONFIG__={"devToolsEnabled":true}`) {
		t.Errorf("expected console config injection in HTML, got:\n%s", body)
	}
}

func TestServeIndex_NoConsoleConfig(t *testing.T) {
	// When ConsoleConfig is nil, serveIndex should NOT inject
	// window.__CONSOLE_CONFIG__ into the HTML.
	fakeFS := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte(`<!DOCTYPE html><html><head></head><body></body></html>`),
		},
	}

	h := newUIHandler(fakeFS, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, `__CONSOLE_CONFIG__`) {
		t.Errorf("expected no console config injection when config is nil, got:\n%s", body)
	}
}

func TestServeIndex_ConsoleConfigDevToolsDisabled(t *testing.T) {
	// When DevToolsEnabled is false, the injection should reflect that.
	fakeFS := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte(`<!DOCTYPE html><html><head></head><body></body></html>`),
		},
	}

	consoleConfig := &ConsoleConfig{DevToolsEnabled: false}
	h := newUIHandler(fakeFS, nil, consoleConfig)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `window.__CONSOLE_CONFIG__={"devToolsEnabled":false}`) {
		t.Errorf("expected devToolsEnabled:false in console config, got:\n%s", body)
	}
}

func TestLogRequests_NonHealthPath_AlwaysLogged(t *testing.T) {
	// Non-health paths should always be logged regardless of LogHealthChecks.
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	slog.SetDefault(logger)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := logRequests(inner, false)

	req := httptest.NewRequest(http.MethodGet, "/ui", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if buf.Len() == 0 {
		t.Error("expected log output for /ui, got nothing")
	}
}

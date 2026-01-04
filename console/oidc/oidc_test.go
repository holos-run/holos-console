package oidc_test

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"testing"

	"github.com/holos-run/holos-console/console/oidc"
)

func TestNewHandler_Success(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// Create OIDC handler with valid configuration
	handler, err := oidc.NewHandler(ctx, oidc.Config{
		Issuer:       "https://test.example.com/dex",
		ClientID:     "test-client",
		RedirectURIs: []string{"https://test.example.com/callback"},
		Logger:       logger,
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}

	// Verify handler is not nil
	if handler == nil {
		t.Error("NewHandler() returned nil handler")
	}

	// Verify handler implements http.Handler
	var _ http.Handler = handler
}

func TestNewHandler_ValidationErrors(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	tests := []struct {
		name   string
		config oidc.Config
	}{
		{
			name: "missing issuer",
			config: oidc.Config{
				Issuer:       "",
				ClientID:     "test-client",
				RedirectURIs: []string{"https://test.example.com/callback"},
				Logger:       logger,
			},
		},
		{
			name: "missing clientID",
			config: oidc.Config{
				Issuer:       "https://test.example.com/dex",
				ClientID:     "",
				RedirectURIs: []string{"https://test.example.com/callback"},
				Logger:       logger,
			},
		},
		{
			name: "missing redirectURIs",
			config: oidc.Config{
				Issuer:       "https://test.example.com/dex",
				ClientID:     "test-client",
				RedirectURIs: []string{},
				Logger:       logger,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := oidc.NewHandler(ctx, tt.config)
			if err == nil {
				t.Error("NewHandler() expected error, got nil")
			}
		})
	}
}
